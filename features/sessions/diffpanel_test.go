package sessions

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func editTranscript() *fakeTranscript {
	edit := toolItem("e1", "Edit", `{"file_path":"/w/a.go"}`, "ok")
	edit.Result.Structured = json.RawMessage(`{"filePath":"/w/a.go","structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]}]}`)
	return &fakeTranscript{items: []*ext.Item{promptItem("p1", "change a"), edit}}
}

func TestDiffPanelRegistration(t *testing.T) {
	h := newHarness(t)
	var found bool
	for _, c := range h.r.Components {
		if c.Component.ID() == DiffPanelID {
			found = c.Slot == ext.SlotSidebarR && slices.Equal(c.Opts.Modes, []ext.LayoutMode{ext.Fullscreen})
		}
	}
	if !found {
		t.Fatal("diff panel not registered in the right sidebar for fullscreen")
	}
	if h.f.diffPanel.View(h.ctx, ext.Area{Width: 36, MaxHeight: 30}).Text != "" {
		t.Fatal("a closed panel must draw nothing")
	}
}

func TestDiffPanelOrDialog(t *testing.T) {
	h := newHarness(t)
	h.ctx.TranscriptV = editTranscript()
	h.f.getwd = func() (string, error) { return t.TempDir(), nil }
	h.ctx.SessionValue.Cwd = ""

	// Inline: the dialog.
	h.command("diff", "")
	if !slices.Equal(h.ctx.Opened, []string{DialogDiff}) {
		t.Fatalf("inline opened %v", h.ctx.Opened)
	}
	// Fullscreen but narrow: still the dialog.
	h.ctx.LayoutMode, h.ctx.W = ext.Fullscreen, 100
	h.command("diff", "")
	if len(h.ctx.Opened) != 2 || h.f.diffPanel.open {
		t.Fatalf("narrow fullscreen opened %v", h.ctx.Opened)
	}
	// Fullscreen and wide: the panel, focused.
	h.ctx.W = 120
	h.ctx.FocusedID = "input.editor"
	h.command("diff", "")
	p := h.f.diffPanel
	if len(h.ctx.Opened) != 2 || !p.open || h.ctx.FocusedID != DiffPanelID {
		t.Fatalf("panel: opened %v open=%v focus=%q", h.ctx.Opened, p.open, h.ctx.FocusedID)
	}
	if v := ansi.Strip(p.View(h.ctx, ext.Area{Width: 36, MaxHeight: 30}).Text); !strings.Contains(v, "Diff") {
		t.Fatalf("panel view:\n%s", v)
	}
	// Keys: next source shows the turn's file; esc closes and gives focus back.
	press(h, p, tea.KeyPressMsg{Code: tea.KeyRight})
	if v := ansi.Strip(p.View(h.ctx, ext.Area{Width: 36, MaxHeight: 30}).Text); !strings.Contains(v, "a.go") {
		t.Fatalf("turn source:\n%s", v)
	}
	press(h, p, tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.open || !p.dismissed || h.ctx.FocusedID != "input.editor" || len(h.ctx.Closed) != 0 {
		t.Fatalf("close: open=%v focus=%q closed=%v", p.open, h.ctx.FocusedID, h.ctx.Closed)
	}
}

func TestDiffPanelAutoOpens(t *testing.T) {
	h := newHarness(t)
	h.ctx.TranscriptV = editTranscript()
	h.f.getwd = func() (string, error) { return t.TempDir(), nil }
	h.ctx.SessionValue.Cwd = ""
	h.ctx.LayoutMode, h.ctx.W = ext.Fullscreen, 120
	h.run(ext.Msg(event(&proto.Result{})))
	if h.f.diffPanel.open {
		t.Fatal("opened on its own below 144 columns")
	}
	h.ctx.W = 150
	h.ctx.FocusedID = "input.editor"
	h.run(ext.Msg(event(&proto.Result{})))
	if !h.f.diffPanel.open || h.ctx.FocusedID != "input.editor" {
		t.Fatalf("auto-open: open=%v focus=%q", h.f.diffPanel.open, h.ctx.FocusedID)
	}
	// Once dismissed, it stays closed until /diff.
	h.f.diffPanel.close(h.ctx)
	h.run(ext.Msg(event(&proto.Result{})))
	if h.f.diffPanel.open {
		t.Fatal("reopened after the user closed it")
	}
}

// press delivers a key to a focused component the way the host does for raw keys.
func press(h *harness, c ext.Focusable, k tea.KeyPressMsg) {
	_, cmd := c.HandleKey(h.ctx, k)
	h.run(cmd)
}
