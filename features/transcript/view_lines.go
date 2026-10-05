package transcript

import (
	"strings"

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
	rev, w     int
	children   int
	mode       ext.ViewMode
	theme      any
	cfg        renderCfg
	fullscreen bool
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
	f.fullscreen = c.Layout() == ext.Fullscreen
	defer func() { f.fullscreen = false }()
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
		if n := f.condensedRun(items, i); n > 0 {
			flush()
			ids = append(ids, "tools:"+it.ID)
			blocks = append(blocks, ext.Block{Lines: f.condensedLines(c, items[i:i+n], w), Collapsible: true})
			i += n
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
	key := viewKey{it.Rev, w, kids, f.mode(), c.Theme(), f.renderCfg(), f.fullscreen}
	if e, ok := f.views[it.ID]; ok && e.key == key && it.State.Finished() {
		return e.block
	}
	b := f.rendererFor(it.Key)(f.renderCtx(c, it, w), it)
	f.views[it.ID] = viewEntry{key, b}
	return b
}

// condensed tool kinds in the fullscreen layout: finished, successful calls of
// these tools fold into one summary line ("Ran 2 shell commands, read 1 file").
var condensedKinds = map[string][2]string{
	"Bash":       {"ran", "shell command"},
	"PowerShell": {"ran", "shell command"},
	"Read":       {"read", "file"},
	"Glob":       {"searched for", "pattern"},
	"Grep":       {"searched for", "pattern"},
	"LS":         {"listed", "directory"},
	"WebFetch":   {"fetched", "page"},
	"WebSearch":  {"searched the web", "time"},
}

// condensedRun returns how many items from i fold into one fullscreen summary
// (0 when none, or outside the fullscreen layout or the verbose view).
func (f *Feature) condensedRun(items []*ext.Item, i int) int {
	if !f.fullscreen || verbose(ext.RenderCtx{Mode: f.mode()}) {
		return 0
	}
	n := 0
	for j := i; j < len(items); j++ {
		tu := toolUse(items[j])
		if tu == nil || items[j].ParentID != "" || items[j].State != ext.Done {
			break
		}
		if _, ok := condensedKinds[tu.Name]; !ok {
			break
		}
		n++
	}
	return n
}

// condensedLines renders a run of finished tool calls as one summary line.
func (f *Feature) condensedLines(c ext.Ctx, run []*ext.Item, w int) []string {
	type part struct {
		verb, noun string
		n          int
	}
	var parts []*part
	index := map[string]*part{}
	for _, it := range run {
		k := condensedKinds[toolUse(it).Name]
		key := k[0] + "|" + k[1]
		if p := index[key]; p != nil {
			p.n++
			continue
		}
		p := &part{k[0], k[1], 1}
		index[key] = p
		parts = append(parts, p)
	}
	var phrases []string
	for _, p := range parts {
		many := p.noun + "s"
		if p.noun == "directory" {
			many = "directories"
		}
		phrases = append(phrases, p.verb+" "+plural(p.n, p.noun, many))
	}
	text := strings.Join(phrases, ", ")
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	st := stylesFor(ext.RenderCtx{Theme: c.Theme()})
	return truncLines([]string{dotIndent + st.dim.Render(text)}, w)
}
