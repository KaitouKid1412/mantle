package sessions

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func openPicker(t *testing.T, h *harness, args any) *picker {
	t.Helper()
	p := h.openDialog(DialogResume, args).(*picker)
	if p.loading {
		t.Fatal("picker still loading after Init")
	}
	return p
}

func viewText(h *harness, p *picker, w int) string {
	return ansi.Strip(p.View(h.ctx, ext.Area{Width: w, MaxHeight: 30}).Text)
}

func visibleIDs(p *picker) []string {
	var out []string
	for _, i := range p.view {
		out = append(out, p.list[i].ID)
	}
	return out
}

func TestPickerListAndSearch(t *testing.T) {
	h := newHarness(t)
	p := openPicker(t, h, nil)
	if got, want := visibleIDs(p), []string{sidPlain, sidTools, sidCompact, sidBranch, sidMessy}; !slices.Equal(got, want) {
		t.Fatalf("list = %v", got)
	}
	v := viewText(h, p, 100)
	for _, s := range []string{"Resume a conversation", "⌕ Search…", "    demo\n", "❯ Explain the build system", "PR #42", "KB"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q:\n%s", s, v)
		}
	}

	h.typeText("color")
	if got := visibleIDs(p); !slices.Equal(got, []string{sidBranch}) {
		t.Fatalf("search = %v", got)
	}
	// Space types when there is a query; backspace and ctrl+u edit it.
	h.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if p.query != "color " {
		t.Fatalf("query = %q", p.query)
	}
	h.key(keyPress('u', tea.ModCtrl))
	if p.query != "" || len(p.view) != 5 {
		t.Fatalf("after ctrl+u: %q %d", p.query, len(p.view))
	}
	// Fuzzy fallback when no session contains the terms.
	h.typeText("xplnbld")
	if got := visibleIDs(p); len(got) == 0 || got[0] != sidPlain {
		t.Fatalf("fuzzy = %v", got)
	}

	// The initial query from /resume <search>.
	h2 := newHarness(t)
	p2 := openPicker(t, h2, PickerArgs{Query: "renamed"})
	if got := visibleIDs(p2); !slices.Equal(got, []string{sidMessy}) {
		t.Fatalf("initial query = %v", got)
	}
}

func TestPickerResumeSelected(t *testing.T) {
	h := newHarness(t)
	openPicker(t, h, nil)
	h.key(tea.KeyPressMsg{Code: tea.KeyDown})
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !slices.Equal(h.ctx.Closed, []string{DialogResume}) {
		t.Fatalf("closed = %v", h.ctx.Closed)
	}
	if s := find[ext.EngineStartMsg](h); len(s) != 1 || s[0].Opts.Resume != sidTools {
		t.Fatalf("start = %+v", s)
	}
}

func TestPickerScopesAndBranch(t *testing.T) {
	h := newHarness(t)
	p := openPicker(t, h, nil)
	h.key(keyPress('a', tea.ModCtrl))
	if !p.scope.all || len(p.view) != 6 || !strings.Contains(viewText(h, p, 120), "all projects") {
		t.Fatalf("all projects: %v", visibleIDs(p))
	}
	if v := viewText(h, p, 160); !strings.Contains(v, "    moved") {
		t.Fatalf("project group not shown:\n%s", v)
	}
	h.key(keyPress('a', tea.ModCtrl))
	if p.scope.all || len(p.view) != 5 {
		t.Fatal("ctrl+a did not toggle back")
	}
	p.branch = "feature/x"
	h.key(keyPress('b', tea.ModCtrl))
	if got := visibleIDs(p); !slices.Equal(got, []string{sidTools}) || !strings.Contains(viewText(h, p, 100), "branch feature/x") {
		t.Fatalf("branch filter = %v", got)
	}
	h.key(keyPress('w', tea.ModCtrl))
	if !p.scope.worktrees {
		t.Fatal("ctrl+w")
	}
}

