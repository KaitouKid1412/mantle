package fullscreen

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func unitView(t *testing.T, text string, h int) (*transcriptView, *exttest.Ctx) {
	t.Helper()
	ctx := exttest.NewCtx()
	ctx.LayoutMode = ext.Fullscreen
	ctx.Renderers[ext.KeyDefault] = func(rc ext.RenderCtx, it *ext.Item) ext.Block {
		s, _ := it.Data.(string)
		return ext.Block{Lines: strings.Split(s, "\n"), Collapsible: strings.HasPrefix(s, "⏺ Bash")}
	}
	ctx.TranscriptV = &store{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, text)}}
	v := newTranscriptView(terminal.Map{})
	v.Init(ctx)
	v.View(ctx, ext.Area{Width: 60, MaxHeight: h, Mode: ext.Fullscreen})
	return v, ctx
}

func ev(m tea.MouseMsg, x, y int) ext.MouseEvent { return ext.MouseEvent{Msg: m, X: x, Y: y} }

func TestMouseDragCopies(t *testing.T) {
	v, ctx := unitView(t, "alpha beta gamma\ndelta epsilon", 10)
	// The document is 3 lines (blank, alpha…, delta…); mouse rows are relative to the
	// returned lines: row 1 is "alpha…", row 2 "delta…".
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 6, 1))
	v.mouse(ctx, ev(tea.MouseMotionMsg{Button: tea.MouseLeft}, 13, 2))
	cmd := v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 13, 2))
	if got := Text(v.line, v.sel, v.width); got != "beta gamma\ndelta epsilon" {
		t.Fatalf("selection = %q (sel %+v, top %d)", got, v.sel, v.top)
	}
	if cmd == nil {
		t.Fatal("copy-on-select should return a copy Cmd")
	}
	ctx.SettingsV.ClaudeM["copyOnSelect"] = false
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 0, 1))
	v.mouse(ctx, ev(tea.MouseMotionMsg{Button: tea.MouseLeft}, 5, 1))
	for _, m := range exttest.Exec(v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 5, 1))) {
		if _, ok := m.(ext.SelectionMsg); !ok {
			t.Errorf("copyOnSelect=false: no copy on release (got %T)", m)
		}
	}
	if handled, cmd := v.action(ext.ActSelectionCopy)(ctx); !handled || cmd == nil {
		t.Error("selection:copy copies explicitly")
	}
	if handled, _ := v.action(ext.ActSelectionClear)(ctx); !handled || !v.sel.Empty() {
		t.Error("selection:clear")
	}
}

func TestMouseDoubleTripleClick(t *testing.T) {
	v, ctx := unitView(t, "open ./cmd/main.go now", 5)
	row := 1
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 8, row))
	v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 8, row))
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 8, row))
	if got := Text(v.line, v.sel, v.width); got != "./cmd/main.go" {
		t.Errorf("double click = %q", got)
	}
	v.mouse(ctx, ev(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 8, row))
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 8, row))
	if got := Text(v.line, v.sel, v.width); got != "open ./cmd/main.go now" {
		t.Errorf("triple click = %q", got)
	}
	ctx.ClockV.Advance(doubleClick * 2)
	v.mouse(ctx, ev(tea.MouseClickMsg{Button: tea.MouseLeft}, 8, row))
	if !v.sel.Empty() {
		t.Error("a slow click starts over")
	}
}

func TestExtendSelection(t *testing.T) {
	v, ctx := unitView(t, "abc\ndef", 5)
	if handled, _ := v.action(ext.ActSelectionExtendRight)(ctx); handled {
		t.Error("no selection: shift+arrows go to the prompt")
	}
	v.sel = Selection{Anchor: Pos{1, 0}, Head: Pos{1, 0}, Active: true}
	v.action(ext.ActSelectionExtendRight)(ctx)
	v.action(ext.ActSelectionExtendLineEnd)(ctx)
	v.action(ext.ActSelectionExtendDown)(ctx)
	if got := Text(v.line, v.sel, v.width); got != "abc\ndef" {
		t.Errorf("extended = %q (%+v)", got, v.sel)
	}
	v.action(ext.ActSelectionExtendLineStart)(ctx)
	v.action(ext.ActSelectionExtendLeft)(ctx)
	if v.sel.Head.Line != 1 {
		t.Errorf("left from column 0 wraps to the previous line end: %+v", v.sel.Head)
	}
}
