// Package vim is a pure vim-mode state machine for the prompt editor.
//
// The machine knows nothing about rendering or terminals. It edits a Target
// (the editor implements it) and reports what the host should do with keys
// it does not use. Supported: INSERT, NORMAL, VISUAL, VISUAL LINE and
// REPLACE modes; motions h j k l w b e W B E ge 0 ^ $ gg G f F t T ; , % { };
// operators d c y > < with counts; text objects iw aw iW aW i" a" i' a' i`
// a` i( a( i[ a[ i{ a{ i< a< ip ap; x X p P r J ~ s S C D Y u ctrl+r . o O
// a A i I; insert-mode remaps with a timeout.
package vim

import (
	"strconv"
	"strings"
	"time"
)

// Mode is the vim mode.
type Mode int

const (
	Insert Mode = iota
	Normal
	Visual
	VisualLine
	Replace
)

// String is the mode name as Claude Code reports it (statusLine vim.mode).
func (m Mode) String() string {
	switch m {
	case Normal:
		return "NORMAL"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "VISUAL LINE"
	case Replace:
		return "REPLACE"
	}
	return "INSERT"
}

// Indicator is the text shown below the prompt ("-- INSERT --"); empty in
// NORMAL mode.
func (m Mode) Indicator() string {
	if m == Normal {
		return ""
	}
	return "-- " + m.String() + " --"
}

// Pos is a buffer position (see editor.Pos).
type Pos struct{ Row, Col int }

func (p Pos) less(q Pos) bool { return p.Row < q.Row || (p.Row == q.Row && p.Col < q.Col) }

func order(p, q Pos) (Pos, Pos) {
	if q.less(p) {
		return q, p
	}
	return p, q
}

// Clip is opaque buffer content produced and consumed by a Target (it keeps
// chips intact).
type Clip any

// ChipGrapheme is what Target.Grapheme returns for a chip cell.
const ChipGrapheme = "￼"

// Target is the buffer the machine edits.
type Target interface {
	LineCount() int
	LineLen(row int) int
	// Grapheme returns the grapheme at (row, col), ChipGrapheme for a chip.
	Grapheme(row, col int) string
	Cursor() Pos
	SetCursor(Pos)
	// Copy returns the content of [from, to) in reading order.
	Copy(from, to Pos) Clip
	// Delete removes [from, to) and returns what was removed.
	Delete(from, to Pos) Clip
	// Insert inserts c at p and returns the position just after it.
	Insert(p Pos, c Clip) Pos
	// InsertText inserts plain text (may contain newlines) at p and returns
	// the position just after it.
	InsertText(p Pos, s string) Pos
	// Key applies an insert-mode key with the host editor's own key handling
	// (typing, backspace, readline keys). It reports whether it was used.
	Key(Key) bool
	// BeginGroup and EndGroup bracket edits that undo as one step.
	BeginGroup()
	EndGroup()
	Undo() bool
	Redo() bool
}

// Key is one key press: Text for typed characters, Name for the keystroke
// ("esc", "enter", "ctrl+r", "left", ...).
type Key struct {
	Text string
	Name string
}

// ID is the key as vim commands see it.
func (k Key) ID() string {
	if k.Text != "" {
		return k.Text
	}
	return k.Name
}

var escKey = Key{Name: "esc"}

// Event asks the host to do something on the machine's behalf.
type Event int

const (
	EventNone Event = iota
	// EventHistorySearch: "/" in NORMAL mode opens history search.
	EventHistorySearch
	// EventHistoryPrev and EventHistoryNext: k/j (or up/down) at the
	// first/last line in NORMAL mode recall history.
	EventHistoryPrev
	EventHistoryNext
)

// Result says what happened to a key.
type Result struct {
	// Handled is false when the host should process the key itself
	// (enter, esc in NORMAL mode with nothing pending, ctrl chords, ...).
	Handled bool
	Event   Event
	// Pending is true when an insert-mode remap prefix is buffered; the
	// host calls Flush after RemapTimeout unless another key arrives.
	Pending bool
}

// Register holds yanked or deleted content.
type Register struct {
	Clip     Clip
	Linewise bool
}

// Machine is the vim state machine.
type Machine struct {
	t    Target
	mode Mode

	// RemapTimeout is how long an insert-mode remap prefix waits.
	RemapTimeout time.Duration
	remaps       map[string][]Key
	remapPrefix  map[string]bool
	pend         []Key

	keys     []Key // pending NORMAL/VISUAL command
	reg      Register
	lastFind findSpec
	goal     int // preferred column for j/k; -1 none, maxCol for $

	anchor Pos // visual mode anchor

	// dot repeat
	rec       []Key
	recCount  int
	recording bool
	dot       []Key
	dotCount  int
	replaying bool

	// insert session
	insertCount   int
	insertKeys    []Key
	insertNewline bool // o/O: repeat opens a new line first
	groupOpen     bool
	replaced      []replacedCell
}

