package research

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// feature is research mode's view state. Everything here runs on the UI goroutine.
// It holds node IDs, never node pointers: a rebuilt tree has new nodes.
type feature struct {
	tree   *research.Tree
	active bool
	// viewing is the ID of the node on screen ("" = the engine's tip).
	viewing string
	// lastChild remembers, per node ID, the child last visited from it, so going down
	// returns where the user came from. Key "" holds the last visited root.
	lastChild map[string]string
	// hover is the bar row under the pointer.
	hover hoverRow
	// selection is the viewport's finished selection ("" = none), for the quote action.
	selection string

	// load normalizes an off-branch node's items from the session JSONL (set by the
	// mode wiring; nil shows off-branch nodes empty).
	load   Loader
	scopes *scopeCache
}

// hoverRow is a bar row: which bar and which row.
type hoverRow struct {
	bar string
	row int
}

var noHover = hoverRow{row: -1}

// node returns the viewed node: viewing, or the engine's tip, or the first root.
func (f *feature) node() *research.Node {
	t := f.tree
	if t == nil {
		return nil
	}
	if n := t.Node(f.viewing); n != nil {
		return n
	}
	if n := t.Leaf(); n != nil {
		return n
	}
	if len(t.Roots) > 0 {
		return t.Roots[0]
	}
	return nil
}

// setTree replaces the tree (after Build or Merge). The viewed node is kept by ID;
// when it is gone the view falls back to the tip. Visit memory for vanished nodes is
// dropped.
func (f *feature) setTree(ctx ext.Ctx, t *research.Tree) tea.Cmd {
	f.tree = t
	if t.Node(f.viewing) == nil {
		f.viewing = ""
	}
	for parent, child := range f.lastChild {
		if (parent != "" && t.Node(parent) == nil) || t.Node(child) == nil {
			delete(f.lastChild, parent)
		}
	}
	f.invalidate(ctx)
	return f.rescope(ctx)
}

// activate turns the view on at node viewing ("" = the tip).
func (f *feature) activate(ctx ext.Ctx, viewing string) tea.Cmd {
	f.active = true
	ctx.SetContextActive(ext.ContextResearch, true)
	if f.tree.Node(viewing) == nil {
		viewing = ""
	}
	if viewing == "" {
		if n := f.node(); n != nil {
			viewing = n.ID
		}
	}
	return f.navigate(ctx, viewing)
}

// deactivate turns the view off and removes the transcript scope.
func (f *feature) deactivate(ctx ext.Ctx) tea.Cmd {
	if !f.active {
		return nil
	}
	f.active = false
	f.hover = noHover
	ctx.SetContextActive(ext.ContextResearch, false)
	f.invalidate(ctx)
	return ext.Msg(ext.TranscriptScopeMsg{Owner: FeatureID})
}

func (f *feature) invalidate(ctx ext.Ctx) {
	ctx.Invalidate(AncestorsID)
	ctx.Invalidate(ChildrenID)
}

// navigate shows node id. It only changes the view; the engine is never touched.
func (f *feature) navigate(ctx ext.Ctx, id string) tea.Cmd {
	n := f.tree.Node(id)
	if n == nil {
		return nil
	}
	// Remember the whole path, so going down from any ancestor retraces it.
	c := n
	for ; c.Parent != nil; c = c.Parent {
		f.lastChild[c.Parent.ID] = c.ID
	}
	f.lastChild[""] = c.ID
	f.viewing = n.ID
	f.hover = noHover
	f.invalidate(ctx)
	return f.rescope(ctx)
}

// siblings returns n's siblings, n included, in order.
func (f *feature) siblings(n *research.Node) []*research.Node {
	if n.Parent != nil {
		return n.Parent.Children
	}
	return f.tree.Roots
}

