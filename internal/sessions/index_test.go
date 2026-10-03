package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReadMetaPlain(t *testing.T) {
	m, err := ReadMeta(fixturePath("-work-demo", sidPlain))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != sidPlain || m.Cwd != "/work/demo" || m.GitBranch != "main" || m.Version != "2.1.288" ||
		m.Entrypoint != "cli" || m.Hidden || !m.HasMessages {
		t.Fatalf("meta = %+v", m)
	}
	if m.FirstPrompt != "Explain the build" || m.LastPrompt != "And the tests?" ||
		m.AITitle != "Explain the build system" || m.Title() != "Explain the build system" ||
		m.MessageCount != 6 || m.LeafUUID != u("1s", 2) || m.PermissionMode != "default" {
		t.Fatalf("meta = %+v", m)
	}
	if !m.Created.Equal(time.Date(2026, 9, 1, 10, 0, 1, 0, time.UTC)) ||
		!m.LastActive.Equal(time.Date(2026, 9, 1, 10, 1, 2, 0, time.UTC)) {
		t.Fatalf("created=%v last=%v", m.Created, m.LastActive)
	}
	if m.Cost == nil || m.Cost.CostUSD != 0.0123 || m.Cost.Tokens() != 410 {
		t.Fatalf("cost = %+v", m.Cost)
	}
}

func TestReadMetaVariants(t *testing.T) {
	tools, _ := ReadMeta(fixturePath("-work-demo", sidTools))
	if !tools.Hidden || tools.GitBranch != "feature/x" || tools.PRNumber != 42 ||
		tools.PRURL != "https://github.com/example/demo/pull/42" || tools.Cost != nil {
		t.Fatalf("tools = %+v", tools)
	}
	messy, _ := ReadMeta(fixturePath("-work-demo", sidMessy))
	if messy.Title() != "My renamed session" || messy.FirstPrompt != "! git status" || messy.Summary != "Legacy summary title" {
		t.Fatalf("messy = %+v", messy)
	}
	empty, _ := ReadMeta(fixturePath("-work-demo", sidEmpty))
	if empty.HasMessages || empty.PermissionMode != "plan" {
		t.Fatalf("empty = %+v", empty)
	}
	other, _ := ReadMeta(fixturePath("-work-other", sidOther))
	if other.Cwd != "/work/moved" || !other.InWorktree {
		t.Fatalf("other = %+v", other)
	}
	compact, _ := ReadMeta(fixturePath("-work-demo", sidCompact))
	if compact.FirstPrompt != "Start a long task" {
		t.Fatalf("compact first prompt = %q", compact.FirstPrompt)
	}
}

// A big transcript: the head and tail windows do not overlap, and the first line is
// longer than the head window.
func TestReadMetaLargeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, sidPlain+".jsonl")
	var b strings.Builder
	huge := strings.Repeat("y", 2*liteChunk)
	fmt.Fprintf(&b, `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"%s"},"uuid":"u0","timestamp":"2026-09-01T09:00:00.000Z","cwd":"/big/repo","sessionId":"%s","gitBranch":"dev"}`+"\n", huge, sidPlain)
	prev := "u0"
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&b, `{"parentUuid":"%s","isSidechain":false,"type":"assistant","message":{"id":"m%d","role":"assistant","model":"m","content":[{"type":"text","text":"%s"}]},"uuid":"u%d","timestamp":"2026-09-01T09:%02d:00.000Z","cwd":"/big/repo","sessionId":"%s","gitBranch":"dev"}`+"\n",
			prev, i, strings.Repeat("z", 1000), i, i%60, sidPlain)
		prev = fmt.Sprintf("u%d", i)
	}
	fmt.Fprintf(&b, `{"type":"custom-title","customTitle":"Big one","sessionId":"%s"}`+"\n", sidPlain)
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.Cwd != "/big/repo" || m.GitBranch != "dev" || m.Title() != "Big one" || !m.HasMessages ||
		m.LastActive.Minute() != 300%60 {
		t.Fatalf("meta = %+v", m)
	}
}