type replacedCell struct {
	pos  Pos
	orig Clip // nil when the cell was appended
}

const maxCol = 1 << 30

// New returns a machine in INSERT mode editing t.
func New(t Target) *Machine {
	return &Machine{t: t, mode: Insert, goal: -1, RemapTimeout: time.Second}
}

// Mode returns the current mode.
func (m *Machine) Mode() Mode { return m.mode }

// SetMode switches mode, ending any pending command or insert session.
func (m *Machine) SetMode(md Mode) {
	m.keys = nil
	m.pend = nil
	if (m.mode == Insert || m.mode == Replace) && md != Insert && md != Replace {
		m.endGroup()
	}
	m.mode = md
	if md == Visual || md == VisualLine {
		m.anchor = m.t.Cursor()
	}
	if md == Normal {
		m.clampNormal()
	}
}

// Reset returns to INSERT mode with nothing pending (after a submit).
func (m *Machine) Reset() {
	m.keys, m.pend = nil, nil
	m.insertKeys, m.insertCount = nil, 0
	m.recording = false
	if m.groupOpen {
		m.t.EndGroup()
		m.groupOpen = false
	}
	m.mode = Insert
}

// Register returns the unnamed register.
func (m *Machine) Register() Register { return m.reg }

// PendingKeys returns the partial NORMAL-mode command (for a "showcmd"
// display).
func (m *Machine) PendingKeys() string {
	var b strings.Builder
	for _, k := range m.keys {
		b.WriteString(k.ID())
	}
	return b.String()
}

// SetRemaps sets insert-mode remaps such as {"jj": "<Esc>"}. Keys are typed
// characters; values may use <Esc>, <CR>, <BS>, <Tab> and <lt>.
func (m *Machine) SetRemaps(r map[string]string) {
	m.remaps = map[string][]Key{}
	m.remapPrefix = map[string]bool{}
	for from, to := range r {
		if from == "" {
			continue
		}
		m.remaps[from] = parseKeys(to)
		rs := []rune(from)
		for i := 1; i < len(rs); i++ {
			m.remapPrefix[string(rs[:i])] = true
		}
	}
}

func parseKeys(s string) []Key {
	var out []Key
	for len(s) > 0 {
		if s[0] == '<' {
			if j := strings.IndexByte(s, '>'); j > 0 {
				switch strings.ToLower(s[1:j]) {
				case "esc":
					out = append(out, escKey)
					s = s[j+1:]
					continue
				case "cr", "enter", "return":
					out = append(out, Key{Name: "enter"})
					s = s[j+1:]
					continue
				case "bs":
					out = append(out, Key{Name: "backspace"})
					s = s[j+1:]
					continue
				case "tab":
					out = append(out, Key{Name: "tab"})
					s = s[j+1:]
					continue
				case "lt":
					out = append(out, Key{Text: "<"})
					s = s[j+1:]
					continue
				}
			}
		}
		r := []rune(s)[0]
		out = append(out, Key{Text: string(r)})
		s = s[len(string(r)):]
	}
	return out
}

// Selection returns the visual selection as a half-open range. For VISUAL
// LINE it spans whole lines.
func (m *Machine) Selection() (from, to Pos, linewise, ok bool) {
	switch m.mode {
	case Visual:
		a, b := order(m.anchor, m.t.Cursor())
		return a, m.after(b), false, true
	case VisualLine:
		a, b := order(m.anchor, m.t.Cursor())
		return Pos{a.Row, 0}, Pos{b.Row, m.t.LineLen(b.Row)}, true, true
	}
	return Pos{}, Pos{}, false, false
}

// after returns the position one cell after p, staying on p's line.
func (m *Machine) after(p Pos) Pos {
	if p.Col < m.t.LineLen(p.Row) {
		p.Col++
	}
	return p
}

// Feed processes one key.
func (m *Machine) Feed(k Key) Result {
	switch m.mode {
	case Insert:
		return m.feedInsert(k)
	case Replace:
		return m.feedReplace(k)
	}
	return m.feedNormal(k)
}

// Flush handles a remap timeout: buffered prefix keys are typed as is.
func (m *Machine) Flush() {
	keys := m.pend
	m.pend = nil
	for _, k := range keys {
		m.insertKey(k)
	}
}

// ---- insert and replace ----

func (m *Machine) feedInsert(k Key) Result {
	if !m.replaying && len(m.remaps) > 0 {
		if k.Text == "" {
			m.Flush()
			return m.insertKey(k)
		}
		m.pend = append(m.pend, k)
		seq := keysText(m.pend)
		if to, ok := m.remaps[seq]; ok {
			m.pend = nil
			res := Result{Handled: true}
			for _, kk := range to {
				res = m.Feed(kk)
				if !res.Handled {
					break
				}
			}
			return res
		}
		if m.remapPrefix[seq] {
			return Result{Handled: true, Pending: true}
		}
		// Not a remap: the first buffered key is typed and the rest are
		// looked at again.
		pend := m.pend
		m.pend = nil
		m.insertKey(pend[0])
		res := Result{Handled: true}
		for _, kk := range pend[1:] {
			res = m.feedInsert(kk)
		}
		return res
	}
	return m.insertKey(k)
}

