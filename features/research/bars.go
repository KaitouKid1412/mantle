package research

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// barRow is one row of a bar: a node, a "… N more" row (opens the tree picker), or
// the follow-up hint.
type barRow struct {
	node *research.Node
	more int
	hint bool
}

// barCap is the most rows one bar shows on a screen h rows tall.
func barCap(h int) int { return max(3, h/5) }

// ancestorRows lists the viewed node's ancestors, root first. Over the cap it keeps
// the root, then "… N more", then the nearest ancestors.
func ancestorRows(n *research.Node, limit int) []barRow {
	if n == nil {
		return nil
	}
	path := n.Path()
	anc := path[:len(path)-1]
	rows := make([]barRow, 0, min(len(anc), limit))
	if len(anc) <= limit {
		for _, a := range anc {
			rows = append(rows, barRow{node: a})
		}
		return rows
	}
	keep := limit - 2
	rows = append(rows, barRow{node: anc[0]}, barRow{more: len(anc) - 1 - keep})
	for _, a := range anc[len(anc)-keep:] {
		rows = append(rows, barRow{node: a})
	}
	return rows
}

// childRows lists the viewed node's children. Over the cap the last row is
// "… N more", and the pinned children (where going down leads, the way to the
// engine's tip) are always among the shown ones, in tree order.
func childRows(n *research.Node, pinned []*research.Node, limit int) []barRow {
	if n == nil {
		return nil
	}
	kids := n.Children
	if len(kids) == 0 {
		return []barRow{{hint: true}}
	}
	if len(kids) <= limit {
		rows := make([]barRow, len(kids))
		for i, c := range kids {
			rows[i] = barRow{node: c}
		}
		return rows
	}
	keep := map[*research.Node]bool{}
	for _, p := range pinned {
		if p != nil && p.Parent == n {
			keep[p] = true
		}
	}
	// Fill the free rows with the first unpinned children, then list in tree order.
	free := limit - 1 - len(keep)
	var shown []*research.Node
	for _, c := range kids {
		switch {
		case keep[c]:
			shown = append(shown, c)
		case free > 0:
			shown = append(shown, c)
			free--
		}
	}
	rows := make([]barRow, 0, limit)
	for _, c := range shown {
		rows = append(rows, barRow{node: c})
	}
	return append(rows, barRow{more: len(kids) - len(shown)})
}

// bar is research.ancestors (header) or research.children (above the input).
type bar struct {
	f  *feature
	id string
}

func (b *bar) ID() string           { return b.id }
func (b *bar) Init(ext.Ctx) tea.Cmd { return nil }

func (b *bar) rows(ctx ext.Ctx) []barRow { return b.f.rowsFor(ctx, b.id) }

// rowsFor lists the rows of bar id (none while research is off).
func (f *feature) rowsFor(ctx ext.Ctx, id string) []barRow {
	if !f.active || f.tree == nil {
		return nil
	}
	_, h := ctx.Size()
	n := f.node()
	if id == AncestorsID {
		return ancestorRows(n, barCap(h))
	}
	if n == nil {
		return nil
	}
	return childRows(n, []*research.Node{f.childTarget(n), f.towardTip(n)}, barCap(h))
}

func (b *bar) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	ev, ok := msg.(ext.MouseEvent)
	if !ok || !b.f.active {
		return nil
	}
	rows := b.rows(ctx)
	row := ev.Y
	switch m := ev.Msg.(type) {
	case tea.MouseMotionMsg:
		h := noHover
		if row >= 0 && row < len(rows) && !rows[row].hint {
			h = hoverRow{bar: b.id, row: row}
		}
		if h != b.f.hover {
			b.f.hover = h
			b.f.invalidate(ctx)
		}
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft || row < 0 || row >= len(rows) {
			return nil
		}
		return b.f.clickRow(ctx, rows[row])
	}
	return nil
}

// clickRow navigates to a row's node, or opens the tree picker for "… N more".
func (f *feature) clickRow(ctx ext.Ctx, r barRow) tea.Cmd {
	switch {
	case r.node != nil:
		return f.navigate(ctx, r.node.ID)
	case r.more > 0:
		return ctx.OpenDialog(TreeID, nil)
	}
	return nil
}

func (b *bar) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if a.Width <= 0 { // mounted in the fullscreen layout only (SlotOpts.Modes)
		return ext.Rendered{}
	}
	rows := b.rows(ctx)
	if len(rows) == 0 {
		return ext.Rendered{}
	}
	f := b.f
	var target *research.Node
	if b.id == ChildrenID {
		target = f.childTarget(f.node())
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		hot := f.hover == hoverRow{bar: b.id, row: i} || (r.node != nil && r.node == target && f.hover.row < 0)
		lines[i] = b.renderRow(ctx.Theme(), r, hot, a.Width)
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// renderRow draws one row in width cells:
//
//	▸ <question…> · N branches ●      (ancestor)
//	▾ re "<quote…>" <question…> ●     (child)
//	… N more
//	↳ type to ask a follow-up
func (b *bar) renderRow(t *theme.Theme, r barRow, hot bool, w int) string {
	switch {
	case r.hint:
		return t.Paint(theme.Subtle, ansi.Truncate("↳ type to ask a follow-up", w, "…"))
	case r.more > 0:
		tok := theme.Inactive
		if hot {
			tok = theme.Suggestion
		}
		return t.Paint(tok, ansi.Truncate("… "+strconv.Itoa(r.more)+" more", w, "…"))
	}
	n := r.node
	glyph := "▸ "
	if b.id == ChildrenID {
		glyph = "▾ "
	}
	var suffix []string
	if k := len(n.Children); b.id == AncestorsID && k > 1 {
		suffix = append(suffix, t.Paint(theme.Inactive, " · "+strconv.Itoa(k)+" branches"))
	}
	switch n.State {
	case research.Running:
		suffix = append(suffix, t.Paint(theme.Inactive, " · answering"))
	case research.Interrupted:
		suffix = append(suffix, t.Paint(theme.Inactive, " · interrupted"))
	case research.Failed:
		suffix = append(suffix, t.Paint(theme.Error, " · failed"))
	}
	if n.ID == b.f.tree.EngineLeaf {
		suffix = append(suffix, t.Paint(theme.Success, " ●"))
	}
	tail := strings.Join(suffix, "")
	room := max(1, w-ansi.StringWidth(glyph)-ansi.StringWidth(tail))

	label := nodeLabel(n)
	if b.id == ChildrenID && n.Quote != "" {
		q := oneLine(n.Quote)
		q = ansi.Truncate(q, max(8, min(32, room/3)), "…")
		label = `re "` + q + `" ` + label
	}
	label = ansi.Truncate(label, room, "…")

	tok, gtok := theme.Text, theme.Inactive
	if hot {
		tok, gtok = theme.Suggestion, theme.Suggestion
	}
	return ansi.Truncate(t.Paint(gtok, glyph)+t.Paint(tok, label)+tail, w, "")
}

// nodeLabel is a node's one-line question.
func nodeLabel(n *research.Node) string {
	if n.Synthetic {
		return "earlier conversation"
	}
	if q := oneLine(n.Prompt); q != "" {
		return q
	}
	return "(no text)"
}

// oneLine collapses s's whitespace, newlines included, into single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
