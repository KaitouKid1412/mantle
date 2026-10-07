package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// The subset of file-baton's state.json that batonwatch reads.
type state struct {
	Sessions map[string]*session `json:"sessions"`
	Locks    map[string]*lock    `json:"locks"`
	Touched  map[string][]touch  `json:"touched"`
}

type session struct {
	ID       string    `json:"id"`
	PID      int       `json:"pid"`
	LastSeen time.Time `json:"last_seen"`
}

type lock struct {
	Path    string   `json:"path"`
	Owner   string   `json:"owner"`
	Status  string   `json:"status"` // "held" | "granted"
	Queue   []waiter `json:"queue"`
	Handoff *handoff `json:"handoff"`
}

type waiter struct {
	Session string `json:"session"`
}

type handoff struct {
	From      string `json:"from"`
	Reason    string `json:"reason"`
	Delivered bool   `json:"delivered"`
}

type touch struct {
	Session string `json:"session"`
}

func readState(path string) (*state, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &state{}, nil
		}
		return nil, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// event is one line of events.jsonl.
type event struct {
	T      time.Time `json:"t"`
	Kind   string    `json:"kind"`
	File   string    `json:"file,omitempty"`
	SID    string    `json:"sid,omitempty"`    // 8-char session id, as in file-baton's log
	Other  string    `json:"other,omitempty"`  // previous owner (handoff)
	Status string    `json:"status,omitempty"` // lock status after the change
	Reason string    `json:"reason,omitempty"`
	Msg    string    `json:"msg,omitempty"` // raw log line (kind "log")
}

// Event kinds derived from state snapshots.
const (
	evAcquire   = "acquire"   // file had no lock, now owned by SID
	evHandoff   = "handoff"   // owner changed Other -> SID without a release in between
	evRelease   = "release"   // lock removed; SID was the owner
	evStatus    = "status"    // held <-> granted
	evQueue     = "queue"     // SID joined the file's queue
	evDequeue   = "dequeue"   // SID left the queue (granted, or gave up)
	evDelivered = "delivered" // the handoff to SID was delivered
	evTouched   = "touched"   // SID left uncommitted changes in the file
	evUntouched = "untouched" // the file's touched record for SID went away (commit)
	evSession   = "session+"  // session appeared
	evSessionX  = "session-"  // session disappeared
	evLog       = "log"       // raw file-baton log line
)

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// diff returns the events that turn prev into cur, in a stable order.
func diff(prev, cur *state, now time.Time) []event {
	var evs []event
	add := func(e event) { e.T = now; evs = append(evs, e) }

	for _, f := range keys(prev.Locks, cur.Locks) {
		a, b := prev.Locks[f], cur.Locks[f]
		switch {
		case a == nil && b != nil:
			add(event{Kind: evAcquire, File: f, SID: short(b.Owner), Status: b.Status})
		case a != nil && b == nil:
			add(event{Kind: evRelease, File: f, SID: short(a.Owner)})
		case a.Owner != b.Owner:
			e := event{Kind: evHandoff, File: f, SID: short(b.Owner), Other: short(a.Owner), Status: b.Status}
			if b.Handoff != nil {
				e.Reason = b.Handoff.Reason
			}
			add(e)
		case a.Status != b.Status:
			add(event{Kind: evStatus, File: f, SID: short(b.Owner), Status: b.Status})
		}
		var qa, qb []waiter
		if a != nil {
			qa = a.Queue
		}
		if b != nil {
			qb = b.Queue
		}
		for _, w := range minus(qb, qa) {
			add(event{Kind: evQueue, File: f, SID: short(w)})
		}
		for _, w := range minus(qa, qb) {
			add(event{Kind: evDequeue, File: f, SID: short(w)})
		}
		if b != nil && b.Handoff != nil && b.Handoff.Delivered &&
			(a == nil || a.Owner != b.Owner || a.Handoff == nil || !a.Handoff.Delivered) {
			add(event{Kind: evDelivered, File: f, SID: short(b.Owner), Other: short(b.Handoff.From)})
		}
	}

	for _, f := range keys(prev.Touched, cur.Touched) {
		ta, tb := touchers(prev.Touched[f]), touchers(cur.Touched[f])
		for _, s := range sortedMinus(tb, ta) {
			add(event{Kind: evTouched, File: f, SID: short(s)})
		}
		for _, s := range sortedMinus(ta, tb) {
			add(event{Kind: evUntouched, File: f, SID: short(s)})
		}
	}

	for _, id := range keys(prev.Sessions, cur.Sessions) {
		_, was := prev.Sessions[id]
		_, is := cur.Sessions[id]
		switch {
		case !was && is:
			add(event{Kind: evSession, SID: short(id)})
		case was && !is:
			add(event{Kind: evSessionX, SID: short(id)})
		}
	}
	return evs
}

func keys[V any](a, b map[string]V) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// minus returns the sessions queued in a but not in b.
func minus(a, b []waiter) []string {
	in := map[string]bool{}
	for _, w := range b {
		in[w.Session] = true
	}
	var out []string
	for _, w := range a {
		if !in[w.Session] {
			out = append(out, w.Session)
		}
	}
	return out
}

func touchers(ts []touch) map[string]bool {
	m := map[string]bool{}
	for _, t := range ts {
		m[t.Session] = true
	}
	return m
}

func sortedMinus(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// logLine is a parsed file-baton log line: "<ts> <sid8> <event> <msg>".
type logLine struct {
	T     time.Time
	SID   string
	Event string
	Msg   string
}

func parseLog(line string) (logLine, bool) {
	f := strings.SplitN(line, " ", 4)
	if len(f) < 3 {
		return logLine{}, false
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05.000", f[0], time.Local)
	if err != nil {
		return logLine{}, false
	}
	l := logLine{T: t, SID: f[1], Event: f[2]}
	if len(f) == 4 {
		l.Msg = f[3]
	}
	return l, true
}
