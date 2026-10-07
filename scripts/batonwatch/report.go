package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Matching windows. State is polled, so a snapshot can trail the log line
// that explains it by up to one poll interval plus file-baton's own write.
const (
	ownWindow   = 1500 * time.Millisecond // allow F vs. the snapshot showing S owns F
	queueWindow = 2 * time.Second         // deny F vs. S appearing in F's queue
	causeWindow = 2 * time.Second         // a release vs. the log line that caused it
	endWindow   = 3 * time.Second         // session-end vs. its locks disappearing
)

func readEvents(path string) ([]event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var evs []event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			evs = append(evs, e)
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].T.Before(evs[j].T) })
	return evs, sc.Err()
}

func writeReport(p paths, out string) error {
	evs, err := readEvents(p.Events)
	if err != nil {
		return err
	}
	for i := range evs {
		evs[i].File = fileKey(p.Repo, evs[i].File)
	}
	commits := gitCommits(firstTime(evs))
	r := analyze(evs, commits)
	var w io.Writer = os.Stdout
	if out != "-" {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	r.write(w)
	if out != "-" {
		fmt.Fprintf(os.Stderr, "batonwatch: %d events, %d violations, %d warnings -> %s\n",
			len(evs), len(r.Violations), len(r.Warnings), out)
	}
	return nil
}

// commit is one git commit in the recorded window. Sessions run in worktrees
// and commit on their own branches, so every local branch is read.
type commit struct {
	Hash, Subject string
	Files         []string
}

func gitCommits(since time.Time) []commit {
	if since.IsZero() {
		return nil
	}
	raw, err := git("log", "--branches", "--no-merges", "--since="+since.Format(time.RFC3339), "--reverse",
		"--format=%x00%h %s", "--name-only")
	if err != nil {
		return nil
	}
	var cs []commit
	for _, block := range strings.Split(raw, "\x00") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		h, s, _ := strings.Cut(lines[0], " ")
		c := commit{Hash: h, Subject: s}
		for _, f := range lines[1:] {
			if f = strings.TrimSpace(f); f != "" {
				c.Files = append(c.Files, f)
			}
		}
		cs = append(cs, c)
	}
	return cs
}

func firstTime(evs []event) time.Time {
	if len(evs) == 0 {
		return time.Time{}
	}
	return evs[0].T
}

// report is the result of replaying the events.
type report struct {
	Start, End time.Time
	Counts     map[string]int
	Files      map[string][]row // per-file timeline
	Sessions   map[string]*sessStats
	Violations []string
	Warnings   []string
}

type row struct {
	T      time.Time
	What   string
	SID    string
	Detail string
}

type sessStats struct {
	Acquired, Denied, Waits int
	MaxWait, Held           time.Duration
}

// ownerSpan says who owned a file from T on ("" = unlocked).
type ownerSpan struct {
	T     time.Time
	Owner string
}

