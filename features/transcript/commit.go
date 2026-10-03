package transcript

import (
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// commitState is the commit policy's memory between updates.
type commitState struct {
	partialID string // text item at the watermark whose closed blocks are printed
	partial   int    // how many of its markdown blocks are printed
	hidden    int    // tool calls skipped by the focus view since the last summary
}

// commitReady prints every finished item at the watermark into scrollback, plus
// the closed markdown blocks of a text item still streaming there. Committed
// lines are never re-rendered. Only inline layout commits; fullscreen keeps
// everything in the store.
func (f *Feature) commitReady(c ext.Ctx) tea.Cmd {
	if c.Layout() != ext.Inline {
		return nil
	}
	tw, _ := c.Size()
	w := printWidth(tw)
	var out []string
	for _, n := range f.store.TakeNotes() {
		if n == NoteReplaced {
			out = append(out, "", stylesFor(f.renderCtx(c, &ext.Item{}, w)).dim.Render("(response replaced)"))
			if f.store.Get(f.commit.partialID) == nil {
				f.commit = commitState{}
			}
		}
	}

	items := f.store.Items()
	i := f.store.Committed()
	for i < len(items) {
		it := items[i]
		if f.hidden(it) {
			if !it.State.Finished() {
				break // keep the order of what follows
			}
			if isTool(it) {
				f.commit.hidden++
			}
			f.forget(it.ID)
			i++
			continue
		}
		if run := f.mcpRun(items, i); run > 1 || (run == 1 && f.mcpHeld(items, i)) {
			if f.mcpHeld(items, i) || !allFinished(items[i:i+run]) {
				break
			}
			out = f.flushHidden(c, out, w)
			out = appendItem(out, f.groupLines(c, items[i:i+run], w))
			for _, g := range items[i : i+run] {
				f.forget(g.ID)
			}
			i += run
			continue
		}
		if it.State.Finished() {
			if lines := f.finishLines(c, it, w); len(lines) > 0 {
				out = f.flushHidden(c, out, w)
				out = appendChunk(out, lines, f.commit.partialID != it.ID)
			}
			if f.commit.partialID == it.ID {
				f.commit.partialID, f.commit.partial = "", 0
			}
			f.forget(it.ID)
			i++
			continue
		}
		if it.Key == ext.KeyAssistantText {
			if chunk, first := f.progressive(c, it, w); len(chunk) > 0 {
				out = f.flushHidden(c, out, w)
				out = appendChunk(out, chunk, first)
			}
		}
		break
	}
	f.store.SetCommitted(i)
	c.Invalidate(LiveID)
	if len(out) == 0 {
		return nil
	}
	return c.Print(strings.Join(out, "\n"))
}

// printWidth is the width items are rendered at for scrollback (and the live
// area, so committing never reflows): one less than the terminal, because a
// line filling the last column loses its last cell when printed above the
// live frame.
func printWidth(termWidth int) int { return max(1, termWidth-1) }

// hidden reports whether the view mode leaves an item out: focus shows
// prompts, answers and a summary of tool use; brief shows prompts, answers and
// messages sent to the user.
func (f *Feature) hidden(it *ext.Item) bool {
	m := f.mode()
	if m != ext.Focus && m != ext.Brief {
		return false
	}
	switch it.Key {
	case ext.KeyUserPrompt, ext.KeyUserBash, ext.KeyAssistantText, ext.KeySystemError,
		ext.KeySystemLocalCommand, ext.KeySystemCompactBoundary, ext.ToolKey("SendUserMessage"):
		return false
	case KeyResult:
		return m == ext.Brief
	}
	return true
}

func isTool(it *ext.Item) bool { return strings.HasPrefix(string(it.Key), "tool.") }

// hiddenSummary is the focus view's stand-in for skipped tool calls.
func (f *Feature) hiddenSummary(c ext.Ctx, n, w int, running bool) []string {
	st := stylesFor(ext.RenderCtx{Theme: c.Theme()})
	verb := "Used "
	if running {
		verb = "Using "
	}
	return truncLines([]string{st.dim.Render(glyphDot + " " + verb + plural(n, "tool", "tools") + " (ctrl+o to see them)")}, w)
}

// flushHidden prints the pending focus-view summary before a visible item.
func (f *Feature) flushHidden(c ext.Ctx, out []string, w int) []string {
	if f.commit.hidden == 0 || f.mode() != ext.Focus {
		f.commit.hidden = 0
		return out
	}
	out = appendItem(out, f.hiddenSummary(c, f.commit.hidden, w, false))
	f.commit.hidden = 0
	return out
}

// appendItem adds a whole item's lines with a blank line before it.
func appendItem(out, lines []string) []string { return appendChunk(out, lines, true) }

// appendChunk adds lines; a new item starts with a blank line.
func appendChunk(out, lines []string, newItem bool) []string {
	if len(lines) == 0 {
		return out
	}
	if newItem {
		out = append(out, "")
	}
	return append(out, lines...)
}

// finishLines renders a finished item for scrollback: the resolved renderer, or
// for a partly printed text item, its remaining blocks.
func (f *Feature) finishLines(c ext.Ctx, it *ext.Item, w int) []string {
	rc := f.renderCtx(c, it, w)
	if f.commit.partialID == it.ID {
		blocks, _ := f.textBlocks(rc, it)
		return flatten(blocks[min(f.commit.partial, len(blocks)):])
	}
	return f.rendererFor(it.Key)(rc, it).Lines
}

// progressive prints the newly closed markdown blocks of a streaming text item
// at the watermark. first reports whether this starts the item. It does
// nothing when a mod changed how assistant text renders.
func (f *Feature) progressive(c ext.Ctx, it *ext.Item, w int) (lines []string, first bool) {
	rc := f.renderCtx(c, it, w)
	blocks, nclosed := f.textBlocks(rc, it)
	if f.commit.partialID != it.ID {
		if !reflect.DeepEqual(flatten(blocks), f.rendererFor(it.Key)(rc, it).Lines) {
			return nil, false // replaced or wrapped renderer: commit whole items only
		}
		if nclosed == 0 {
			return nil, false
		}
		f.commit.partialID, f.commit.partial = it.ID, 0
		first = true
	}
	if nclosed <= f.commit.partial {
		return nil, false
	}
	lines = flatten(blocks[f.commit.partial:nclosed])
	f.commit.partial = nclosed
	if first && len(lines) == 0 {
		// Only empty blocks so far: the item has not visibly started.
		f.commit.partialID = ""
		f.commit.partial = 0
		return nil, false
	}
	return lines, first
}

func flatten(blocks [][]string) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

func allFinished(items []*ext.Item) bool {
	for _, it := range items {
		if !it.State.Finished() {
			return false
		}
	}
	return true
}

// mcpRun returns the length of the run of calls to the same MCP server that
// starts at i (0 if items[i] is not an MCP call). Verbose views never group.
func (f *Feature) mcpRun(items []*ext.Item, i int) int {
	if f.mode() == ext.Verbose || !isMCP(items[i]) {
		return 0
	}
	server, _ := mcpNames(items[i], toolUse(items[i]))
	n := 1
	for j := i + 1; j < len(items) && isMCP(items[j]); j++ {
		if s, _ := mcpNames(items[j], toolUse(items[j])); s != server {
			break
		}
		n++
	}
	return n
}

// mcpHeld reports whether the MCP run at i reaches the end of the store, so
// more calls to the same server may still join it.
func (f *Feature) mcpHeld(items []*ext.Item, i int) bool {
	return i+f.mcpRun(items, i) == len(items)
}

func isMCP(it *ext.Item) bool {
	return strings.HasPrefix(string(it.Key), "tool.mcp.")
}

// groupLines renders a run of MCP calls to one server: one line when there are
// several, the normal rendering for a single call.
func (f *Feature) groupLines(c ext.Ctx, run []*ext.Item, w int) []string {
	if len(run) == 1 {
		return f.finishLines(c, run[0], w)
	}
	rc := f.renderCtx(c, run[0], w)
	st := stylesFor(rc)
	state := ext.Done
	for _, it := range run {
		if it.State == ext.Failed {
			state = ext.Failed
		}
	}
	server, _ := mcpNames(run[0], toolUse(run[0]))
	lines := header(rc, st, bulletStyle(st, state), "Called "+server+" "+plural(len(run), "time", "times"), "")
	var names []string
	for _, it := range run {
		_, tool := mcpNames(it, toolUse(it))
		names = append(names, tool)
	}
	lines = append(lines, truncLines(result(rc, st.dim, strings.Join(names, ", ")+" (ctrl+o to expand)"), w)...)
	return lines
}

// reprint re-prints the whole store after the host cleared the screen.
func (f *Feature) reprint(c ext.Ctx) tea.Cmd {
	f.store.SetCommitted(0)
	f.store.TakeNotes()
	f.commit = commitState{}
	return f.commitReady(c)
}

// liveItems returns the rendered lines of every item not yet in scrollback,
// item by item (the partly printed text item contributes its remaining blocks).
func (f *Feature) liveItems(c ext.Ctx, w int) (chunks [][]string, running []bool) {
	items := f.store.Items()
	pending, runningTools := f.commit.hidden, false
	flush := func() {
		if pending > 0 && f.mode() == ext.Focus {
			chunks = append(chunks, append([]string{""}, f.hiddenSummary(c, pending, w, runningTools)...))
			running = append(running, runningTools)
		}
		pending, runningTools = 0, false
	}
	defer flush()
	for i := f.store.Committed(); i < len(items); i++ {
		it := items[i]
		if f.hidden(it) {
			if isTool(it) {
				pending++
				runningTools = runningTools || !it.State.Finished()
			}
			continue
		}
		var lines []string
		newItem := true
		if it.ID == f.commit.partialID {
			blocks, _ := f.textBlocks(f.renderCtx(c, it, w), it)
			lines = flatten(blocks[min(f.commit.partial, len(blocks)):])
			newItem = false
		} else {
			lines = f.rendererFor(it.Key)(f.renderCtx(c, it, w), it).Lines
		}
		if len(lines) == 0 {
			continue
		}
		flush()
		if newItem {
			lines = append([]string{""}, lines...)
		}
		chunks = append(chunks, lines)
		running = append(running, !it.State.Finished())
	}
	return chunks, running
}

// fitLive caps the live area at height rows. The newest items win; older ones
// collapse into a "+N more" line, and a single item taller than the budget
// shows its last lines.
func fitLive(chunks [][]string, running []bool, height int, more func(n int, running bool) string) []string {
	total := 0
	for _, ch := range chunks {
		total += len(ch)
	}
	if height <= 0 || total <= height {
		return flatten(chunks)
	}
	budget := height - 1 // the "+N more" line
	k := len(chunks)
	used := 0
	for k > 0 && used+len(chunks[k-1]) <= budget {
		used += len(chunks[k-1])
		k--
	}
	if k == len(chunks) { // the newest item alone is too tall: show its tail
		last := chunks[len(chunks)-1]
		hidden := len(chunks) - 1
		if hidden == 0 {
			return last[len(last)-height:]
		}
		tail := last[len(last)-budget:]
		return append([]string{more(hidden, anyTrue(running[:hidden]))}, tail...)
	}
	return append([]string{more(k, anyTrue(running[:k]))}, flatten(chunks[k:])...)
}

func anyTrue(b []bool) bool {
	for _, x := range b {
		if x {
			return true
		}
	}
	return false
}

// liveView is the SlotLive component: running and streaming items.
type liveView struct{ f *Feature }

func (l *liveView) ID() string                      { return LiveID }
func (l *liveView) Init(ext.Ctx) tea.Cmd            { return nil }
func (l *liveView) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }

func (l *liveView) View(c ext.Ctx, a ext.Area) ext.Rendered {
	if a.Mode != ext.Inline {
		return ext.Rendered{}
	}
	w := printWidth(a.Width)
	chunks, running := l.f.liveItems(c, w)
	dim := stylesFor(ext.RenderCtx{Theme: c.Theme()}).dim
	lines := fitLive(chunks, running, a.MaxHeight, func(n int, run bool) string {
		label := "… +" + plural(n, "more item", "more items")
		if run {
			label = "… +" + itoa(n) + " more running"
		}
		return dim.Render(label)
	})
	for i, ln := range lines {
		lines[i] = render.Truncate(ln, w, "…")
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