func TestIndexDirAndCache(t *testing.T) {
	l := copyConfig(t)
	cache := filepath.Join(t.TempDir(), "cache", "sessions.idx")
	ix := NewIndex(l, cache)
	ms, err := ix.Project("/work/demo")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	// Newest mtime first; the metadata-only session is left out.
	if want := []string{sidPlain, sidTools, sidCompact, sidBranch, sidMessy}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %v", ids)
	}
	if ms[0].ProjectDir != l.ProjectDir("/work/demo") {
		t.Fatalf("project dir = %q", ms[0].ProjectDir)
	}
	if err := ix.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatal(err)
	}

	// A fresh index serves unchanged files from the cache without reading them: make a
	// cached file unreadable and check its metadata still comes back.
	victim := SessionFile(l.ProjectDir("/work/demo"), sidBranch)
	if err := os.Chmod(victim, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(victim, 0o644) })
	ix2 := NewIndex(l, cache)
	ms2, err := ix2.Dir(l.ProjectDir("/work/demo"))
	if err != nil || len(ms2) != 5 {
		t.Fatalf("cached listing = %d, %v", len(ms2), err)
	}
	os.Chmod(victim, 0o644)

	// A changed file is re-read.
	p := SessionFile(l.ProjectDir("/work/demo"), sidCompact)
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, `{"type":"custom-title","customTitle":"Renamed later","sessionId":"%s"}`+"\n", sidCompact)
	f.Close()
	m, err := ix2.Get(p)
	if err != nil || m.Title() != "Renamed later" {
		t.Fatalf("Get after append = %q, %v", m.Title(), err)
	}

	// A deleted file drops out of the cache.
	if err := os.Remove(SessionFile(l.ProjectDir("/work/demo"), sidMessy)); err != nil {
		t.Fatal(err)
	}
	ms3, _ := ix2.Dir(l.ProjectDir("/work/demo"))
	if len(ms3) != 4 {
		t.Fatalf("after delete = %d", len(ms3))
	}
	if err := ix2.Save(); err != nil {
		t.Fatal(err)
	}
	ix3 := NewIndex(l, cache)
	ix3.mu.Lock()
	ix3.load()
	n := len(ix3.entries)
	ix3.mu.Unlock()
	if n != 5 { // 4 demo files with messages + the metadata-only one
		t.Fatalf("cache entries = %d", n)
	}
}

func TestIndexAllAndGroups(t *testing.T) {
	l := copyConfig(t)
	ix := NewIndex(l, "")
	all, err := ix.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 || all[0].ID != sidOther {
		t.Fatalf("all = %d, first %s", len(all), all[0].ID)
	}
	pds, _ := ix.ProjectDirs()
	if len(pds) != 2 {
		t.Fatalf("project dirs = %v", pds)
	}
	groups := GroupBy(all, ByProject)
	if len(groups) != 2 {
		t.Fatalf("groups = %d", len(groups))
	}
	var demo *Group
	for _, g := range groups {
		if g.Key == "/work/demo" {
			demo = g
		}
	}
	if demo == nil || demo.Sessions != 5 || demo.WithCost != 1 || demo.Totals.CostUSD != 0.0123 {
		t.Fatalf("demo group = %+v", demo)
	}
	days := GroupBy(all, ByDay(time.UTC))
	if len(days) != 1 || days[0].Key != "2026-09-01" || days[0].Sessions != 6 {
		t.Fatalf("days = %+v", days[0])
	}
	if tot := Total(all); tot.Sessions != 6 || tot.Totals.Models["claude-opus-5-5"].OutputTokens != 50 {
		t.Fatalf("total = %+v", tot)
	}
}

func TestIndexCacheVersionMismatch(t *testing.T) {
	l := copyConfig(t)
	cache := filepath.Join(t.TempDir(), "sessions.idx")
	if err := os.WriteFile(cache, []byte(`{"version":999,"entries":{"x":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(l, cache)
	ms, err := ix.Project("/work/demo")
	if err != nil || len(ms) != 5 {
		t.Fatalf("listing = %d, %v", len(ms), err)
	}
	if _, ok := ix.entries["x"]; ok {
		t.Fatal("stale cache version was used")
	}
}
