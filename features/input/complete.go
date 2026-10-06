package input

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

type compKind int

const (
	compNone compKind = iota
	compSlash
	compArgs
	compFile
	compEmoji
	compPath // local paths in ! mode
)

// Menu limits.
const (
	maxItems     = 100
	fileDebounce = 120 * time.Millisecond
	fileRetry    = 300 * time.Millisecond // the engine's file index may still be building
	fileRetries  = 3
)

// compItem is one menu entry.
type compItem struct {
	value string // inserted text (after the trigger character)
	label string // shown name
	alias string // alias that matched, shown next to the name
	hint  string // argument hint
	desc  string
	dir   bool // a directory: accepting keeps completing
}

// completion is the autocomplete menu state.
type completion struct {
	kind      compKind
	start     int // token start column on the cursor row
	items     []compItem
	sel       int
	dismissed string // token hidden by esc until it changes
	token     string

	noMatch string // the leading /command typed when nothing matches it

	fileAsked string // newest @ query sent to the engine
	fileTries int    // empty answers for fileAsked so far
}

// fileTickMsg fires after the @ debounce (or a retry delay).
type fileTickMsg struct {
	seq   int
	query string
	retry bool
}

func (m *completion) open() bool { return m.kind != compNone && len(m.items) > 0 }

func (m *completion) close() {
	m.kind, m.items, m.sel, m.noMatch = compNone, nil, 0, ""
}

// noMatchLine is shown in place of the menu when no command matches.
func (m *completion) noMatchLine(t *theme.Theme) string {
	return "  " + t.Paint(theme.Inactive, `No commands match "`+m.noMatch+`"`)
}

func (m *completion) dismiss(s *state) {
	m.dismissed = m.token
	m.close()
	s.ed.SetGhost("")
}

func (m *completion) move(d int) {
	if len(m.items) == 0 {
		return
	}
	m.sel = (m.sel + d + len(m.items)) % len(m.items)
}

// update recomputes the menu from the token before the cursor.
func (m *completion) update(c ext.Ctx, s *state) tea.Cmd {
	tok, start := s.ed.TokenBeforeCursor()
	m.token = tok
	if m.dismissed != "" && tok == m.dismissed {
		m.close()
		return nil
	}
	m.dismissed = ""
	m.noMatch = ""
	if s.ed.InAttachments() {
		m.close()
		return nil
	}
	if s.mode == modeBash {
		return m.bash(s)
	}
	if tok == "" || !s.ed.TokenAtWordStart(start) {
		return m.args(c, s)
	}
	m.start = start
	switch tok[0] {
	case '/':
		leading := s.ed.Cursor().Row == 0 && start == 0
		return m.slash(c, s, tok[1:], leading)
	case '@':
		return m.file(c, s, tok[1:])
	case ':':
		if s.cfg.emoji && isEmojiQuery(tok[1:]) {
			m.emoji(tok[1:])
		} else {
			m.close()
		}
	default:
		return m.args(c, s)
	}
	return nil
}

// ---- slash commands ----

func (m *completion) slash(c ext.Ctx, s *state, query string, leading bool) tea.Cmd {
	m.kind = compSlash
	m.items = slashItems(c, query, leading, s.skills)
	m.sel = 0
	m.noMatch = ""
	if len(m.items) == 0 && leading && query != "" {
		m.noMatch = "/" + query
	}
	return s.askSkills(c)
}