func TestPickerPreview(t *testing.T) {
	h := newHarness(t)
	p := openPicker(t, h, nil)
	h.key(tea.KeyPressMsg{Code: tea.KeyTab})
	if p.preview == nil || p.preview.loading {
		t.Fatalf("preview = %+v", p.preview)
	}
	v := viewText(h, p, 100)
	if !strings.Contains(v, "> Explain the build") || !strings.Contains(v, "Run make test.") {
		t.Fatalf("preview view:\n%s", v)
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.preview != nil || len(h.ctx.Closed) != 0 {
		t.Fatal("esc should close the preview first")
	}
	// Space previews when the search is empty.
	h.key(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if p.preview == nil {
		t.Fatal("space did not preview")
	}
}

func TestPickerRename(t *testing.T) {
	h := newHarness(t)
	h.eng.supports[proto.SubRenameSession] = true
	p := openPicker(t, h, nil)
	h.key(tea.KeyPressMsg{Code: tea.KeyDown})
	h.key(keyPress('r', tea.ModCtrl))
	if !p.renaming {
		t.Fatal("ctrl+r did not start renaming")
	}
	h.typeText("Tool tour")
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(h.eng.controls) != 1 {
		t.Fatalf("controls = %+v", h.eng.controls)
	}
	req := h.eng.controls[0].req.(proto.RenameSessionRequest)
	if req.Title != "Tool tour" || req.SessionID != sidTools {
		t.Fatalf("rename req = %+v", req)
	}
	if m, _ := p.selected(); m.Title() != "Tool tour" || h.f.titles[sidTools] != "Tool tour" {
		t.Fatalf("row title = %q", m.Title())
	}

	// Without an engine that can rename: a notice.
	h2 := newHarness(t)
	openPicker(t, h2, nil)
	h2.key(keyPress('r', tea.ModCtrl))
	h2.typeText("x")
	h2.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(h2.ctx.Notices) != 1 || !strings.Contains(h2.ctx.Notices[0].Text, "running Claude session") {
		t.Fatalf("notices = %+v", h2.ctx.Notices)
	}
}

// Opened by `mantle -r` before any engine: cancelling quits, picking starts the engine
// from the command-line options.
func TestPickerWithoutEngine(t *testing.T) {
	h := newHarness(t)
	delete(h.ctx.Engines, ext.MainEngine)
	h.f.startupSpawn = func() (ext.SpawnOpts, bool) { return ext.SpawnOpts{Cwd: "/work/demo", Model: "cli-model"}, true }
	openPicker(t, h, nil)
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	s := find[ext.EngineStartMsg](h)
	if len(s) != 1 || s[0].Opts.Resume != sidPlain || s[0].Opts.Model != "cli-model" {
		t.Fatalf("start = %+v", s)
	}

	h2 := newHarness(t)
	delete(h2.ctx.Engines, ext.MainEngine)
	openPicker(t, h2, nil)
	h2.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if ex := find[ext.ExitMsg](h2); len(ex) != 1 || ex[0].Code != 0 {
		t.Fatalf("exit = %v", h2.out)
	}
}

func TestStoriesFitWidths(t *testing.T) {
	h := newHarness(t)
	ctx := exttest.NewCtx()
	for _, s := range h.r.Stories {
		for _, w := range []int{40, 60, 100, 160} {
			out := s.Render(ctx, ext.Area{Width: w})
			for _, line := range strings.Split(out.Text, "\n") {
				if ansi.StringWidth(line) > w {
					t.Fatalf("%s at %d: line too wide: %q", s.ID, w, ansi.Strip(line))
				}
			}
			if strings.TrimSpace(out.Text) == "" {
				t.Fatalf("%s at %d is empty", s.ID, w)
			}
		}
	}
}

func TestPickerStoryFitsWidths(t *testing.T) {
	ctx := exttest.NewCtx()
	for _, w := range []int{40, 60, 100, 160} {
		out := pickerStory(ctx, ext.Area{Width: w}).Text
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > w {
				t.Fatalf("width %d: line too wide (%d): %q", w, ansi.StringWidth(line), ansi.Strip(line))
			}
		}
		if !strings.Contains(ansi.Strip(out), "My renamed session") {
			t.Fatalf("story at %d lacks rows", w)
		}
	}
}
