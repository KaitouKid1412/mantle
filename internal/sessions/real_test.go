package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestRealSessions parses every local session read-only and reports record types this
// package does not know. It only logs counts and type names, never content.
//
//	MANTLE_TEST_REAL_SESSIONS=1 go test -run TestRealSessions -v ./internal/sessions
func TestRealSessions(t *testing.T) {
	if os.Getenv("MANTLE_TEST_REAL_SESSIONS") != "1" {
		t.Skip("set MANTLE_TEST_REAL_SESSIONS=1 to parse local sessions")
	}
	l := DefaultLayout()
	files, _ := filepath.Glob(filepath.Join(l.ProjectsDir(), "*", "*.jsonl"))
	subs, _ := filepath.Glob(filepath.Join(l.ProjectsDir(), "*", "*", "subagents", "*.jsonl"))
	files = append(files, subs...)
	if len(files) == 0 {
		t.Skip("no local sessions")
	}

	unknown := map[string]int{}
	var records, malformed, partial, orphans int
	start := time.Now()
	for _, f := range files {
		tr, err := Load(f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		records += len(tr.Records)
		malformed += tr.Malformed
		orphans += tr.Tree.Orphans
		if tr.Partial {
			partial++
		}
		for _, r := range tr.Records {
			if !KnownKinds[r.Kind()] {
				unknown[r.Kind()]++
			}
		}
		_ = tr.Main(BranchOptions{AcrossCompaction: true})
		_ = tr.Title()
		_ = SessionTotals(tr)
	}
	t.Logf("parsed %d files, %d records in %v: malformed=%d partial=%d orphans=%d",
		len(files), records, time.Since(start).Round(time.Millisecond), malformed, partial, orphans)
	kinds := make([]string, 0, len(unknown))
	for k := range unknown {
		kinds = append(kinds, fmt.Sprintf("%q×%d", k, unknown[k]))
	}
	sort.Strings(kinds)
	if len(kinds) > 0 {
		t.Logf("record types not in KnownKinds: %s", strings.Join(kinds, ", "))
	}

	// Slugs: each session's first recorded cwd must map to the directory it lives in.
	var checked, mismatched int
	mains, _ := filepath.Glob(filepath.Join(l.ProjectsDir(), "*", "*.jsonl"))
	for _, f := range mains {
		m, err := ReadMeta(f)
		if err != nil || m.Cwd == "" || m.Cwd != firstField(mustHead(t, f), "cwd") {
			continue // relocated, or no envelope in the head
		}
		checked++
		if want := filepath.Base(filepath.Dir(f)); Slug(m.Cwd) != want && l.ProjectDirName == "" {
			mismatched++
		}
	}
	t.Logf("slug check: %d sessions, %d mismatched", checked, mismatched)
	if mismatched > 0 {
		t.Errorf("%d sessions live in a directory that is not Slug(cwd)", mismatched)
	}

	cache := filepath.Join(t.TempDir(), "sessions.idx")
	ix := NewIndex(l, cache)
	start = time.Now()
	all, err := ix.All()
	cold := time.Since(start)
	if err != nil {
		t.Error(err)
	}
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if _, err := NewIndex(l, cache).All(); err != nil {
		t.Error(err)
	}
	t.Logf("index: %d sessions, cold %v, warm %v", len(all), cold.Round(time.Millisecond), time.Since(start).Round(time.Millisecond))
}

func mustHead(t *testing.T, path string) []byte {
	ht, err := readHeadTail(path)
	if err != nil {
		t.Fatal(err)
	}
	return ht.head
}
