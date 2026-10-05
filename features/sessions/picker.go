package sessions

import (
	"encoding/json"
	"os"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/sahilm/fuzzy"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// DialogResume is the /resume picker.
const DialogResume = "dialog.resume"

// pickerContext is the picker's keybinding context. It is mantle's own: Claude Code's
// Select context binds j/k, which must type into the search box here.
const pickerContext = "ResumePicker"

// PickerArgs opens the picker with a search query.
type PickerArgs struct {
	Query string
}

func (f *feature) registerPicker(r ext.Registrar) {
	r.AddDialog(DialogResume, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		var a PickerArgs
		switch v := args.(type) {
		case PickerArgs:
			a = v
		case *PickerArgs:
			if v != nil {
				a = *v
			}
		case string:
			a.Query = v
		}
		return newPicker(f, ctx, a), nil
	})
	r.AddStory(ext.Story{ID: "sessions.resume-picker", Render: pickerStory})
	r.AddStory(ext.Story{ID: "sessions.context-grid", Render: contextStory})
	r.AddStory(ext.Story{ID: "sessions.handoff-dialog", Render: handoffStory})
}

type pickerScope struct {
	all       bool // every project
	worktrees bool // this repository's other worktrees too
}

// picker lists sessions newest first. Typing filters; tab (or space with an empty
// search) previews; enter resumes.
type picker struct {
	f       *feature
	cwd     string
	live    string // the main engine's session
	noEng   bool   // opened before any engine ran (mantle -r)
	home    string
	scope   pickerScope
	branch  string // current git branch, for ctrl+b
	byBr    bool   // ctrl+b: only sessions on the current branch
	query   string
	list    []sessions.SessionMeta
	view    []int
	sel     int
	top     int
	gen     int
	loading bool
	err     error

	preview  *pickerPreview
	renaming bool
	rename   string
}

type pickerPreview struct {
	id      string
	lines   []string
	loading bool
	err     error
}

type pickerLoadedMsg struct {
	gen    int
	list   []sessions.SessionMeta
	branch string
	err    error
}

type pickerPreviewMsg struct {
	id    string
	lines []string
	err   error
}

type pickerRenamedMsg struct {
	id, title string
	err       error
}

func newPicker(f *feature, ctx ext.Ctx, a PickerArgs) *picker {
	home, _ := os.UserHomeDir()
	return &picker{
		f: f, cwd: f.cwd(ctx), live: ctx.Session().SessionID, noEng: ctx.Engine(ext.MainEngine) == nil,
		home: home, query: a.Query,
	}
}

func (p *picker) ID() string               { return DialogResume }
func (p *picker) KeyContext() string       { return pickerContext }
func (p *picker) Placement() ext.Placement { return ext.PlaceAltScreen }
func (p *picker) Init(ext.Ctx) tea.Cmd     { return p.load() }

// load lists the sessions for the current scope off the UI goroutine.
func (p *picker) load() tea.Cmd {
	p.gen++
	p.loading = true
	gen, cwd, scope, ix, wt := p.gen, p.cwd, p.scope, p.f.index, p.f.worktrees
	needBranch := p.branch == ""
	return func() tea.Msg {
		m := pickerLoadedMsg{gen: gen}
		switch {
		case scope.all:
			m.list, m.err = ix.All()
		case scope.worktrees:
			m.list, m.err = ix.Project(cwd, wt(cwd)...)
		default:
			m.list, m.err = ix.Project(cwd)
		}
		_ = ix.Save()
		if needBranch && cwd != "" {
			if out, err := runGit(cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
				m.branch = strings.TrimSpace(string(out))
			}
		}
		return ext.AddressedMsg{To: DialogResume, Msg: m}
	}
}

func (p *picker) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case pickerLoadedMsg:
		if m.gen != p.gen {
			return nil
		}
		p.loading, p.err = false, m.err
		p.list = m.list
		if m.branch != "" {
			p.branch = m.branch
		}
		p.refilter()
		ctx.Invalidate(DialogResume)
	case pickerPreviewMsg:
		if p.preview != nil && p.preview.id == m.id {
			p.preview.loading, p.preview.lines, p.preview.err = false, m.lines, m.err
			ctx.Invalidate(DialogResume)
		}
	case pickerRenamedMsg:
		return p.renamed(ctx, m)
	}
	return nil
}