func analyze(evs []event, commits []commit) *report {
	r := &report{Counts: map[string]int{}, Files: map[string][]row{}, Sessions: map[string]*sessStats{}}
	if len(evs) > 0 {
		r.Start, r.End = evs[0].T, evs[len(evs)-1].T
	}
	stat := func(sid string) *sessStats {
		if r.Sessions[sid] == nil {
			r.Sessions[sid] = &sessStats{}
		}
		return r.Sessions[sid]
	}
	ts := func(t time.Time) string { return t.Format("15:04:05.000") }

	// Pass 1: owner timelines, queue joins and log lines, for windowed lookups.
	owners := map[string][]ownerSpan{}
	queued := map[string][]event{} // file -> queue events
	var logs []event
	locked := map[string]bool{} // files that were ever locked or touched
	for _, e := range evs {
		switch e.Kind {
		case evAcquire, evHandoff:
			owners[e.File] = append(owners[e.File], ownerSpan{e.T, e.SID})
			locked[relOf(e.File)] = true
		case evRelease:
			owners[e.File] = append(owners[e.File], ownerSpan{e.T, ""})
		case evQueue:
			queued[e.File] = append(queued[e.File], e)
		case evTouched:
			locked[relOf(e.File)] = true
		case evLog:
			logs = append(logs, e)
		}
	}
	ownerAt := func(f string, t time.Time) string {
		o := ""
		for _, s := range owners[f] {
			if s.T.After(t) {
				break
			}
			o = s.Owner
		}
		return o
	}
	ownedWithin := func(f, sid string, t time.Time, d time.Duration) bool {
		if ownerAt(f, t) == sid {
			return true
		}
		for _, s := range owners[f] {
			if s.Owner == sid && !s.T.Before(t.Add(-d)) && !s.T.After(t.Add(d)) {
				return true
			}
		}
		return false
	}
	cause := func(sid string, t time.Time) string {
		for _, l := range logs {
			if l.SID != sid || l.T.Before(t.Add(-causeWindow)) || l.T.After(t.Add(causeWindow/4)) {
				continue
			}
			switch l.Reason {
			case "stop":
				return "turn end"
			case "session-end":
				return "session end"
			case "post-bash":
				return "commit"
			case "release":
				return "forced"
			}
		}
		return "timeout/idle (no log line)"
	}

	// Pass 2: replay.
	queue := map[string]map[string]time.Time{} // file -> sid -> since
	since := map[string]time.Time{}            // file -> owned since
	undelivered := map[string]string{}         // file -> new owner awaiting its handoff
	for _, e := range evs {
		r.Counts[e.Kind]++
		add := func(what, detail string) {
			r.Files[e.File] = append(r.Files[e.File], row{e.T, what, e.SID, detail})
		}
		switch e.Kind {
		case evAcquire:
			stat(e.SID).Acquired++
			since[e.File] = e.T
			add("acquire", e.Status)
		case evHandoff:
			if _, ok := queue[e.File][e.SID]; !ok {
				r.Violations = append(r.Violations, fmt.Sprintf("%s %s: owner changed %s -> %s, but %s was not queued",
					ts(e.T), e.File, e.Other, e.SID, e.SID))
			}
			if t, ok := since[e.File]; ok {
				stat(e.Other).Held += e.T.Sub(t)
			}
			stat(e.SID).Acquired++
			since[e.File] = e.T
			undelivered[e.File] = e.SID
			add("handoff", fmt.Sprintf("from %s (%s)", e.Other, e.Reason))
		case evRelease:
			if t, ok := since[e.File]; ok {
				stat(e.SID).Held += e.T.Sub(t)
				delete(since, e.File)
			}
			delete(undelivered, e.File)
			add("release", cause(e.SID, e.T))
		case evStatus:
			add("status", e.Status)
		case evQueue:
			if queue[e.File] == nil {
				queue[e.File] = map[string]time.Time{}
			}
			queue[e.File][e.SID] = e.T
			add("queue", "")
		case evDequeue:
			t0 := queue[e.File][e.SID]
			delete(queue[e.File], e.SID)
			wait := e.T.Sub(t0)
			s := stat(e.SID)
			s.Waits++
			if wait > s.MaxWait {
				s.MaxWait = wait
			}
			granted := ownedWithin(e.File, e.SID, e.T, ownWindow)
			detail := "granted after " + wait.Round(time.Second).String()
			if !granted {
				detail = "left without the file after " + wait.Round(time.Second).String()
				r.Warnings = append(r.Warnings, fmt.Sprintf("%s %s: %s left the queue without getting the file",
					ts(e.T), e.File, e.SID))
			}
			add("dequeue", detail)
		case evDelivered:
			delete(undelivered, e.File)
			add("delivered", "from "+e.Other)
		case evTouched:
			add("touched", "uncommitted changes")
		case evUntouched:
			add("untouched", "")
		case evLog:
			r.checkLog(e, ownedWithin, queue, queued, undelivered, owners)
		}
	}

	// Waiters still queued, and locks of sessions that ended.
	for f, q := range queue {
		for sid, t := range q {
			r.Violations = append(r.Violations, fmt.Sprintf("%s: %s queued at %s and never granted or dequeued", f, sid, ts(t)))
		}
	}
	for _, l := range logs {
		if l.Reason != "session-end" {
			continue
		}
		for f := range owners {
			if ownerAt(f, l.T.Add(endWindow)) == l.SID {
				r.Violations = append(r.Violations, fmt.Sprintf("%s %s: still owned by %s %s after its session-end",
					ts(l.T), f, l.SID, endWindow))
			}
		}
	}

	// Files committed without ever being locked: probably edited from a shell.
	for _, c := range commits {
		for _, f := range c.Files {
			if !locked[f] {
				r.Warnings = append(r.Warnings, fmt.Sprintf("commit %s %q: %s was never locked or touched (shell edit, or a file no Claude session edited)",
					c.Hash, c.Subject, f))
			}
		}
	}
	return r
}

