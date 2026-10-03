package input

import (
	"encoding/json"
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
	if s.mode != modePrompt || s.ed.InAttachments() {
		m.close()
		return nil
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