// slashItems lists the commands matching query, in tiers: names (and
// aliases) starting with it, names containing it, then descriptions
// containing it. A leading command falls back to fuzzy name matching when
// nothing else matches; a mid-prompt "/" only takes name prefixes. Hidden
// commands show only when typed exactly; internal "__" names never show.
func slashItems(c ext.Ctx, query string, leading bool, skills map[string]bool) []compItem {
	var cmds []ext.Command
	for _, cmd := range c.Commands() {
		if !cmd.Hidden && !strings.HasPrefix(cmd.Name, "__") {
			cmds = append(cmds, cmd)
		}
	}
	var out []compItem
	if query != "" && !strings.HasPrefix(query, "__") {
		if cmd, ok := c.Command(query); ok && cmd.Hidden && cmd.Name == query {
			out = append(out, cmdItem(cmd, ""))
		}
	}
	if query == "" {
		// Like claude, the unfiltered menu lists commands; skills show up
		// once you type something they match.
		for _, cmd := range cmds {
			if !skills[cmd.Name] {
				out = append(out, cmdItem(cmd, ""))
			}
		}
		return limit(out)
	}
	q := strings.ToLower(query)
	seen := map[string]bool{}
	for _, it := range out {
		seen[it.value] = true
	}
	add := func(cmd ext.Command, alias string) {
		if !seen[cmd.Name] {
			seen[cmd.Name] = true
			out = append(out, cmdItem(cmd, alias))
		}
	}
	tier := func(match func(ext.Command) (bool, string)) {
		var hits []ext.Command
		aliases := map[string]string{}
		for _, cmd := range cmds {
			if ok, alias := match(cmd); ok && !seen[cmd.Name] {
				hits = append(hits, cmd)
				aliases[cmd.Name] = alias
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return len(hits[i].Name) < len(hits[j].Name) })
		for _, cmd := range hits {
			add(cmd, aliases[cmd.Name])
		}
	}
	tier(func(cmd ext.Command) (bool, string) { return strings.HasPrefix(strings.ToLower(cmd.Name), q), "" })
	tier(func(cmd ext.Command) (bool, string) {
		for _, a := range cmd.Aliases {
			if strings.HasPrefix(strings.ToLower(a), q) {
				return true, a
			}
		}
		return false, ""
	})
	if !leading {
		return limit(out)
	}
	tier(func(cmd ext.Command) (bool, string) { return strings.Contains(strings.ToLower(cmd.Name), q), "" })
	var desc []ext.Command
	for _, cmd := range cmds {
		if !seen[cmd.Name] && strings.Contains(strings.ToLower(cmd.Description), q) {
			desc = append(desc, cmd)
		}
	}
	for _, cmd := range desc {
		add(cmd, "")
	}
	if len(out) == 0 {
		names := make([]string, len(cmds))
		for i, cmd := range cmds {
			names[i] = cmd.Name
		}
		for _, mt := range fuzzy.Find(query, names) {
			add(cmds[mt.Index], "")
		}
	}
	return limit(out)
}

func cmdItem(cmd ext.Command, alias string) compItem {
	return compItem{value: cmd.Name, label: "/" + cmd.Name, alias: alias, hint: cmd.ArgHint, desc: cmd.Description}
}

func limit(items []compItem) []compItem {
	if len(items) > maxItems {
		return items[:maxItems]
	}
	return items
}

// args offers a command's own argument completions ("/model op").
func (m *completion) args(c ext.Ctx, s *state) tea.Cmd {
	if s.ed.Cursor().Row != 0 {
		m.close()
		return nil
	}
	line := s.ed.Line(0)
	if !strings.HasPrefix(line, "/") || !strings.Contains(line, " ") {
		m.close()
		return nil
	}
	name, _ := splitCommand(line)
	cmd, ok := c.Command(name)
	if !ok || cmd.Complete == nil {
		m.close()
		return nil
	}
	tok, start := s.ed.TokenBeforeCursor()
	prefix := strings.TrimSpace(strings.TrimPrefix(line, "/"+name))
	var items []compItem
	for _, cp := range cmd.Complete(c, prefix) {
		label := cp.Display
		if label == "" {
			label = cp.Value
		}
		items = append(items, compItem{value: cp.Value, label: label, desc: cp.Description})
	}
	if len(items) == 0 {
		m.close()
		return nil
	}
	m.kind, m.items, m.start = compArgs, limit(items), start
	if m.sel >= len(m.items) {
		m.sel = 0
	}
	m.token = tok
	return nil
}