func keysText(ks []Key) string {
	var b strings.Builder
	for _, k := range ks {
		b.WriteString(k.Text)
	}
	return b.String()
}

func (m *Machine) insertKey(k Key) Result {
	if k.Name == "esc" {
		m.exitInsert()
		return Result{Handled: true}
	}
	if !m.t.Key(k) {
		return Result{}
	}
	m.record(k)
	m.insertKeys = append(m.insertKeys, k)
	return Result{Handled: true}
}

// enterInsert starts an insert session (the undo group is already open).
func (m *Machine) enterInsert(count int, newline bool) {
	m.mode = Insert
	m.insertCount = count
	m.insertKeys = nil
	m.insertNewline = newline
}

func (m *Machine) exitInsert() {
	for i := 1; i < m.insertCount; i++ {
		if m.insertNewline {
			m.t.SetCursor(m.t.InsertText(m.t.Cursor(), "\n"))
		}
		for _, k := range m.insertKeys {
			m.t.Key(k)
		}
	}
	m.insertCount, m.insertKeys, m.insertNewline = 0, nil, false
	m.record(escKey)
	m.finishRecording()
	m.endGroup()
	m.mode = Normal
	c := m.t.Cursor()
	if c.Col > 0 {
		c.Col--
	}
	m.t.SetCursor(c)
	m.clampNormal()
	m.goal = -1
}

func (m *Machine) feedReplace(k Key) Result {
	switch {
	case k.Name == "esc":
		m.replaced = nil
		m.exitInsert()
		return Result{Handled: true}
	case k.Name == "backspace":
		if n := len(m.replaced); n > 0 {
			r := m.replaced[n-1]
			m.replaced = m.replaced[:n-1]
			m.t.Delete(r.pos, Pos{r.pos.Row, r.pos.Col + 1})
			if r.orig != nil {
				m.t.Insert(r.pos, r.orig)
			}
			m.t.SetCursor(r.pos)
		} else {
			c := m.t.Cursor()
			if c.Col > 0 {
				m.t.SetCursor(Pos{c.Row, c.Col - 1})
			}
		}
		m.record(k)
		return Result{Handled: true}
	case k.Text != "":
		c := m.t.Cursor()
		var orig Clip
		if c.Col < m.t.LineLen(c.Row) {
			orig = m.t.Delete(c, Pos{c.Row, c.Col + 1})
		}
		end := m.t.InsertText(c, k.Text)
		m.t.SetCursor(end)
		m.replaced = append(m.replaced, replacedCell{pos: c, orig: orig})
		m.record(k)
		return Result{Handled: true}
	}
	if m.t.Key(k) {
		m.record(k)
		return Result{Handled: true}
	}
	return Result{}
}

// ---- recording and undo groups ----

func (m *Machine) startChange(keys []Key, count int) {
	if !m.groupOpen {
		m.t.BeginGroup()
		m.groupOpen = true
	}
	if m.replaying {
		return
	}
	m.recording = true
	m.rec = append([]Key(nil), keys...)
	m.recCount = count
}

func (m *Machine) record(k Key) {
	if m.recording && !m.replaying {
		m.rec = append(m.rec, k)
	}
}

func (m *Machine) finishRecording() {
	if m.recording && !m.replaying {
		m.dot = m.rec
		m.dotCount = m.recCount
	}
	m.recording = false
}

func (m *Machine) endGroup() {
	if m.groupOpen {
		m.t.EndGroup()
		m.groupOpen = false
	}
}

// ---- helpers over the target ----

func (m *Machine) lineLen(r int) int { return m.t.LineLen(r) }
func (m *Machine) last() int         { return m.t.LineCount() - 1 }

// clampNormal keeps the cursor on a character in NORMAL mode.
func (m *Machine) clampNormal() {
	c := m.t.Cursor()
	if n := m.lineLen(c.Row); c.Col >= n {
		c.Col = max(n-1, 0)
		m.t.SetCursor(c)
	}
}

func (m *Machine) firstNonBlank(r int) int {
	n := m.lineLen(r)
	for c := 0; c < n; c++ {
		if !isBlank(m.t.Grapheme(r, c)) {
			return c
		}
	}
	return max(n-1, 0)
}

func (m *Machine) lineText(r int) string {
	var b strings.Builder
	for c := 0; c < m.lineLen(r); c++ {
		b.WriteString(m.t.Grapheme(r, c))
	}
	return b.String()
}

func countKeys(n int) []Key {
	var out []Key
	for _, r := range strconv.Itoa(n) {
		out = append(out, Key{Text: string(r)})
	}
	return out
}
