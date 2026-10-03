package transcript

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// ViewerDialogID is the ctrl+o transcript viewer's dialog ID.
const ViewerDialogID = "dialog.transcript"

// viewerEditedMsg reports that the external editor closed.
type viewerEditedMsg struct{ err error }

// viewer is the ctrl+o transcript viewer: the whole store in an alt-screen
// view, every item at full detail, with less-style navigation and search.
type viewer struct {
	f       *Feature
	showAll bool // ctrl+e: expand every collapsible item

	lines  []string // rendered body
	plain  []string // lines without styles (search)
	key    viewerKey
	top    int  // first visible body line
	follow bool // stick to the bottom until the user scrolls up

	searching bool   // typing a query
	query     string // last confirmed (or in-progress) query
	matches   []int  // body line indexes matching query
	cur       int    // current match
	note      string // transient status text
}

type viewerKey struct {
	rev, width int
	showAll    bool
	theme      any
}

func (f *Feature) newViewer(c ext.Ctx, _ any) (ext.Dialog, error) {
	return &viewer{f: f, follow: true}, nil
}

func (v *viewer) ID() string               { return ViewerDialogID }
func (v *viewer) Init(ext.Ctx) tea.Cmd     { return nil }
func (v *viewer) KeyContext() string       { return ext.ContextTranscript }
func (v *viewer) Placement() ext.Placement { return ext.PlaceAltScreen }
func (v *viewer) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	if v.searching {
		v.query += oneLine(p.Content)
		c.Invalidate(v.ID())
	}
	return true, nil
}

func (v *viewer) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.EngineEventMsg:
		c.Invalidate(v.ID()) // the store may have changed
	case viewerEditedMsg:
		if m.err != nil {
			v.note = "Editor: " + m.err.Error()
		}
		c.Invalidate(v.ID())
	}
	return nil
}

// HandleAction implements ext.ActionHandler for the Transcript context.
func (v *viewer) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActTranscriptExit, ext.ActAppToggleTranscript:
		if v.searching && a == ext.ActTranscriptExit {
			v.searching = false
			c.Invalidate(v.ID())
			return true, nil
		}
		return true, c.CloseDialog(v.ID())
	case ext.ActTranscriptToggleShowAll:
		v.showAll = !v.showAll
		c.Invalidate(v.ID())
		return true, nil
	}
	return false, nil
}

func (v *viewer) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	defer c.Invalidate(v.ID())
	_, h := c.Size()
	page := max(h-2, 1)
	v.note = ""
	key := k.String()
	if v.searching {
		switch key {
		case "enter":
			v.searching = false
			v.search(true)
		case "esc", "escape":
			v.searching = false
		case "backspace":
			if r := []rune(v.query); len(r) > 0 {
				v.query = string(r[:len(r)-1])
			}
		default:
			if k.Text != "" {
				v.query += k.Text
			}
		}
		return true, nil
	}
	switch key {
	case "j", "down", "ctrl+n", "enter":
		v.scroll(1)
	case "k", "up", "ctrl+p":
		v.scroll(-1)
	case "ctrl+d":
		v.scroll(page / 2)
	case "ctrl+u":
		v.scroll(-page / 2)
	case "ctrl+f", "space", "pgdown", "f":
		v.scroll(page)
	case "ctrl+b", "b", "pgup":
		v.scroll(-page)
	case "g", "home":
		v.top, v.follow = 0, false
	case "G", "shift+g", "end":
		v.top, v.follow = len(v.lines), true
	case "/":
		v.searching, v.query = true, ""
	case "n":
		v.next(1)
	case "N", "shift+n":
		v.next(-1)
	case "v":
		return true, v.openEditor(c)
	default:
		return false, nil
	}
	return true, nil
}

func (v *viewer) scroll(n int) {
	v.top = max(v.top+n, 0)
	v.follow = false
}

// search finds the query in the body and jumps to the first match at or
// after the top line.
func (v *viewer) search(jump bool) {
	v.matches = nil
	q := strings.ToLower(v.query)
	if q == "" {
		return
	}
	for i, l := range v.plain {
		if strings.Contains(strings.ToLower(l), q) {
			v.matches = append(v.matches, i)
		}
	}
	if len(v.matches) == 0 {
		v.note = "Pattern not found: " + v.query
		return
	}
	if jump {
		v.cur = 0
		for i, m := range v.matches {
			if m >= v.top {
				v.cur = i
				break
			}
		}
		v.top, v.follow = v.matches[v.cur], false
	}
}

func (v *viewer) next(dir int) {
	if len(v.matches) == 0 {
		v.search(true)
		return
	}
	v.cur = (v.cur + dir + len(v.matches)) % len(v.matches)
	v.top, v.follow = v.matches[v.cur], false
}

// build renders the whole store for the viewer (cached by store revision,
// width and expansion).
func (v *viewer) build(c ext.Ctx, w int) {
	k := viewerKey{v.f.store.Rev(), w, v.showAll, c.Theme()}
	if k == v.key && v.lines != nil {
		return
	}
	v.key = k
	v.lines = v.f.transcriptLines(c, w, v.showAll)
	v.plain = make([]string, len(v.lines))
	for i, l := range v.lines {
		v.plain[i] = render.Strip(l)
	}
	if v.query != "" {
		v.search(false)
	}
}

