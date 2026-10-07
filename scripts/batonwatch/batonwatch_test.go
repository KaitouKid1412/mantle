package main

import (
	"strings"
	"testing"
	"time"
)

const (
	sidA = "aaaaaaaa-1111"
	sidB = "bbbbbbbb-2222"
)

func at(sec int) time.Time { return time.Date(2026, 10, 7, 12, 0, sec, 0, time.Local) }

func TestDiff(t *testing.T) {
	empty := &state{}
	held := &state{Locks: map[string]*lock{"a.go": {Owner: sidA, Status: "held"}}}
	queued := &state{Locks: map[string]*lock{"a.go": {Owner: sidA, Status: "held", Queue: []waiter{{Session: sidB}}}}}
	handed := &state{Locks: map[string]*lock{"a.go": {Owner: sidB, Status: "granted",
		Handoff: &handoff{From: sidA, Reason: "turn ended"}}}}
	delivered := &state{Locks: map[string]*lock{"a.go": {Owner: sidB, Status: "held",
		Handoff: &handoff{From: sidA, Reason: "turn ended", Delivered: true}}}}

	kinds := func(evs []event) string {
		var s []string
		for _, e := range evs {
			s = append(s, e.Kind+":"+e.SID)
		}
		return strings.Join(s, " ")
	}
	for _, tc := range []struct {
		name      string
		prev, cur *state
		want      string
	}{
		{"acquire", empty, held, "acquire:aaaaaaaa"},
		{"queue", held, queued, "queue:bbbbbbbb"},
		{"handoff dequeues", queued, handed, "handoff:bbbbbbbb dequeue:bbbbbbbb"},
		{"delivered", handed, delivered, "status:bbbbbbbb delivered:bbbbbbbb"},
		{"release", delivered, empty, "release:bbbbbbbb"},
		{"no change", held, held, ""},
	} {
		if got := kinds(diff(tc.prev, tc.cur, at(0))); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseLog(t *testing.T) {
	l, ok := parseLog("2026-10-07T13:10:36.276 a503edfd pre-edit allow internal/x.go")
	if !ok || l.SID != "a503edfd" || l.Event != "pre-edit" || l.Msg != "allow internal/x.go" {
		t.Fatalf("got %+v %v", l, ok)
	}
	if _, ok := parseLog("garbage"); ok {
		t.Fatal("parsed garbage")
	}
}

func TestAnalyzeCleanRun(t *testing.T) {
	evs := []event{
		{T: at(0), Kind: evAcquire, File: "a.go", SID: "aaaaaaaa"},
		{T: at(0), Kind: evLog, SID: "aaaaaaaa", Reason: "pre-edit", Msg: "allow a.go"},
		{T: at(1), Kind: evLog, SID: "bbbbbbbb", Reason: "pre-edit", Msg: "deny a.go"},
		{T: at(1), Kind: evQueue, File: "a.go", SID: "bbbbbbbb"},
		{T: at(5), Kind: evLog, SID: "aaaaaaaa", Reason: "stop", Msg: "released turn locks"},
		{T: at(5), Kind: evHandoff, File: "a.go", SID: "bbbbbbbb", Other: "aaaaaaaa"},
		{T: at(5), Kind: evDequeue, File: "a.go", SID: "bbbbbbbb"},
		{T: at(6), Kind: evDelivered, File: "a.go", SID: "bbbbbbbb", Other: "aaaaaaaa"},
		{T: at(7), Kind: evLog, SID: "bbbbbbbb", Reason: "pre-edit", Msg: "allow a.go"},
		{T: at(9), Kind: evLog, SID: "bbbbbbbb", Reason: "post-bash", Msg: "released 1 committed file(s), 0 handed on"},
		{T: at(9), Kind: evRelease, File: "a.go", SID: "bbbbbbbb"},
	}
	r := analyze(evs, []commit{{Hash: "abc", Files: []string{"a.go"}}})
	if len(r.Violations) != 0 || len(r.Warnings) != 0 {
		t.Fatalf("violations %v warnings %v", r.Violations, r.Warnings)
	}
	if s := r.Sessions["bbbbbbbb"]; s.Denied != 1 || s.Waits != 1 || s.MaxWait != 4*time.Second {
		t.Fatalf("stats %+v", s)
	}
	last := r.Files["a.go"][len(r.Files["a.go"])-1]
	if last.Detail != "commit" {
		t.Fatalf("release cause %q", last.Detail)
	}
}

func TestAnalyzeViolations(t *testing.T) {
	evs := []event{
		{T: at(0), Kind: evAcquire, File: "a.go", SID: "aaaaaaaa"},
		{T: at(1), Kind: evLog, SID: "bbbbbbbb", Reason: "pre-edit", Msg: "allow a.go"}, // not owner
		{T: at(2), Kind: evLog, SID: "cccccccc", Reason: "pre-edit", Msg: "deny a.go"},  // never queued
		{T: at(3), Kind: evHandoff, File: "a.go", SID: "dddddddd", Other: "aaaaaaaa"},   // not queued
		{T: at(4), Kind: evLog, SID: "dddddddd", Reason: "pre-edit", Msg: "allow a.go"}, // undelivered
		{T: at(5), Kind: evLog, SID: "dddddddd", Reason: "session-end", Msg: "released everything (other)"},
		{T: at(20), Kind: evRelease, File: "a.go", SID: "dddddddd"}, // outlived session
	}
	r := analyze(evs, []commit{{Hash: "abc", Files: []string{"b.go"}}})
	want := []string{"did not own", "not queued", "was not queued", "before its handoff", "after its session-end"}
	all := strings.Join(r.Violations, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing violation %q in:\n%s", w, all)
		}
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "b.go") {
		t.Errorf("warnings %v", r.Warnings)
	}
}

func TestFileKey(t *testing.T) {
	for in, want := range map[string]string{
		"/repo/pkg/x.go":                       "pkg/x.go",
		"/repo/.claude/worktrees/13a/pkg/x.go": "[13a] pkg/x.go",
		"pkg/x.go":                             "pkg/x.go",
		"/elsewhere/x.go":                      "/elsewhere/x.go",
	} {
		if got := fileKey("/repo", in); got != want {
			t.Errorf("fileKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := relOf("[13a] pkg/x.go"); got != "pkg/x.go" {
		t.Errorf("relOf = %q", got)
	}
}

// Two worktrees hold the same relative path at once; log lines name it relatively.
func TestAnalyzeWorktrees(t *testing.T) {
	evs := []event{
		{T: at(0), Kind: evAcquire, File: "[13a] a.go", SID: "aaaaaaaa"},
		{T: at(0), Kind: evAcquire, File: "[13b] a.go", SID: "bbbbbbbb"},
		{T: at(1), Kind: evLog, SID: "aaaaaaaa", Reason: "pre-edit", Msg: "allow a.go"},
		{T: at(1), Kind: evLog, SID: "bbbbbbbb", Reason: "pre-edit", Msg: "allow a.go"},
		{T: at(2), Kind: evLog, SID: "cccccccc", Reason: "pre-edit", Msg: "allow a.go"},
	}
	r := analyze(evs, []commit{{Hash: "abc", Files: []string{"a.go"}}})
	if len(r.Violations) != 1 || !strings.Contains(r.Violations[0], "cccccccc") {
		t.Fatalf("violations %v", r.Violations)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("warnings %v", r.Warnings)
	}
}
