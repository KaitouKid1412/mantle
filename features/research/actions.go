package research

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Actions, in the ambient Research context (active only in research mode). Moving
// between nodes only changes the view; it never touches the engine.
const (
	ActParent      ext.ActionID = "mantle:research.parent"
	ActChild       ext.ActionID = "mantle:research.child"
	ActPrevSibling ext.ActionID = "mantle:research.prevSibling"
	ActNextSibling ext.ActionID = "mantle:research.nextSibling"
	ActRoot        ext.ActionID = "mantle:research.root"
	ActTip         ext.ActionID = "mantle:research.tip"
	ActTree        ext.ActionID = "mantle:research.tree"
	ActQuote       ext.ActionID = "mantle:research.quote"
	ActToggle      ext.ActionID = "mantle:research.toggle"
)

func (f *feature) addActions(r ext.Registrar) {
	// nav wraps a move: handled only while research is on, so the keys fall through
	// otherwise (and at the tree's edges, where there is nowhere to go).
	nav := func(move func(ext.Ctx) tea.Cmd) ext.ActionFunc {
		return func(ctx ext.Ctx) (bool, tea.Cmd) {
			if !f.active || f.tree == nil {
				return false, nil
			}
			return true, move(ctx)
		}
	}
	for _, a := range []struct {
		id   ext.ActionID
		keys string
		desc string
		run  ext.ActionFunc
	}{
		{ActParent, "alt+up", "Go to the parent question", nav(f.goParent)},
		{ActChild, "alt+down", "Go to the last visited (or first) follow-up", nav(f.goChild)},
		{ActPrevSibling, "alt+left", "Go to the previous sibling question", nav(func(c ext.Ctx) tea.Cmd { return f.goSibling(c, -1) })},
		{ActNextSibling, "alt+right", "Go to the next sibling question", nav(func(c ext.Ctx) tea.Cmd { return f.goSibling(c, 1) })},
		{ActRoot, "alt+home", "Go to the first question", nav(f.goRoot)},
		{ActTip, "alt+end", "Go to Claude's current question", nav(f.goTip)},
		{ActTree, "ctrl+t", "Open the question tree", nav(func(c ext.Ctx) tea.Cmd { return c.OpenDialog(TreeID, nil) })},
		{ActQuote, ">", "Quote the selection into the prompt", f.quote},
	} {
		r.AddAction(ext.Action{ID: a.id, Context: ext.ContextResearch, Description: a.desc, Run: a.run})
		r.AddBinding(ext.Binding{Context: ext.ContextResearch, Keys: a.keys, Action: a.id})
	}
	// No default key: shift+tab and /research are the usual ways in.
	r.AddAction(ext.Action{ID: ActToggle, Context: ext.ContextMantle, Description: "Enter or leave research mode",
		Run: func(ctx ext.Ctx) (bool, tea.Cmd) {
			mode := ext.UIModeResearch
			if f.active {
				mode = ""
			}
			return true, ext.Msg(ext.UIModeRequestMsg{Mode: mode})
		}})
}

// quote puts the viewport's selection into the prompt as a "> " block. Without a
// selection it declines, so ">" types as usual.
func (f *feature) quote(ctx ext.Ctx) (bool, tea.Cmd) {
	if !f.active || f.selection == "" {
		return false, nil
	}
	q := research.FormatQuote(f.selection)
	if q == "" {
		return false, nil
	}
	return true, ext.Msg(ext.EditorQuoteMsg{Text: q})
}
