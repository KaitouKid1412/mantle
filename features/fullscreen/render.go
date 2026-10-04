package fullscreen

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// cacheKey identifies a rendered item: the item and its children's revisions, the
// width, the view mode, the theme and whether it is expanded.
type cacheKey struct {
	rev      int
	childRev int
	width    int
	mode     ext.ViewMode
	theme    string
	expanded bool
}

type cacheEntry struct {
	key         cacheKey
	lines       []string
	collapsible bool
}

// renderer turns the transcript store into blocks, re-rendering only items whose
// cache key changed. Every top-level item becomes a block that starts with a blank
// line, exactly as the inline transcript prints it.
type renderer struct {
	cache    map[string]cacheEntry
	expanded map[string]bool
}

func newRenderer() *renderer {
	return &renderer{cache: map[string]cacheEntry{}, expanded: map[string]bool{}}
}

// childrenSource is the transcript store's child lookup (plan 03's store has it; any
// other Transcript gets children grouped by ParentID).
type childrenSource interface {
	Children(id string) []*ext.Item
}

// viewMode mirrors the transcript's view mode from settings.
func viewMode(ctx ext.Ctx) ext.ViewMode {
	s := ctx.Settings()
	vm := ext.ClaudeString(s, "viewMode", "")
	switch {
	case ext.ClaudeBool(s, "verbose", false) || vm == "verbose":
		return ext.Verbose
	case vm == "focus":
		return ext.Focus
	}
	return ext.Normal
}

// blocks renders the document at width w. collapsible reports, per block, whether a
// click can expand it.
func (r *renderer) blocks(ctx ext.Ctx, w int) (out []Block, collapsible []bool, items []*ext.Item) {
	tr := ctx.Transcript()
	if tr == nil {
		return nil, nil, nil
	}
	all := tr.Items()
	var kids map[string][]*ext.Item
	cs, hasCS := tr.(childrenSource)
	if !hasCS {
		kids = map[string][]*ext.Item{}
		for _, it := range all {
			if it.ParentID != "" {
				kids[it.ParentID] = append(kids[it.ParentID], it)
			}
		}
	}
	mode, theme, now := viewMode(ctx), ctx.Theme().Name, ctx.Clock().Now()
	seen := make(map[string]bool, len(all))
	for _, it := range all {
		if it.ParentID != "" {
			continue
		}
		seen[it.ID] = true
		var children []*ext.Item
		if hasCS {
			children = cs.Children(it.ID)
		} else {
			children = kids[it.ID]
		}
		childRev := len(children)
		for _, c := range children {
			childRev = childRev*31 + c.Rev + int(c.State)
		}
		key := cacheKey{rev: it.Rev*8 + int(it.State), childRev: childRev, width: w, mode: mode,
			theme: theme, expanded: r.expanded[it.ID]}
		e, ok := r.cache[it.ID]
		running := it.State == ext.Running || it.State == ext.Streaming
		if !ok || e.key != key || running {
			b := ctx.Renderer(it.Key)(ext.RenderCtx{Width: w, Mode: mode, Theme: ctx.Theme(),
				Expanded: key.expanded, Now: now, Children: children}, it)
			e = cacheEntry{key: key, collapsible: b.Collapsible}
			if len(b.Lines) > 0 {
				e.lines = append(make([]string, 0, len(b.Lines)+1), "")
				for _, l := range b.Lines {
					if ansi.StringWidth(l) > w {
						l = ansi.Truncate(l, w, "")
					}
					e.lines = append(e.lines, l)
				}
			}
			r.cache[it.ID] = e
		}
		if len(e.lines) == 0 {
			continue
		}
		out = append(out, Block{ID: it.ID, Lines: e.lines})
		collapsible = append(collapsible, e.collapsible)
		items = append(items, it)
	}
	for id := range r.cache { // forget items that left the store (/clear, rewind)
		if !seen[id] {
			delete(r.cache, id)
			delete(r.expanded, id)
		}
	}
	return out, collapsible, items
}