// refilter applies the branch filter and the search, keeping recency order. When no
// session contains every search term, a fuzzy match over titles ranks the rest.
func (p *picker) refilter() {
	p.view = p.view[:0]
	var pool []int
	for i, m := range p.list {
		if p.byBr && p.branch != "" && m.GitBranch != p.branch {
			continue
		}
		pool = append(pool, i)
	}
	q := strings.TrimSpace(p.query)
	for _, i := range pool {
		if q == "" || p.list[i].Matches(q) {
			p.view = append(p.view, i)
		}
	}
	if len(p.view) == 0 && q != "" {
		src := make(fuzzySource, len(pool))
		for k, i := range pool {
			src[k] = p.list[i].Title()
		}
		for _, match := range fuzzy.FindFrom(q, src) {
			p.view = append(p.view, pool[match.Index])
		}
	}
	p.sel = min(p.sel, max(len(p.view)-1, 0))
	p.top = min(p.top, p.sel)
}

type fuzzySource []string

func (s fuzzySource) String(i int) string { return s[i] }
func (s fuzzySource) Len() int            { return len(s) }

func (p *picker) selected() (sessions.SessionMeta, bool) {
	if p.sel < 0 || p.sel >= len(p.view) {
		return sessions.SessionMeta{}, false
	}
	return p.list[p.view[p.sel]], true
}

func (p *picker) HandlePaste(ctx ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	text := oneLine(m.Content)
	if p.renaming {
		p.rename += text
	} else {
		p.query += text
		p.refilter()
	}
	ctx.Invalidate(DialogResume)
	return true, nil
}

// HandleAction claims the global actions whose keys mean something here.
func (p *picker) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActHistorySearch: // ctrl+r
		return true, p.startRename(ctx)
	case ext.ActAppInterrupt: // ctrl+c
		return true, p.cancel(ctx)
	}
	return false, nil
}

func (p *picker) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	defer ctx.Invalidate(DialogResume)
	key := k.String()
	if p.renaming {
		switch key {
		case "enter":
			return true, p.submitRename(ctx)
		case "esc":
			p.renaming = false
		case "backspace":
			p.rename = dropLastRune(p.rename)
		case "ctrl+u":
			p.rename = ""
		default:
			if t := typed(k); t != "" {
				p.rename += t
			}
		}
		return true, nil
	}
	switch key {
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "pgup":
		p.move(-p.pageSize())
	case "pgdown":
		p.move(p.pageSize())
	case "enter":
		return true, p.choose(ctx)
	case "esc":
		if p.preview != nil {
			p.preview = nil
			return true, nil
		}
		return true, p.cancel(ctx)
	case "tab":
		return true, p.togglePreview()
	case "space":
		if p.query == "" || p.preview != nil {
			return true, p.togglePreview()
		}
		p.query += " "
		p.refilter()
	case "ctrl+a":
		p.scope.all = !p.scope.all
		return true, p.load()
	case "ctrl+w":
		p.scope.worktrees = !p.scope.worktrees
		if !p.scope.all {
			return true, p.load()
		}
	case "ctrl+b":
		p.byBr = !p.byBr
		p.refilter()
	case "backspace":
		p.query = dropLastRune(p.query)
		p.refilter()
	case "ctrl+u":
		p.query = ""
		p.refilter()
	default:
		t := typed(k)
		if t == "" {
			return true, nil
		}
		p.query += t
		p.preview = nil
		p.refilter()
	}
	return true, nil
}

// typed is the printable text of a key press ("" for control keys).
func typed(k tea.KeyPressMsg) string {
	if k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper) != 0 {
		return ""
	}
	return k.Text
}

func dropLastRune(s string) string {
	if s == "" {
		return s
	}
	_, n := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-n]
}

func (p *picker) move(d int) {
	if len(p.view) == 0 {
		return
	}
	p.sel = min(max(p.sel+d, 0), len(p.view)-1)
	if p.preview != nil {
		p.preview = nil
	}
}

func (p *picker) pageSize() int { return 8 }

// choose resumes the selected session.
func (p *picker) choose(ctx ext.Ctx) tea.Cmd {
	m, ok := p.selected()
	if !ok {
		return nil
	}
	if m.ID == p.live && !p.noEng {
		return ctx.CloseDialog(DialogResume)
	}
	return tea.Sequence(ctx.CloseDialog(DialogResume), p.f.switchTo(ctx, m, switchOpts{}))
}

// cancel closes the picker. Opened by `mantle -r` before any engine ran, cancelling
// quits, as `claude -r` does.
func (p *picker) cancel(ctx ext.Ctx) tea.Cmd {
	if p.noEng && ctx.Engine(ext.MainEngine) == nil {
		return tea.Sequence(ctx.CloseDialog(DialogResume), ext.Msg(ext.ExitMsg{Code: 0, Reason: "no conversation selected"}))
	}
	return ctx.CloseDialog(DialogResume)
}

