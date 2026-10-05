package transcript

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Lines renders the transcript at width w in the current view mode, for layouts
// that draw the whole store themselves (plan 12's fullscreen viewport). It
// returns one block per visible item, in order, with that item's ID; the focus
// view's tool summaries and grouped MCP calls get the IDs "summary:<first
// hidden item>" and "group:<first call>". Rendering goes through Ctx.Renderer
// (mods applied) and is cached per item revision, so steady-state calls are
// cheap.
//
// Callers outside this package reach it through an interface of their own:
//
//	interface{ Lines(ext.Ctx, int) ([]string, []ext.Block) }
func (s *Store) Lines(c ext.Ctx, w int) (ids []string, blocks []ext.Block) {
	if s.view == nil {
		return nil, nil
	}
	return s.view(c, w)
}

// viewKey is everything besides the item that a cached rendering depends on.
type viewKey struct {
	rev, w   int
	children int
	mode     ext.ViewMode
	theme    any
	cfg      renderCfg
}

// renderCfg is the comparable part of the settings renderers read.
type renderCfg struct {
	maxProse                                       int
	noHighlight, showThinking, showDuration, stamp bool
	timeFormat, timeZone                           string
}

type viewEntry struct {
	key   viewKey
	block ext.Block
}

func (f *Feature) renderCfg() renderCfg {
	c := f.cfg
	return renderCfg{c.maxProse, c.noHighlight, c.showThinking, c.showTurnDuration, c.showTimestamps, c.timeFormat, c.timeZone}
}

// viewLines implements Store.Lines.
func (f *Feature) viewLines(c ext.Ctx, w int) (ids []string, blocks []ext.Block) {
	items := f.store.Items()
	if f.views == nil || len(f.views) > 2*len(items)+64 {
		f.views = map[string]viewEntry{} // drop entries of items that are gone
	}
	focus := f.mode() == ext.Focus
	hidden, firstHidden, running := 0, "", false
	flush := func() {
		if hidden > 0 && focus {
			ids = append(ids, "summary:"+firstHidden)
			blocks = append(blocks, ext.Block{Lines: f.hiddenSummary(c, hidden, w, running), Collapsible: true})
		}
		hidden, firstHidden, running = 0, "", false
	}
	for i := 0; i < len(items); {
		it := items[i]
		if f.hidden(it) {
			if isTool(it) {
				if hidden == 0 {
					firstHidden = it.ID
				}
				hidden++
				running = running || !it.State.Finished()
			}
			i++
			continue
		}
		if run := f.mcpRun(items, i); run > 1 && !f.mcpHeld(items, i) && allFinished(items[i:i+run]) {
			flush()
			ids = append(ids, "group:"+it.ID)
			blocks = append(blocks, ext.Block{Lines: f.groupLines(c, items[i:i+run], w), Collapsible: true})
			i += run
			continue
		}
		b := f.cachedBlock(c, it, w)
		i++
		if len(b.Lines) == 0 {
			continue
		}
		flush()
		ids = append(ids, it.ID)
		blocks = append(blocks, b)
	}
	flush()
	return ids, blocks
}

// cachedBlock renders an item through the resolved renderer, reusing the last
// rendering while nothing it depends on changed.
func (f *Feature) cachedBlock(c ext.Ctx, it *ext.Item, w int) ext.Block {
	kids := 0
	for _, k := range f.store.Children(it.ID) {
		kids += k.Rev
	}
	key := viewKey{it.Rev, w, kids, f.mode(), c.Theme(), f.renderCfg()}
	if e, ok := f.views[it.ID]; ok && e.key == key && it.State.Finished() {
		return e.block
	}
	b := f.rendererFor(it.Key)(f.renderCtx(c, it, w), it)
	f.views[it.ID] = viewEntry{key, b}
	return b
}
