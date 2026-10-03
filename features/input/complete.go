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
}

// fileTickMsg fires after the @ debounce.
type fileTickMsg struct {
	seq   int
	query string
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
	// Ghost-complete the top match while typing a leading command.
	if leading && len(m.items) > 0 && query != "" && strings.HasPrefix(m.items[0].value, query) && s.ed.AtEnd() {
		s.ed.SetGhost(m.items[0].value[len(query):])
	}
}

// slashItems lists the commands matching query: fuzzy on the leading
// command, prefix-only for a mid-prompt "/". Hidden commands show only when
// typed exactly.
func slashItems(c ext.Ctx, query string, fuzzyMatch bool) []compItem {
	cmds := c.Commands()
	var out []compItem
	if hidden, ok := c.Command(query); ok && query != "" && hidden.Hidden && hidden.Name == query {
		out = append(out, cmdItem(hidden, ""))
	}
	if query == "" {
		for _, cmd := range cmds {
			if !cmd.Hidden {
				out = append(out, cmdItem(cmd, ""))
			}
		}
		return limit(out)
	}
	type cand struct {
		cmd   ext.Command
		alias string
	}
	var keys []string
	var cands []cand
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}
		keys = append(keys, cmd.Name)
		cands = append(cands, cand{cmd, ""})
		for _, a := range cmd.Aliases {
			keys = append(keys, a)
			cands = append(cands, cand{cmd, a})
		}
	}
	type scored struct {
		item   compItem
		prefix int // 0 name prefix, 1 alias prefix, 2 fuzzy
		score  int
	}
	best := map[string]scored{}
	consider := func(i, score int) {
		cd := cands[i]
		pre := 2
		switch {
		case strings.HasPrefix(cd.cmd.Name, query):
			pre = 0
		case cd.alias != "" && strings.HasPrefix(cd.alias, query):
			pre = 1
		}
		if pre == 2 && !fuzzyMatch {
			return
		}
		sc := scored{item: cmdItem(cd.cmd, cd.alias), prefix: pre, score: score}
		if pre == 0 {
			sc.item.alias = ""
		}
		if old, ok := best[cd.cmd.Name]; !ok || sc.prefix < old.prefix || (sc.prefix == old.prefix && sc.score > old.score) {
			best[cd.cmd.Name] = sc
		}
	}
	if fuzzyMatch {
		for _, mt := range fuzzy.Find(query, keys) {
			consider(mt.Index, mt.Score)
		}
	} else {
		for i, k := range keys {
			if strings.HasPrefix(k, query) {
				consider(i, 0)
			}
		}
	}
	list := make([]scored, 0, len(best))
	for _, v := range best {
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.prefix != b.prefix {
			return a.prefix < b.prefix
		}
		if a.prefix < 2 && len(a.item.value) != len(b.item.value) {
			return len(a.item.value) < len(b.item.value)
		}
		if a.score != b.score {
			return a.score > b.score
		}
		return a.item.value < b.item.value
	})
	for _, v := range list {
		if len(out) > 0 && out[0].value == v.item.value {
			continue
		}
		out = append(out, v.item)
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
	return nil
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

func (m *completion) view(t *theme.Theme, width int) []string {
	if !m.open() {
		return nil
	}
	first := 0
	if m.sel >= menuRows {
		first = m.sel - menuRows + 1
	}
	last := min(first+menuRows, len(m.items))
	nameW := 0
	for _, it := range m.items[first:last] {
		nameW = max(nameW, ansi.StringWidth(itemName(it)))
	}
	nameW = min(nameW, width/2)
	var out []string
	for i := first; i < last; i++ {
		it := m.items[i]
		name := ansi.Truncate(itemName(it), nameW, "…")
		pad := strings.Repeat(" ", max(nameW-ansi.StringWidth(name), 0))
		line := "  " + name + pad
		if it.desc != "" {
			room := width - ansi.StringWidth(line) - 2
			if room > 4 {
				line += "  " + t.Paint(theme.Inactive, ansi.Truncate(it.desc, room, "…"))
			}
		}
		if i == m.sel {
			line = t.Paint(theme.Suggestion, "  "+name+pad) + strings.TrimPrefix(line, "  "+name+pad)
		}
		out = append(out, line)
	}
	return out
}

func itemName(it compItem) string {
	name := it.label
	if it.alias != "" {
		name += " (" + it.alias + ")"
	}
	if it.hint != "" {
		name += " " + it.hint
	}
	return name
}