// ---- @ files ----

func (m *completion) file(c ext.Ctx, s *state, query string) tea.Cmd {
	if m.kind != compFile {
		m.items, m.sel = nil, 0
	}
	m.kind = compFile
	s.seq++
	seq := s.seq
	return c.Clock().Tick(fileDebounce, func(time.Time) tea.Msg { return fileTickMsg{seq: seq, query: query} })
}

func (m *completion) fileTick(c ext.Ctx, s *state, t fileTickMsg) tea.Cmd {
	if t.seq != s.seq || m.kind != compFile {
		return nil
	}
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		return nil
	}
	if !t.retry || t.query != m.fileAsked {
		m.fileTries = 0
	}
	m.fileAsked = t.query
	return eng.Control(proto.SubFileSuggestions, proto.FileSuggestionsRequest{Query: t.query})
}

func (m *completion) fileResults(c ext.Ctx, s *state, res ext.ControlResultMsg) tea.Cmd {
	if m.kind != compFile || res.Err != nil {
		return nil
	}
	if tok, _ := s.ed.TokenBeforeCursor(); tok != "@"+m.fileAsked {
		return nil // an older answer; a newer request is on its way
	}
	var r proto.FileSuggestionsResponse
	if json.Unmarshal(res.Resp, &r) != nil {
		return nil
	}
	var items []compItem
	for _, sg := range r.Suggestions {
		if sg.Path == "" {
			continue
		}
		items = append(items, compItem{value: sg.Path, label: sg.Path, dir: strings.HasSuffix(sg.Path, "/")})
	}
	m.items, m.sel = limit(items), 0
	s.invalidate(c)
	if len(items) == 0 && m.fileAsked != "" && m.fileTries < fileRetries {
		// Right after start the engine answers from a file index that is
		// still being built; ask again shortly.
		m.fileTries++
		seq, q := s.seq, m.fileAsked
		return c.Clock().Tick(fileRetry, func(time.Time) tea.Msg { return fileTickMsg{seq: seq, query: q, retry: true} })
	}
	return nil
}

// warmFiles asks for file suggestions once so the engine builds its index
// before the first @.
func warmFiles(eng ext.Engine) tea.Cmd {
	if eng == nil {
		return nil
	}
	return eng.Control(proto.SubFileSuggestions, proto.FileSuggestionsRequest{Query: ""})
}

// ---- ! mode ----

// bash completes "!" commands: local paths for a path-like token, else the
// newest earlier command that starts with the typed text, as ghost text.
func (m *completion) bash(s *state) tea.Cmd {
	tok, start := s.ed.TokenBeforeCursor()
	if looksLikePath(tok) {
		if items := pathItems(s.cwd, tok); len(items) > 0 {
			m.kind, m.items, m.start, m.sel = compPath, items, start, 0
			return nil
		}
	}
	m.close()
	if s.ed.LineCount() == 1 && s.ed.AtEnd() {
		typed := s.ed.Line(0)
		if cmd := s.bashHistoryMatch(typed); cmd != "" {
			s.ed.SetGhost(cmd[len(typed):])
		}
	}
	return nil
}

// bashHistoryMatch is the newest "!" command in this project that extends
// typed.
func (s *state) bashHistoryMatch(typed string) string {
	if typed == "" {
		return ""
	}
	for i := len(s.histAll) - 1; i >= 0; i-- {
		e := s.histAll[i]
		if e.Project != s.cwd || !strings.HasPrefix(e.Display, "!") || strings.Contains(e.Display, "\n") {
			continue
		}
		if cmd := e.Display[1:]; len(cmd) > len(typed) && strings.HasPrefix(cmd, typed) {
			return cmd
		}
	}
	return ""
}

func looksLikePath(tok string) bool {
	return strings.Contains(tok, "/") || strings.HasPrefix(tok, ".") || strings.HasPrefix(tok, "~")
}