func (p *picker) togglePreview() tea.Cmd {
	m, ok := p.selected()
	if !ok {
		return nil
	}
	if p.preview != nil && p.preview.id == m.ID {
		p.preview = nil
		return nil
	}
	p.preview = &pickerPreview{id: m.ID, loading: true}
	path := m.Path
	return func() tea.Msg {
		lines, err := previewLines(path, 16)
		return ext.AddressedMsg{To: DialogResume, Msg: pickerPreviewMsg{id: m.ID, lines: lines, err: err}}
	}
}

// previewLines is the tail of a conversation as "you:"/"claude:" lines.
func previewLines(path string, n int) ([]string, error) {
	tr, err := sessions.Load(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, it := range Normalize(tr, NormalizeOptions{}) {
		if it.ParentID != "" {
			continue
		}
		switch d := it.Data.(type) {
		case *proto.User:
			if it.Key == ext.KeyUserPrompt {
				out = append(out, "> "+oneLine(d.Message.Content.PlainText()))
			}
		case *proto.ContentBlock:
			if it.Key == ext.KeyAssistantText {
				out = append(out, "  "+oneLine(d.Text))
			}
		case *proto.ToolUse:
			out = append(out, "  · "+d.Name)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

func (p *picker) startRename(ctx ext.Ctx) tea.Cmd {
	m, ok := p.selected()
	if !ok {
		return nil
	}
	p.renaming = true
	p.rename = m.CustomTitle
	ctx.Invalidate(DialogResume)
	return nil
}

// submitRename renames through the engine (rename_session with the session id), which
// writes the title into that session's transcript; mantle never writes transcripts.
func (p *picker) submitRename(ctx ext.Ctx) tea.Cmd {
	m, ok := p.selected()
	p.renaming = false
	title := strings.TrimSpace(p.rename)
	if !ok || title == "" {
		return nil
	}
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil || !eng.Supports(proto.SubRenameSession) {
		return notice(ctx, "rename", "Renaming needs a running Claude session", ext.NoticeWarning)
	}
	req := proto.RenameSessionRequest{Title: title}
	if m.ID != p.live {
		req.SessionID = m.ID
	}
	id := m.ID
	return controlCmd(eng.Control(proto.SubRenameSession, req), func(r ext.ControlResultMsg) tea.Msg {
		return ext.AddressedMsg{To: DialogResume, Msg: pickerRenamedMsg{id: id, title: title, err: r.Err}}
	})
}

func (p *picker) renamed(ctx ext.Ctx, m pickerRenamedMsg) tea.Cmd {
	if m.err != nil {
		return notice(ctx, "rename", "Rename failed: "+m.err.Error(), ext.NoticeError)
	}
	for i := range p.list {
		if p.list[i].ID == m.id {
			p.list[i].CustomTitle = m.title
		}
	}
	p.f.titles[m.id] = m.title
	p.f.names[m.id] = m.title
	ctx.Invalidate(DialogResume)
	cmds := []tea.Cmd{notice(ctx, "rename", "Renamed to "+m.title, ext.NoticeSuccess)}
	if m.id == p.live {
		info := ctx.Session()
		info.Title = m.title
		cmds = append(cmds, ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: info}))
	}
	return tea.Batch(cmds...)
}

// controlCmd runs an engine control Cmd and converts its result with wrap, so the
// reply reaches the code that asked (other messages pass through).
func controlCmd(cmd tea.Cmd, wrap func(ext.ControlResultMsg) tea.Msg) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if r, ok := msg.(ext.ControlResultMsg); ok {
			return wrap(r)
		}
		return msg
	}
}

// decodeControl unmarshals a control reply into out.
func decodeControl(r ext.ControlResultMsg, out any) error {
	if r.Err != nil {
		return r.Err
	}
	if len(r.Resp) == 0 {
		return nil
	}
	return json.Unmarshal(r.Resp, out)
}