// childTarget is the child going down leads to: the last visited one, else the first.
func (f *feature) childTarget(n *research.Node) *research.Node {
	if n == nil || len(n.Children) == 0 {
		return nil
	}
	if id, ok := f.lastChild[n.ID]; ok {
		for _, c := range n.Children {
			if c.ID == id {
				return c
			}
		}
	}
	return n.Children[0]
}

// Navigation moves; each returns nil when there is nowhere to go.

func (f *feature) goParent(ctx ext.Ctx) tea.Cmd {
	n := f.node()
	if n == nil || n.Parent == nil {
		return nil
	}
	return f.navigate(ctx, n.Parent.ID)
}

func (f *feature) goChild(ctx ext.Ctx) tea.Cmd {
	c := f.childTarget(f.node())
	if c == nil {
		return nil
	}
	return f.navigate(ctx, c.ID)
}

func (f *feature) goSibling(ctx ext.Ctx, delta int) tea.Cmd {
	n := f.node()
	if n == nil {
		return nil
	}
	sibs := f.siblings(n)
	for i, s := range sibs {
		if s == n {
			j := i + delta
			if j < 0 || j >= len(sibs) {
				return nil
			}
			return f.navigate(ctx, sibs[j].ID)
		}
	}
	return nil
}

func (f *feature) goRoot(ctx ext.Ctx) tea.Cmd {
	n := f.node()
	if n == nil {
		return nil
	}
	return f.navigate(ctx, n.Path()[0].ID)
}

func (f *feature) goTip(ctx ext.Ctx) tea.Cmd {
	if l := f.tree.Leaf(); l != nil {
		return f.navigate(ctx, l.ID)
	}
	return nil
}

// towardTip returns n's child on the way to the engine's tip, or nil.
func (f *feature) towardTip(n *research.Node) *research.Node {
	for c := f.tree.Leaf(); c != nil; c = c.Parent {
		if c.Parent == n && n != nil {
			return c
		}
	}
	return nil
}

// onBranch reports whether n is on the engine's current branch: the tip or one of its
// ancestors.
func (f *feature) onBranch(n *research.Node) bool {
	l := f.tree.Leaf()
	return l != nil && (n == l || n.IsAncestorOf(l))
}

// rescope sends the transcript scope for the viewed node (nothing while inactive).
func (f *feature) rescope(ctx ext.Ctx) tea.Cmd {
	if !f.active {
		return nil
	}
	n := f.node()
	if n == nil {
		return ext.Msg(ext.TranscriptScopeMsg{Owner: FeatureID, Source: newStaticScope(nil)})
	}
	if live := ctx.Transcript(); live != nil && f.onBranch(n) && live.Get(promptItemID(n.ID)) != nil {
		return ext.Msg(ext.TranscriptScopeMsg{Owner: FeatureID, Source: liveScope{src: live, nodeID: n.ID}})
	}
	s, load := f.scopes.get(scopeKey{node: n.ID, leaf: n.LeafUUID}, f.load)
	if s == nil {
		// Loading: show the node empty until its items arrive, never another node's.
		s = newStaticScope(nil)
	}
	return tea.Batch(ext.Msg(ext.TranscriptScopeMsg{Owner: FeatureID, Source: s}), load)
}

// scopeLoaded stores an off-branch load and shows it if that node is still on screen.
func (f *feature) scopeLoaded(ctx ext.Ctx, m scopeLoadedMsg) tea.Cmd {
	if m.err != nil {
		f.scopes.failed(m.key)
		ctx.Log().Warn("research: loading node", "node", m.key.node, "err", m.err)
		return ctx.Notify(ext.Notice{Key: "research.load", Text: "Could not load this question's answer", Level: ext.NoticeWarning, Source: FeatureID})
	}
	s := f.scopes.loaded(m)
	n := f.node()
	if !f.active || n == nil || n.ID != m.key.node || n.LeafUUID != m.key.leaf {
		return nil
	}
	return ext.Msg(ext.TranscriptScopeMsg{Owner: FeatureID, Source: s})
}