// pathItems lists directory entries completing tok (relative to cwd).
func pathItems(cwd, tok string) []compItem {
	dirPart, base := filepath.Split(tok)
	dir := dirPart
	switch {
	case strings.HasPrefix(dir, "~/") || dir == "~":
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
		}
	case dir == "":
		dir = cwd
	case !filepath.IsAbs(dir):
		dir = filepath.Join(cwd, dir)
	}
	if tok == "~" {
		return []compItem{{value: "~/", label: "~/", dir: true}}
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []compItem
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, base) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}
		it := compItem{value: dirPart + name, label: name}
		if e.IsDir() {
			it.value += "/"
			it.label += "/"
			it.dir = true
		}
		out = append(out, it)
		if len(out) >= maxItems {
			break
		}
	}
	if len(out) == 1 && out[0].value == tok {
		return nil // already complete
	}
	return out
}

// ---- accepting ----

// selectedIsTyped reports whether accepting the selected item would leave
// the token unchanged (an exact match is already typed).
func (m *completion) selectedIsTyped() bool {
	if !m.open() {
		return false
	}
	it := m.items[m.sel]
	switch m.kind {
	case compSlash:
		return m.token == "/"+it.value
	case compArgs, compPath:
		return m.token == it.value
	case compFile:
		return m.token == "@"+quotePath(it.value)
	}
	return false
}

// accept inserts the selected item. When submitting, a command gets no
// trailing space.
func (m *completion) accept(c ext.Ctx, s *state, submitting bool) tea.Cmd {
	if !m.open() {
		return nil
	}
	it := m.items[m.sel]
	var text string
	switch m.kind {
	case compSlash:
		text = "/" + it.value
		if !submitting {
			text += " "
		}
	case compArgs:
		text = it.value
		if !submitting {
			text += " "
		}
	case compFile:
		// No trailing space: as in Claude Code the menu stays on the exact
		// match, and Enter then sends the prompt.
		text = "@" + quotePath(it.value)
	case compEmoji:
		text = it.value
	case compPath:
		text = it.value
		if !it.dir {
			text += " "
		}
	}
	s.ed.ReplaceBeforeCursor(m.start, text)
	m.close()
	return s.changed(c)
}

// quotePath quotes paths with spaces the way @-mentions accept them.
func quotePath(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}

// ---- view ----

// nameCol is the width of the menu's name column, including the two-column
// selection marker; descriptions start two columns after it.
const nameCol = 30

// view draws the menu below the prompt (inline layout). Claude Code gives it half
// the terminal height in rows: it fills down from the first item, and once the
// selection moves on, the rows above the selection fill up to half the budget and
// the rest follow below it (checked side by side at 100x20 to 160x40).
func (m *completion) view(t *theme.Theme, width, budget int) []string {
	if m.noMatch != "" {
		return []string{m.noMatchLine(t)}
	}
	if !m.open() {
		return nil
	}
	budget = max(budget, 1)
	rows := map[int][]string{}
	render := func(i int) []string {
		if r, ok := rows[i]; ok {
			return r
		}
		rows[i] = m.menuItem(t, i, width)
		return rows[i]
	}
	n := len(m.items)
	start, above := m.sel, 0
	for start > 0 && above+len(render(start-1)) <= budget/2 {
		start--
		above += len(render(start))
	}
	total, end := above+len(render(m.sel)), m.sel+1
	for end < n && total+len(render(end)) <= budget {
		total += len(render(end))
		end++
	}
	for start > 0 && total+len(render(start-1)) <= budget { // the list's end: use the rest above
		start--
		total += len(render(start))
	}
	var out []string
	for i := start; i < end; i++ {
		out = append(out, render(i)...)
	}
	if len(out) > budget {
		out = out[:budget]
	}
	return out
}