func (p *picker) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 20)
	h := a.MaxHeight
	if h <= 0 {
		h = 24
	}

	scope := "this directory"
	switch {
	case p.scope.all:
		scope = "all projects"
	case p.scope.worktrees:
		scope = "this repository's worktrees"
	}
	if p.byBr && p.branch != "" {
		scope += " · branch " + p.branch
	}
	count := ""
	if !p.loading {
		count = " (" + itoa(len(p.view)) + ")"
	}
	head := th.Fg(theme.Accent).Bold(true).Render("Resume a conversation") + th.Paint(theme.Inactive, " · "+scope+count)
	search := th.Paint(theme.Inactive, "Search: ") + p.query + th.Paint(theme.Subtle, "▏")
	if p.renaming {
		search = th.Paint(theme.Suggestion, "New name: ") + p.rename + th.Paint(theme.Subtle, "▏")
	}
	lines := []string{fit(head, w), fit(search, w), ""}

	footer := "↑↓ select · enter resume · tab preview · ctrl+r rename · ctrl+a all projects · ctrl+w worktrees · ctrl+b branch · esc close"
	if p.renaming {
		footer = "enter save · esc cancel"
	}
	footerLines := strings.Split(wrapLines([]string{th.Paint(theme.Inactive, footer)}, w), "\n")
	body := max(h-len(lines)-len(footerLines)-1, 2)

	switch {
	case p.loading:
		lines = append(lines, th.Paint(theme.Inactive, "Loading conversations…"))
	case p.err != nil && len(p.list) == 0:
		lines = append(lines, th.Paint(theme.Error, "Could not list conversations: "+p.err.Error()))
	case len(p.view) == 0:
		msg := "No conversations here yet."
		if p.query != "" {
			msg = "No conversations match \"" + p.query + "\"."
		}
		lines = append(lines, th.Paint(theme.Inactive, msg))
		if !p.scope.all {
			lines = append(lines, th.Paint(theme.Inactive, "ctrl+a searches every project."))
		}
	case p.preview != nil:
		lines = append(lines, p.previewView(ctx, w, body)...)
	default:
		lines = append(lines, p.listView(ctx, w, body)...)
	}
	for len(lines) < h-len(footerLines) {
		lines = append(lines, "")
	}
	lines = append(lines, footerLines...)
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// listView draws two lines per session around the selection.
func (p *picker) listView(ctx ext.Ctx, w, rows int) []string {
	th := ctx.Theme()
	now := ctx.Clock().Now()
	group := func(vi int) string { return dateGroup(now, p.list[p.view[vi]].Modified) }
	// end returns the first row index that no longer fits when the window starts at top
	// (two lines per session, plus a header line where the date group changes).
	end := func(top int) int {
		used, prev := 0, ""
		i := top
		for ; i < len(p.view); i++ {
			need := 2
			if g := group(i); g != prev {
				need, prev = 3, g
			}
			if used+need > rows && i > top {
				break
			}
			used += need
		}
		return i
	}
	if p.sel < p.top {
		p.top = p.sel
	}
	for p.top < p.sel && end(p.top) <= p.sel {
		p.top++
	}
	var out []string
	prev := ""
	for vi := p.top; vi < end(p.top); vi++ {
		if g := group(vi); g != prev {
			out = append(out, fit(th.Fg(theme.Inactive).Bold(true).Render(g), w))
			prev = g
		}
		m := p.list[p.view[vi]]
		title := m.Title()
		if title == "" {
			title = "(no prompt)"
		}
		marker, style := "  ", th.Fg(theme.Text)
		if vi == p.sel {
			marker, style = th.Paint(theme.Suggestion, "› "), th.Fg(theme.Suggestion).Bold(true)
		}
		tags := ""
		if m.ID == p.live {
			tags += " (current)"
		}
		out = append(out, fit(marker+style.Render(oneLine(title))+th.Paint(theme.Inactive, tags), w))

		var meta []string
		meta = append(meta, relTime(ctx.Clock().Now(), m.Modified))
		if m.GitBranch != "" {
			meta = append(meta, m.GitBranch)
		}
		if m.MessageCount > 0 {
			meta = append(meta, plural(m.MessageCount, "message", "messages"))
		}
		if m.PRNumber != 0 {
			meta = append(meta, "PR #"+itoa(m.PRNumber))
		}
		if m.Hidden {
			meta = append(meta, "headless")
		}
		if p.scope.all || p.scope.worktrees {
			if m.Cwd != "" {
				meta = append(meta, shortPath(m.Cwd, p.home))
			}
		}
		out = append(out, fit("  "+th.Paint(theme.Inactive, strings.Join(meta, " · ")), w))
	}
	return out
}

func (p *picker) previewView(ctx ext.Ctx, w, rows int) []string {
	th := ctx.Theme()
	m, _ := p.selected()
	out := []string{fit(th.Fg(theme.Suggestion).Bold(true).Render(oneLine(m.Title())), w)}
	switch {
	case p.preview.loading:
		out = append(out, th.Paint(theme.Inactive, "Loading…"))
	case p.preview.err != nil:
		out = append(out, th.Paint(theme.Error, p.preview.err.Error()))
	default:
		lines := p.preview.lines
		if len(lines) > rows-1 {
			lines = lines[len(lines)-(rows-1):]
		}
		for _, l := range lines {
			if strings.HasPrefix(l, ">") {
				out = append(out, fit(th.Paint(theme.Text, l), w))
			} else {
				out = append(out, fit(th.Paint(theme.Inactive, l), w))
			}
		}
	}
	return out
}
