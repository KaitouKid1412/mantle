package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStore(t *testing.T) (Store, string) {
	t.Helper()
	l := Layout{Root: filepath.Join(t.TempDir(), "home")}
	if err := l.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "mantle-ui")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Store{L: l}, bin
}

func at(sec int) time.Time { return time.Date(2026, 10, 3, 12, 0, sec, 0, time.UTC) }

func TestInstallAndGet(t *testing.T) {
	s, bin := newStore(t)
	v, err := s.Install("b1", bin, Manifest{GitSHA: "abc", Mods: []string{"demo"}, Created: at(1)})
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "b1" || v.Manifest.BuildID != "b1" || v.Manifest.GitSHA != "abc" || !slices.Equal(v.Manifest.Mods, []string{"demo"}) {
		t.Errorf("version = %+v", v)
	}
	if !isExecutable(v.Binary()) {
		t.Error("installed binary is not executable")
	}
	fi, _ := os.Stat(v.Binary())
	if fi.Mode().Perm()&0o222 != 0 {
		t.Errorf("installed binary is writable: %v", fi.Mode())
	}
	if _, err := s.Install("b1", bin, Manifest{}); !errors.Is(err, ErrVersionExists) {
		t.Errorf("reinstall err = %v, want ErrVersionExists", err)
	}
	for _, bad := range []string{"", ".hidden", "a/b", "../x", strings.Repeat("x", 200)} {
		if _, err := s.Install(bad, bin, Manifest{}); !errors.Is(err, ErrInvalidBuildID) {
			t.Errorf("Install(%q) err = %v", bad, err)
		}
	}
	if _, err := s.Install("b2", filepath.Join(t.TempDir(), "missing"), Manifest{}); err == nil {
		t.Error("install of a missing binary succeeded")
	}
	ents, _ := os.ReadDir(s.L.Versions())
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp") {
			t.Errorf("temporary folder left behind: %s", e.Name())
		}
	}
	if _, err := s.Get("nope"); !errors.Is(err, ErrNoVersion) {
		t.Errorf("Get(nope) err = %v", err)
	}
}

func TestListNewestFirst(t *testing.T) {
	s, bin := newStore(t)
	for i, id := range []string{"old", "newest", "mid"} {
		created := []time.Time{at(1), at(3), at(2)}[i]
		if _, err := s.Install(id, bin, Manifest{Created: created}); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(s.L.Versions(), ".tmp-junk"), 0o755)
	vs, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, v := range vs {
		ids = append(ids, v.ID)
	}
	if !slices.Equal(ids, []string{"newest", "mid", "old"}) {
		t.Errorf("ids = %v", ids)
	}
}

func TestFlipLinks(t *testing.T) {
	s, bin := newStore(t)
	if _, err := s.Current(); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Current() before install err = %v", err)
	}
	s.Install("a", bin, Manifest{Created: at(1)})
	s.Install("b", bin, Manifest{Created: at(2)})
	if err := s.SetCurrent("missing"); err == nil {
		t.Error("SetCurrent to a missing version succeeded")
	}
	if err := s.SetCurrent("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLastGood("a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent("b"); err != nil {
		t.Fatal(err)
	}
	cur, err := s.Current()
	if err != nil || cur.ID != "b" {
		t.Fatalf("current = %+v, %v", cur, err)
	}
	if lg, _ := s.LastGood(); lg.ID != "a" {
		t.Errorf("last-good = %s", lg.ID)
	}
	target, _ := os.Readlink(s.L.Current())
	if filepath.IsAbs(target) {
		t.Errorf("current is an absolute link %q; want relative so ~/.mantle can move", target)
	}
	// The home directory can be moved and the links still resolve.
	moved := s.L.Root + "-moved"
	if err := os.Rename(s.L.Root, moved); err != nil {
		t.Fatal(err)
	}
	s2 := Store{L: Layout{Root: moved}}
	if cur, err := s2.Current(); err != nil || cur.ID != "b" {
		t.Errorf("after move: %+v, %v", cur, err)
	}
	ents, _ := os.ReadDir(moved)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestFlipIsAtomicUnderConcurrentReaders(t *testing.T) {
	s, bin := newStore(t)
	s.Install("a", bin, Manifest{Created: at(1)})
	s.Install("b", bin, Manifest{Created: at(2)})
	s.SetCurrent("a")
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var bad error
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := s.Current(); err != nil {
				mu.Lock()
				bad = err
				mu.Unlock()
				return
			}
		}
	}()
	for i := range 200 {
		id := "a"
		if i%2 == 0 {
			id = "b"
		}
		if err := s.SetCurrent(id); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if bad != nil {
		t.Errorf("a reader saw a missing current link: %v", bad)
	}
}

