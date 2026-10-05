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
	menuRows     = 8
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
	m.kind, m.items, m.sel = compNone, nil, 0
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
		m.slash(c, s, tok[1:], leading)
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

func (m *completion) slash(c ext.Ctx, s *state, query string, leading bool) {
	m.kind = compSlash
	m.items = slashItems(c, query, leading)
	m.sel = 0
}

// slashItems lists the commands matching query, in tiers: names (and
// aliases) starting with it, names containing it, then descriptions
// containing it. A leading command falls back to fuzzy name matching when
// nothing else matches; a mid-prompt "/" only takes name prefixes. Hidden
// commands show only when typed exactly; internal "__" names never show.
func slashItems(c ext.Ctx, query string, leading bool) []compItem {
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
		for _, cmd := range cmds {
			out = append(out, cmdItem(cmd, ""))
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
		text = "@" + quotePath(it.value)
		if !it.dir {
			text += " "
		}
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

// nameCol is the width of the menu's name column.
const nameCol = 30

// view draws the menu: a name column, then the description wrapped to at
// most two lines (the second cut with "…"), within maxLines rows.
func (m *completion) view(t *theme.Theme, width, maxLines int) []string {
	if !m.open() {
		return nil
	}
	if maxLines < 1 {
		maxLines = menuRows
	}
	type row struct{ lines []string }
	render := func(i int) row {
		it := m.items[i]
		name := itemName(it, m.kind)
		col := min(nameCol, max(width/3, 12))
		if ansi.StringWidth(name) > col-2 {
			name = ansi.Truncate(name, col-2, "…")
		}
		pad := strings.Repeat(" ", col-ansi.StringWidth(name))
		nameText := name
		if i == m.sel {
			nameText = t.Paint(theme.Suggestion, name)
		}
		first := "  " + nameText + pad
		room := width - 2 - col
		if it.desc == "" || room < 8 {
			return row{[]string{first}}
		}
		desc := wrapDesc(it.desc, room)
		lines := []string{first + t.Paint(theme.Inactive, desc[0])}
		if len(desc) > 1 {
			lines = append(lines, strings.Repeat(" ", 2+col)+t.Paint(theme.Inactive, desc[1]))
		}
		return row{lines}
	}
	// Show a window of items around the selection that fits maxLines.
	first := 0
	for {
		used := 0
		last := first
		for last < len(m.items) {
			n := len(render(last).lines)
			if used+n > maxLines {
				break
			}
			used += n
			last++
		}
		if m.sel < last || first >= m.sel {
			var out []string
			for i := first; i < last; i++ {
				out = append(out, render(i).lines...)
			}
			if len(out) == 0 && len(m.items) > 0 {
				out = append(out, render(m.sel).lines[0])
			}
			return out
		}
		first++
	}
}

// wrapDesc wraps a description to at most two lines of width w.
func wrapDesc(desc string, w int) []string {
	desc = strings.Join(strings.Fields(desc), " ")
	if ansi.StringWidth(desc) <= w {
		return []string{desc}
	}
	lines := strings.Split(ansi.Wordwrap(desc, w, ""), "\n")
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

func itemName(it compItem, kind compKind) string {
	switch kind {
	case compFile:
		return "+ " + it.label
	case compEmoji, compPath, compArgs:
		return it.label
	}
	return it.label // "/name": no argument hints or aliases, as claude
}
