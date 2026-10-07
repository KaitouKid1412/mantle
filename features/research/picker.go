package research

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui"
)

// picker is the research.tree dialog: every node as an indented tree, filtered by
// typing. Enter shows the highlighted node.
type picker struct {
	f   *feature
	sel *ui.Select
}

var (
	_ ext.Dialog        = (*picker)(nil)
	_ ext.ActionHandler = (*picker)(nil)
)

func (f *feature) openPicker(ctx ext.Ctx, _ any) (ext.Dialog, error) {
	p := &picker{f: f}
	p.sel = ui.NewSelect(TreeID, pickerItems(f.tree, f.viewedID())...)
	p.sel.Filterable = true
	p.sel.OnAccept = func(ctx ext.Ctx, _ int, it ui.Item) tea.Cmd {
		return tea.Sequence(ctx.CloseDialog(TreeID), f.navigate(ctx, it.Value.(string)))
	}
	p.sel.OnCancel = func(ctx ext.Ctx) tea.Cmd { return ctx.CloseDialog(TreeID) }
	for i, it := range p.sel.Items {
		if it.Value == f.viewedID() {
			p.sel.SetCurrent(i)
		}
	}
	return p, nil
}

// viewedID is the ID of the node on screen ("" with no tree).
func (f *feature) viewedID() string {
	if n := f.node(); n != nil {
		return n.ID
	}
	return ""
}

// pickerItems lists the tree depth-first, indented two cells per level.
func pickerItems(t *research.Tree, viewing string) []ui.Item {
	var items []ui.Item
	t.Walk(func(n *research.Node) bool {
		var desc []string
		if n.ID == viewing {
			desc = append(desc, "viewing")
		}
		if n.ID == t.EngineLeaf {
			desc = append(desc, "● current")
		}
		if n.Quote != "" {
			desc = append(desc, `re "`+truncRunes(oneLine(n.Quote), 24)+`"`)
		}
		items = append(items, ui.Item{
			Label:       strings.Repeat("  ", n.Depth()) + nodeLabel(n),
			Description: strings.Join(desc, " · "),
			Value:       n.ID,
		})
		return true
	})
	return items
}

// truncRunes cuts s to n runes, with "…" when cut.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (p *picker) ID() string                      { return TreeID }
func (p *picker) Init(ext.Ctx) tea.Cmd            { return nil }
func (p *picker) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (p *picker) KeyContext() string              { return ext.ContextSelect }
func (p *picker) Placement() ext.Placement        { return ext.PlaceCentered }

func (p *picker) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	return p.sel.HandleKey(ctx, k)
}

func (p *picker) HandlePaste(ctx ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	return p.sel.HandlePaste(ctx, m)
}

func (p *picker) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	ok, cmd := p.sel.HandleAction(ctx, a)
	ctx.Invalidate(TreeID)
	return ok, cmd
}

func (p *picker) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t := ctx.Theme()
	w := max(a.Width, 20)
	inner := ui.FrameInner(w)
	h := 0
	if a.MaxHeight > 0 {
		h = max(3, a.MaxHeight-4) // frame and hint rows
	}
	body := p.sel.View(ctx, ext.Area{Width: inner, MaxHeight: h, Focused: true, Mode: a.Mode}).Text
	hints := ui.Hints(t, inner,
		ui.Hint{Keys: "enter", Label: "to view"},
		ui.Hint{Keys: "esc", Label: "to close"})
	return ext.Rendered{Text: ui.Frame(t, theme.Suggestion, "Questions", body+"\n"+hints, w)}
}