func TestGC(t *testing.T) {
	s, bin := newStore(t)
	for i := range 14 {
		id := "v" + string(rune('a'+i))
		if _, err := s.Install(id, bin, Manifest{Created: at(i)}); err != nil {
			t.Fatal(err)
		}
	}
	// Newest is "vn". Protect three old ones: current, last-good, a live run.
	s.SetCurrent("va")
	s.SetLastGood("vb")
	WriteRunFile(s.L.RunFile(4242), RunFile{PID: 4242, Version: "vc"})
	WriteRunFile(s.L.RunFile(4343), RunFile{PID: 4343, Version: "vd"}) // dead
	alive := func(pid int) bool { return pid == 4242 }
	old := filepath.Join(s.L.Versions(), ".tmp-old")
	os.MkdirAll(old, 0o755)
	os.Chtimes(old, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour))
	fresh := filepath.Join(s.L.Versions(), ".tmp-fresh")
	os.MkdirAll(fresh, 0o755)

	removed, err := s.GC(10, alive)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(removed)
	if !slices.Equal(removed, []string{"vd"}) {
		t.Errorf("removed = %v, want [vd]", removed)
	}
	vs, _ := s.List()
	if len(vs) != 13 {
		t.Errorf("%d versions left, want 13", len(vs))
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("old temporary folder kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh temporary folder removed (could be an install in progress)")
	}
}

func TestNewBuildID(t *testing.T) {
	ts := time.Date(2026, 10, 3, 17, 45, 1, 0, time.FixedZone("x", 3600))
	if got := NewBuildID(ts, "0123456789abcdef"); got != "20261003-164501-0123456" {
		t.Errorf("got %q", got)
	}
	if got := NewBuildID(ts, ""); got != "20261003-164501" {
		t.Errorf("got %q", got)
	}
	s, bin := newStore(t)
	s.Install("x", bin, Manifest{})
	if got := s.UniqueID("x"); got != "x-2" {
		t.Errorf("UniqueID = %q", got)
	}
}

func TestRunFileRoundTrip(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	rf := RunFile{PID: 7, Version: "v", SessionID: "s", Argv: []string{"a"}, EnginePGIDs: map[string]int{"main": 9}}
	if err := WriteRunFile(l.RunFile(7), rf); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRunFile(l.RunFile(7))
	if err != nil || got.SessionID != "s" || got.EnginePGIDs["main"] != 9 || got.Updated.IsZero() {
		t.Errorf("got %+v, %v", got, err)
	}
	if args := got.RelaunchArgs(); !slices.Equal(args, []string{"--resume", "s"}) {
		t.Errorf("RelaunchArgs = %q", args)
	}
	if args := (RunFile{}).RelaunchArgs(); args != nil {
		t.Errorf("RelaunchArgs of empty = %q", args)
	}
	os.WriteFile(filepath.Join(l.Run(), "junk.json"), []byte("{"), 0o644)
	os.WriteFile(filepath.Join(l.Run(), "8.json"), []byte("{bad"), 0o644)
	runs, _ := l.RunFiles()
	if len(runs) != 1 {
		t.Errorf("runs = %+v", runs)
	}
	if n := l.RemoveStaleRunFiles(func(int) bool { return false }); n != 1 {
		t.Errorf("removed %d stale run files", n)
	}
}

func TestProbationFiles(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	if st := LoadProbation(l, "b"); st != (ProbationState{}) {
		t.Errorf("fresh state = %+v", st)
	}
	SaveProbation(l, "b", ProbationState{Failures: 1})
	if st := LoadProbation(l, "b"); st.Failures != 1 || st.Healthy {
		t.Errorf("state = %+v", st)
	}
	SaveProbation(l, "b", ProbationState{Healthy: true})
	if st := LoadProbation(l, "b"); st.Failures != 0 || !st.Healthy {
		t.Errorf("state = %+v", st)
	}
	if err := MarkHealthy(l, "../evil"); err == nil {
		t.Error("MarkHealthy accepted a path")
	}
}

func TestLockFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "promote.lock")
	l1, err := LockFile(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := LockFile(ctx, p); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second lock err = %v, want deadline exceeded", err)
	}
	got := make(chan error, 1)
	go func() {
		l2, err := LockFile(context.Background(), p)
		if err == nil {
			l2.Unlock()
		}
		got <- err
	}()
	time.Sleep(100 * time.Millisecond)
	l1.Unlock()
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never got the lock")
	}
}
