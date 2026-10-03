package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeSessions writes n synthetic sessions (about 20 KB each) under one project.
func makeSessions(tb testing.TB, n int) Layout {
	tb.Helper()
	l := Layout{ConfigDir: tb.TempDir()}
	dir := l.ProjectDir("/bench/repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		tb.Fatal(err)
	}
	body := strings.Repeat("lorem ipsum ", 80)
	for i := range n {
		id := fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i)
		var b strings.Builder
		prev := "null"
		for j := range 20 {
			kind, content := "assistant", `[{"type":"text","text":"`+body+`"}]`
			if j%2 == 0 {
				kind, content = "user", `"prompt `+fmt.Sprint(j)+`"`
			}
			uuid := fmt.Sprintf("%s-%d", id, j)
			fmt.Fprintf(&b, `{"parentUuid":%s,"isSidechain":false,"type":"%s","message":{"role":"%s","content":%s},"uuid":"%s","timestamp":"2026-09-01T10:00:%02d.000Z","cwd":"/bench/repo","sessionId":"%s","gitBranch":"main","entrypoint":"cli","version":"2.1.288"}`+"\n",
				prev, kind, kind, content, uuid, j, id)
			prev = `"` + uuid + `"`
		}
		fmt.Fprintf(&b, `{"type":"ai-title","aiTitle":"Session %d","sessionId":"%s"}`+"\n", i, id)
		fmt.Fprintf(&b, `{"type":"last-prompt","lastPrompt":"prompt 18","leafUuid":"%s-19","sessionId":"%s"}`+"\n", id, id)
		if err := os.WriteFile(SessionFile(dir, id), []byte(b.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	return l
}

// warmIndex lists the project once to fill and save the cache.
func warmIndex(tb testing.TB, l Layout, cache string, n int) {
	tb.Helper()
	ix := NewIndex(l, cache)
	ms, err := ix.Project("/bench/repo")
	if err != nil || len(ms) != n {
		tb.Fatalf("cold listing = %d, %v", len(ms), err)
	}
	if err := ix.Save(); err != nil {
		tb.Fatal(err)
	}
}

// The plan's budget: a warm listing of 1,000 sessions (fresh process, cache on disk)
// takes under 200 ms. Best of three, to ride out a busy machine.
func TestIndexWarm1000Budget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	const n = 1000
	l := makeSessions(t, n)
	cache := filepath.Join(t.TempDir(), "sessions.idx")
	warmIndex(t, l, cache, n)
	best := time.Hour
	for range 3 {
		start := time.Now()
		ms, err := NewIndex(l, cache).Project("/bench/repo")
		if d := time.Since(start); d < best {
			best = d
		}
		if err != nil || len(ms) != n || ms[0].Title() == "" {
			t.Fatalf("warm listing = %d, %v", len(ms), err)
		}
	}
	t.Logf("warm listing of %d sessions: %v", n, best)
	if best > 200*time.Millisecond {
		t.Fatalf("warm listing took %v, budget 200ms", best)
	}
}

func BenchmarkIndexWarm1000(b *testing.B) {
	const n = 1000
	l := makeSessions(b, n)
	cache := filepath.Join(b.TempDir(), "sessions.idx")
	warmIndex(b, l, cache, n)
	b.ResetTimer()
	for b.Loop() {
		if _, err := NewIndex(l, cache).Project("/bench/repo"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIndexCold1000(b *testing.B) {
	const n = 1000
	l := makeSessions(b, n)
	b.ResetTimer()
	for b.Loop() {
		if _, err := NewIndex(l, "").Project("/bench/repo"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoad(b *testing.B) {
	l := makeSessions(b, 1)
	p := SessionFile(l.ProjectDir("/bench/repo"), "00000000-0000-4000-8000-000000000000")
	for b.Loop() {
		if _, err := Load(p); err != nil {
			b.Fatal(err)
		}
	}
}
