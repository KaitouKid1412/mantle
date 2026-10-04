package input

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

const vtWait = 3 * time.Second

// startHost runs the real host with this feature on a vt emulator, with a
// fake engine attached.
func startHost(t *testing.T, w, h int) (*testkit.Harness, *fakeEngine, *state) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	s := newState()
	s.histPath = filepath.Join(dir, "history.jsonl")
	s.cwd = "/work/demo"
	f := ext.Feature{ID: FeatureID, Order: 400, Setup: func(r ext.Registrar) error { register(r, s); return nil }}
	host := app.NewHost([]ext.Feature{f}, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, StateDir: t.TempDir()})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	eng := &fakeEngine{}
	hs.SendMsg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: eng})
	hs.WaitForText("❯", vtWait)
	return hs, eng, s
}

func waitPrompts(t *testing.T, eng *fakeEngine, n int) []ext.Prompt {
	t.Helper()
	deadline := time.Now().Add(vtWait)
	for time.Now().Before(deadline) {
		if ps := eng.prompts(); len(ps) >= n {
			return ps
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("engine got %d prompts, want %d", len(eng.prompts()), n)
	return nil
}

func TestVTTypeAndSubmit(t *testing.T) {
	hs, eng, _ := startHost(t, 60, 20)
	hs.Type("hello")
	hs.Send("shift+enter")
	hs.Type("world")
	hs.WaitForText("  world", vtWait)
	if !strings.Contains(hs.Screen(), "❯ hello") {
		t.Fatalf("prompt marker and first line:\n%s", hs.Screen())
	}
	hs.Send("enter")
	ps := waitPrompts(t, eng, 1)
	if ps[0].Blocks[0].Text != "hello\nworld" {
		t.Fatalf("sent %q", ps[0].Blocks[0].Text)
	}
	hs.WaitFor(func(s string) bool { return !strings.Contains(s, "world") }, vtWait)

	// Up recalls it.
	hs.Send("up")
	hs.WaitForText("❯ hello", vtWait)
}

func TestVTPasteChip(t *testing.T) {
	hs, eng, s := startHost(t, 60, 20)
	hs.Type("fix ")
	hs.Paste("line one\nline two\nline three\nline four\nline five")
	hs.WaitForText("[Pasted text #1 +4 lines]", vtWait)
	hs.Send("enter")
	ps := waitPrompts(t, eng, 1)
	if !strings.HasSuffix(ps[0].Blocks[0].Text, "line four\nline five") || len(ps[0].InlinePastes) != 1 {
		t.Fatalf("prompt %+v", ps[0])
	}
	time.Sleep(50 * time.Millisecond)
	es, _ := history.Load(s.histPath)
	if len(es) != 1 || es[0].Display != "fix [Pasted text #1 +4 lines]" {
		t.Fatalf("history %+v", es)
	}
}

func TestVTSlashMenu(t *testing.T) {
	hs, eng, _ := startHost(t, 70, 20)
	hs.SendMsg(ext.CommandsMsg{Source: ext.SourceEngine, EngineID: ext.MainEngine, Commands: []ext.Command{
		{Name: "compact", Description: "Compact the conversation", Source: ext.SourceEngine},
		{Name: "cost", Description: "Show the session cost", Source: ext.SourceEngine},
	}})
	hs.Type("/co")
	hs.WaitForText("Compact the conversation", vtWait)
	hs.WaitForText("Show the session cost", vtWait)
	hs.Send("down", "tab")
	hs.WaitForText("❯ /compact", vtWait)
	hs.Send("enter")
	ps := waitPrompts(t, eng, 1)
	if strings.TrimSpace(ps[0].Blocks[0].Text) != "/compact" {
		t.Fatalf("sent %q", ps[0].Blocks[0].Text)
	}
}

func TestVTHelpAndBashMode(t *testing.T) {
	hs, _, _ := startHost(t, 80, 24)
	hs.Type("?")
	hs.WaitForText("for bash mode", vtWait)
	hs.Type("!")
	hs.WaitForText("! ", vtWait)
	hs.WaitForText("Run a shell command", vtWait)
}