// Fullscreen overlay limits. Claude Code (2.1.289/2.1.290) shows at most five items in
// at most five rows above the prompt at every terminal size (checked side by side at
// 100x20 to 200x100).
const (
	fsMenuItems = 5
	fsMenuRows  = 5
)

// viewOverlay draws the menu as Claude Code's fullscreen overlay: the selection marked
// ❯, files marked "+", the window centred on the selection (two items above it) and
// clamped to the list's ends, then cut to fsMenuRows rows by dropping items below the
// selection first and above it second (descriptions can take two rows).
func (m *completion) viewOverlay(t *theme.Theme, width int) []string {
	if m.noMatch != "" {
		return []string{m.noMatchLine(t)}
	}
	if !m.open() {
		return nil
	}
	n := len(m.items)
	start := max(0, min(m.sel-fsMenuItems/2, n-fsMenuItems))
	end := min(n, start+fsMenuItems)
	rows := map[int][]string{}
	render := func(i int) []string {
		if r, ok := rows[i]; ok {
			return r
		}
		rows[i] = m.menuItem(t, i, width)
		return rows[i]
	}
	height := func() int {
		h := 0
		for i := start; i < end; i++ {
			h += len(render(i))
		}
		return h
	}
	for height() > fsMenuRows {
		switch {
		case end-1 > m.sel:
			end--
		case start < m.sel:
			start++
		default:
			return render(m.sel)[:1]
		}
	}
	var out []string
	for i := start; i < end; i++ {
		out = append(out, render(i)...)
	}
	return out
}

// menuItem renders one menu entry as Claude Code does in both layouts:
// "  ❯ /name   description" (the marker column blank for other items, files shown
// as "+ path"), the description wrapped to at most two rows.
func (m *completion) menuItem(t *theme.Theme, i, width int) []string {
	it := m.items[i]
	col := min(nameCol, max(width/3, 12))
	marker := "  "
	if i == m.sel && m.kind != compFile {
		marker = t.Paint(theme.Suggestion, "❯ ")
	}
	name := itemName(it, m.kind)
	if ansi.StringWidth(name) > col-4 {
		name = ansi.Truncate(name, col-4, "…")
	}
	pad := strings.Repeat(" ", max(col-2-ansi.StringWidth(name), 1))
	if i == m.sel {
		name = t.Paint(theme.Suggestion, name)
	}
	room := width - 4 - col
	if it.desc == "" || room < 8 {
		return []string{"  " + marker + name}
	}
	first := "  " + marker + name + pad
	desc := wrapDesc(it.desc, room)
	lines := []string{first + t.Paint(theme.Inactive, desc[0])}
	if len(desc) > 1 {
		lines = append(lines, strings.Repeat(" ", 2+col)+t.Paint(theme.Inactive, desc[1]))
	}
	return lines
}

// wrapDesc wraps a description to at most two lines of width w.
func wrapDesc(desc string, w int) []string {
	desc = strings.Join(strings.Fields(desc), " ")
	if ansi.StringWidth(desc) <= w {
		return []string{desc}
	}
	lines := wordWrap(desc, w)
	if len(lines) == 1 {
		return []string{ansi.Truncate(desc, w, "…")}
	}
	second := strings.Join(lines[1:], " ")
	if len(lines) > 2 || ansi.StringWidth(second) > w {
		second = ansi.Truncate(second, w-1, "") + "…"
		if ansi.StringWidth(second) > w {
			second = ansi.Truncate(second, w, "…")
		}
	}
	return []string{ansi.Truncate(lines[0], w, "…"), second}
}

// wordWrap breaks text at spaces into lines no wider than w (a word longer
// than w gets a line of its own).
func wordWrap(text string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case ansi.StringWidth(cur)+1+ansi.StringWidth(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func itemName(it compItem, kind compKind) string {
	switch kind {
	case compFile:
		return "+ " + it.label
	case compEmoji, compPath, compArgs:
		return it.label
	}
	return it.label // "/name": no argument hints or aliases, as claude
}