func (v *viewer) View(c ext.Ctx, a ext.Area) ext.Rendered {
	w, h := a.Width, a.MaxHeight
	if h <= 0 {
		_, h = c.Size()
	}
	v.build(c, w)
	st := stylesFor(ext.RenderCtx{Theme: c.Theme()})
	body := max(h-2, 1)
	maxTop := max(len(v.lines)-body, 0)
	if v.follow || v.top > maxTop {
		v.top = maxTop
	}

	expand := "ctrl+e"
	if keys := c.KeysFor(ext.ContextTranscript, ext.ActTranscriptToggleShowAll); len(keys) > 0 {
		expand = keys[0]
	}
	verb := "expand all"
	if v.showAll {
		verb = "collapse"
	}
	head := st.bold.Render("Transcript") + st.dim.Render(" · "+expand+" to "+verb+" · / search · v editor · q to exit")
	out := []string{render.Truncate(head, w, "…")}

	matchLine := -1
	if len(v.matches) > 0 {
		matchLine = v.matches[v.cur]
	}
	for i := v.top; i < v.top+body; i++ {
		switch {
		case i >= len(v.lines):
			out = append(out, "")
		case i == matchLine || (v.query != "" && v.isMatch(i)):
			out = append(out, render.Truncate(highlight(v.plain[i], v.query, i == matchLine), w, "…"))
		default:
			out = append(out, v.lines[i])
		}
	}

	var status string
	switch {
	case v.searching:
		status = "/" + v.query + "█"
	case v.note != "":
		status = st.warn.Render(v.note)
	default:
		end := min(v.top+body, len(v.lines))
		status = st.dim.Render(fmt.Sprintf("lines %d–%d of %d", min(v.top+1, end), end, len(v.lines)))
		if len(v.matches) > 0 {
			status += st.dim.Render(fmt.Sprintf(" · match %d/%d (n/N)", v.cur+1, len(v.matches)))
		}
	}
	out = append(out, render.Truncate(status, w, "…"))
	return ext.Rendered{Text: strings.Join(out, "\n")}
}

func (v *viewer) isMatch(i int) bool {
	for _, m := range v.matches {
		if m == i {
			return true
		}
	}
	return false
}

// highlight shows a line plainly with every occurrence of q reversed (the
// current match also bold).
func highlight(line, q string, current bool) string {
	if q == "" {
		return line
	}
	hi := render.Style{Reverse: true, Bold: current}
	lower, lq := strings.ToLower(line), strings.ToLower(q)
	var b strings.Builder
	for {
		i := strings.Index(lower, lq)
		if i < 0 {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(line[:i])
		b.WriteString(hi.Render(line[i : i+len(q)]))
		line, lower = line[i+len(q):], lower[i+len(q):]
	}
}

// transcriptLines renders every item at full detail, each under a dim line
// with its time and (for assistant output) the model.
func (f *Feature) transcriptLines(c ext.Ctx, w int, showAll bool) []string {
	st := stylesFor(ext.RenderCtx{Theme: c.Theme()})
	var out []string
	for i, it := range f.store.Items() {
		rc := f.renderCtx(c, it, w)
		rc.Mode = ext.FullTranscript
		rc.Expanded = showAll
		lines := f.rendererFor(it.Key)(rc, it).Lines
		if i < f.store.Committed() && it.ID != f.commit.partialID {
			f.forget(it.ID) // don't keep render caches for scrollback items
		}
		if len(lines) == 0 {
			continue
		}
		if meta := f.itemMeta(it); meta != "" {
			out = append(out, "", render.Truncate(st.dim.Render(meta), w, "…"))
		} else {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

// itemMeta is the viewer's header for an item: time, and the model for
// assistant output.
func (f *Feature) itemMeta(it *ext.Item) string {
	var parts []string
	switch {
	case it.Key == ext.KeyUserPrompt || it.Key == ext.KeyUserBash:
		parts = append(parts, "You")
	case it.Key == ext.KeyAssistantText || it.Key == ext.KeyAssistantThinking:
		parts = append(parts, "Claude")
		if m := f.store.Model(it.ID); m != "" {
			parts = append(parts, m)
		}
	default:
		return ""
	}
	if !it.Start.IsZero() {
		parts = append(parts, f.zoned(it.Start).Format("15:04:05"))
	}
	return strings.Join(parts, " · ")
}

// openEditor writes the transcript as plain text to a temp file and opens it
// in $EDITOR (via tea.ExecProcess, so the terminal is handed over cleanly).
func (v *viewer) openEditor(c ext.Ctx) tea.Cmd {
	w, _ := c.Size()
	var b strings.Builder
	for _, l := range v.f.transcriptLines(c, max(w, 80), true) {
		b.WriteString(strings.TrimRight(render.Strip(l), " "))
		b.WriteByte('\n')
	}
	file, err := os.CreateTemp("", "mantle-transcript-*.txt")
	if err != nil {
		v.note = "Editor: " + err.Error()
		return nil
	}
	path := file.Name()
	_, err = file.WriteString(b.String())
	file.Close()
	if err != nil {
		v.note = "Editor: " + err.Error()
		return nil
	}
	cmd, err := editor.Cmd("mantle", path)
	if err != nil {
		v.note = "Editor: " + err.Error()
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		os.Remove(path)
		return ext.AddressedMsg{To: ViewerDialogID, Msg: viewerEditedMsg{err}}
	})
}

// registerViewer adds the dialog and the ctrl+o action.
func (f *Feature) registerViewer(r ext.Registrar) {
	r.AddDialog(ViewerDialogID, f.newViewer)
	r.AddAction(ext.Action{
		ID: ext.ActAppToggleTranscript, Context: ext.ContextGlobal,
		Description: "Show the full transcript",
		Run: func(c ext.Ctx) (bool, tea.Cmd) {
			return true, c.OpenDialog(ViewerDialogID, nil)
		},
	})
}

var _ ext.ActionHandler = (*viewer)(nil)
