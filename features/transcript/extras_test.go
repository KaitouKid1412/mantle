package transcript

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

func TestFileLinks(t *testing.T) {
	got := applyLinks(fileLink("a b.go", "/w/a b.go"))
	if !strings.Contains(got, "\x1b]8;;file:///w/a%20b.go\x07a b.go\x1b]8;;\x07") {
		t.Fatalf("link = %q", got)
	}
	if got := applyLinks(fileLink("rel.go", "rel.go")); got != "rel.go" {
		t.Fatalf("relative path linked: %q", got)
	}
	// Markers smuggled in through tool input never become non-file links.
	evil := linkOpen + "https://evil.example" + linkMid + "click" + linkClose
	if got := applyLinks(clean(evil)); strings.Contains(got, "\x1b]8") || got != "click" {
		t.Fatalf("untrusted link = %q", got)
	}
	g := newRig(t, 81)
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/w/x.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}}`)
	if raw := strings.Join(g.c.Printed, "\n"); !strings.Contains(raw, "\x1b]8;;file:///w/x.go\x07") {
		t.Fatalf("Read header has no file link: %q", raw)
	}
}

func TestMemoryRecall(t *testing.T) {
	g := newRig(t, 61)
	g.send(`{"type":"system","subtype":"memory_recall","uuid":"m1","mode":"auto","memories":[{"name":"build steps"},{"path":"/m/b.md"}]}`)
	if got := join(g.printed()); !strings.Contains(got, "⏺ Recalled 2 memories") {
		t.Fatalf("printed = %q", got)
	}
	it := g.f.store.Get("mem:m1")
	lines := g.f.renderMemoryRecall(ext.RenderCtx{Width: 60, Mode: ext.Verbose}, it).Lines
	if got := render.Strip(strings.Join(lines, "\n")); !strings.Contains(got, "build steps") || !strings.Contains(got, "/m/b.md") {
		t.Fatalf("verbose = %q", got)
	}
}

func TestDefaultViewTranscript(t *testing.T) {
	f, r := setupFeature(t)
	c := exttest.NewCtx()
	c.Renderers = r.Renderers
	c.SettingsV = exttest.NewSettings(map[string]any{"defaultView": "transcript"})
	for _, s := range r.Starts {
		if cmd := s.Value.(func(ext.Ctx) tea.Cmd)(c); cmd == nil {
			t.Fatal("start hook returned nothing")
		}
	}
	if len(c.Opened) != 1 || c.Opened[0] != ViewerDialogID {
		t.Fatalf("opened = %v", c.Opened)
	}
	// "[" leaves the viewer for the scrollback.
	d, _ := f.newViewer(c, nil)
	if ok, cmd := d.HandleKey(c, keyPress("[")); !ok || cmd == nil || len(c.Closed) != 1 {
		t.Fatalf("[ did not close the viewer: %v %v", ok, c.Closed)
	}
}