// checkLog checks one file-baton decision against the recorded state. The log
// names files relative to the session's checkout while the state keys them by
// absolute path, so a log line matches every recorded key with that relative path.
func (r *report) checkLog(e event, ownedWithin func(string, string, time.Time, time.Duration) bool,
	queue map[string]map[string]time.Time, queued map[string][]event, undelivered map[string]string,
	owners map[string][]ownerSpan) {
	if e.Reason != "pre-edit" {
		return
	}
	verdict, f, ok := strings.Cut(e.Msg, " ")
	if !ok {
		return
	}
	var ks []string
	for k := range owners {
		if relOf(k) == f {
			ks = append(ks, k)
		}
	}
	for k := range queued {
		if relOf(k) == f && owners[k] == nil {
			ks = append(ks, k)
		}
	}
	ts := e.T.Format("15:04:05.000")
	switch verdict {
	case "allow":
		owned := len(ks) == 0 // untracked: nothing to check against
		for _, k := range ks {
			owned = owned || ownedWithin(k, e.SID, e.T, ownWindow)
			if undelivered[k] == e.SID {
				r.Violations = append(r.Violations, fmt.Sprintf("%s %s: %s edited before its handoff was delivered", ts, k, e.SID))
			}
		}
		if !owned {
			r.Violations = append(r.Violations, fmt.Sprintf("%s %s: edit allowed for %s, which did not own it", ts, f, e.SID))
		}
	case "deny":
		r.stat(e.SID).Denied++
		for _, k := range ks {
			if _, ok := queue[k][e.SID]; ok {
				return
			}
			for _, q := range queued[k] {
				if q.SID == e.SID && !q.T.Before(e.T.Add(-queueWindow)) && !q.T.After(e.T.Add(queueWindow)) {
					return
				}
			}
		}
		r.Violations = append(r.Violations, fmt.Sprintf("%s %s: edit denied for %s, but it was not queued", ts, f, e.SID))
	}
}

// fileKey turns a state path into a report key: the path relative to its
// checkout, prefixed with "[name] " for a worktree under .claude/worktrees.
// Relative paths (log lines, tests) are returned unchanged.
func fileKey(repo, path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(repo, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	rel = filepath.ToSlash(rel)
	if rest, ok := strings.CutPrefix(rel, ".claude/worktrees/"); ok {
		if name, file, ok := strings.Cut(rest, "/"); ok {
			return "[" + name + "] " + file
		}
	}
	return rel
}

// relOf strips the worktree prefix from a report key.
func relOf(key string) string {
	if strings.HasPrefix(key, "[") {
		if _, rel, ok := strings.Cut(key, "] "); ok {
			return rel
		}
	}
	return key
}

func (r *report) stat(sid string) *sessStats {
	if r.Sessions[sid] == nil {
		r.Sessions[sid] = &sessStats{}
	}
	return r.Sessions[sid]
}

func (r *report) write(w io.Writer) {
	p := func(format string, a ...any) { fmt.Fprintf(w, format, a...) }
	p("# file-baton run report\n\n")
	p("Generated by `scripts/batonwatch report` from `.git/file-baton-watch/events.jsonl`.\n")
	p("Recorded %s to %s (%s).\n\n", r.Start.Format("2006-01-02 15:04:05"), r.End.Format("15:04:05"),
		r.End.Sub(r.Start).Round(time.Second))

	p("## Result\n\n")
	if len(r.Violations) == 0 {
		p("**No invariant violations.**\n\n")
	} else {
		p("**%d invariant violation(s):**\n\n", len(r.Violations))
		for _, v := range r.Violations {
			p("- %s\n", v)
		}
		p("\n")
	}
	if len(r.Warnings) > 0 {
		p("Warnings (worth a look, not necessarily bugs):\n\n")
		for _, v := range r.Warnings {
			p("- %s\n", v)
		}
		p("\n")
	}
	p("Checks: a handoff only goes to a queued session; an allowed edit happens while the\n")
	p("session owns the file and after its handoff was delivered; a denied edit queues the\n")
	p("session; every waiter is granted or leaves; no lock outlives its session; every\n")
	p("committed file was locked by some session.\n\n")

	p("## Counts\n\n| Event | Count |\n|---|---|\n")
	for _, k := range sortedKeys(r.Counts) {
		p("| %s | %d |\n", k, r.Counts[k])
	}

	p("\n## Sessions\n\n| Session | Locks taken | Edits denied | Waits | Longest wait | Lock time (summed over files) |\n|---|---|---|---|---|---|\n")
	for _, sid := range sortedKeys(r.Sessions) {
		s := r.Sessions[sid]
		p("| %s | %d | %d | %d | %s | %s |\n", sid, s.Acquired, s.Denied, s.Waits,
			s.MaxWait.Round(time.Second), s.Held.Round(time.Second))
	}

	p("\n## Files\n")
	for _, f := range sortedKeys(r.Files) {
		if f == "" {
			continue
		}
		p("\n### %s\n\n| Time | Event | Session | Detail |\n|---|---|---|---|\n", f)
		for _, x := range r.Files[f] {
			p("| %s | %s | %s | %s |\n", x.T.Format("15:04:05"), x.What, x.SID, x.Detail)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
