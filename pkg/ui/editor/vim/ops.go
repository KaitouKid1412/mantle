package vim

import (
	"strings"
	"unicode"
)

// indentUnit is what > and < add or remove.
const indentUnit = "  "

func (m *Machine) feedNormal(k Key) Result {
	visual := m.mode == Visual || m.mode == VisualLine
	if len(m.keys) == 0 && !visual && !m.isVimKey(k) {
		return Result{}
	}
	m.keys = append(m.keys, k)
	cmd, st := parse(m.keys, visual)
	switch st {
	case parseMore:
		return Result{Handled: true}
	case parseInvalid:
		pending := len(m.keys) > 1
		m.keys = nil
		if k.Name == "esc" {
			if visual {
				m.SetMode(Normal)
				return Result{Handled: true}
			}
			return Result{Handled: pending}
		}
		if visual && !pending && !m.isVimKey(k) {
			return Result{}
		}
		return Result{Handled: true}
	}
	m.keys = nil
	if visual {
		return m.execVisual(cmd)
	}
	return m.exec(cmd)
}

// isVimKey reports whether a key with nothing pending is a NORMAL-mode key.
// Others (enter, tab, ctrl chords, esc) go to the host.
func (m *Machine) isVimKey(k Key) bool {
	if k.Text != "" {
		return true
	}
	switch k.Name {
	case "ctrl+r", "left", "right", "up", "down", "home", "end", "backspace", "delete", "space":
		return true
	}
	return false
}

func (m *Machine) exec(c command) Result {
	t := m.t
	cur := t.Cursor()
	n := max(c.count, 1)

	if c.op != "" {
		return m.execOperator(c, cur)
	}

	switch c.name {
	case "u":
		for i := 0; i < n && t.Undo(); i++ {
		}
		m.clampNormal()
		return Result{Handled: true}
	case "ctrl+r":
		for i := 0; i < n && t.Redo(); i++ {
		}
		m.clampNormal()
		return Result{Handled: true}
	case ".":
		m.repeat(c.count)
		return Result{Handled: true}
	case "/":
		return Result{Handled: true, Event: EventHistorySearch}
	case "v":
		m.SetMode(Visual)
		return Result{Handled: true}
	case "V":
		m.SetMode(VisualLine)
		return Result{Handled: true}
	}

	if isChange(c.name) {
		m.startChange(c.keys, c.count)
		defer func() {
			if m.mode == Normal {
				m.finishRecording()
				m.endGroup()
			}
		}()
	}

	switch c.name {
	case "i":
		m.enterInsert(n, false)
	case "a":
		if m.lineLen(cur.Row) > 0 {
			t.SetCursor(Pos{cur.Row, cur.Col + 1})
		}
		m.enterInsert(n, false)
	case "I":
		t.SetCursor(Pos{cur.Row, m.firstNonBlankOrEnd(cur.Row)})
		m.enterInsert(n, false)
	case "A":
		t.SetCursor(Pos{cur.Row, m.lineLen(cur.Row)})
		m.enterInsert(n, false)
	case "o":
		end := t.InsertText(Pos{cur.Row, m.lineLen(cur.Row)}, "\n")
		t.SetCursor(end)
		m.enterInsert(n, true)
	case "O":
		t.InsertText(Pos{cur.Row, 0}, "\n")
		t.SetCursor(Pos{cur.Row, 0})
		m.enterInsert(n, true)
	case "R":
		m.replaced = nil
		m.mode = Replace
	case "x", "s":
		ln := m.lineLen(cur.Row)
		if ln > 0 {
			end := Pos{cur.Row, min(cur.Col+n, ln)}
			m.reg = Register{Clip: t.Delete(cur, end)}
		}
		if c.name == "s" {
			m.enterInsert(1, false)
		}
	case "X":
		if cur.Col > 0 {
			from := Pos{cur.Row, max(cur.Col-n, 0)}
			m.reg = Register{Clip: t.Delete(from, cur)}
			t.SetCursor(from)
		}
	case "D", "C":
		m.reg = Register{Clip: t.Delete(cur, Pos{cur.Row, m.lineLen(cur.Row)})}
		if n > 1 && cur.Row < m.last() {
			r2 := min(cur.Row+n-1, m.last())
			t.Delete(Pos{cur.Row, m.lineLen(cur.Row)}, Pos{r2, m.lineLen(r2)})
		}
		if c.name == "C" {
			m.enterInsert(1, false)
		}
	case "S":
		m.changeLines(cur.Row, min(cur.Row+n-1, m.last()))
	case "Y":
		r2 := min(cur.Row+n-1, m.last())
		m.reg = Register{Clip: t.Copy(Pos{cur.Row, 0}, Pos{r2, m.lineLen(r2)}), Linewise: true}
	case "p", "P":
		m.put(c.name == "P", n)
	case "r":
		ln := m.lineLen(cur.Row)
		if cur.Col+n <= ln {
			t.Delete(cur, Pos{cur.Row, cur.Col + n})
			t.InsertText(cur, strings.Repeat(c.arg, n))
			t.SetCursor(Pos{cur.Row, cur.Col + n - 1})
		}
	case "J":
		m.join(cur.Row, max(n, 2))
	case "~":
		ln := m.lineLen(cur.Row)
		end := min(cur.Col+n, ln)
		m.mapCase(cur, Pos{cur.Row, end}, toggleCase)
		t.SetCursor(Pos{cur.Row, end})
	default:
		// A motion.
		to, kind, ok := m.motion(c, cur, false)
		if !ok {
			if c.name == "k" || c.name == "j" {
				if c.name == "k" && cur.Row == 0 {
					return Result{Handled: true, Event: EventHistoryPrev}
				}
				if c.name == "j" && cur.Row == m.last() {
					return Result{Handled: true, Event: EventHistoryNext}
				}
			}
			return Result{Handled: true}
		}
		if kind != linewise || c.name == "G" || c.name == "gg" || c.name == "_" {
			m.goal = to.Col
			if c.name == "$" {
				m.goal = maxCol
			}
		}
		t.SetCursor(to)
	}
	if m.mode == Normal {
		m.clampNormal()
	}
	return Result{Handled: true}
}

func isChange(name string) bool {
	switch name {
	case "i", "a", "I", "A", "o", "O", "R", "x", "s", "X", "D", "C", "S", "p", "P", "r", "J", "~":
		return true
	}
	return false
}

func (m *Machine) firstNonBlankOrEnd(r int) int {
	n := m.lineLen(r)
	for c := 0; c < n; c++ {
		if !isBlank(m.t.Grapheme(r, c)) {
			return c
		}
	}
	return n
}

// execOperator applies d/c/y/>/< over a motion, text object or whole lines.
func (m *Machine) execOperator(c command, cur Pos) Result {
	var from, to Pos
	lw := false
	switch {
	case c.name == "line":
		r2 := min(cur.Row+max(c.count, 1)-1, m.last())
		from, to, lw = Pos{cur.Row, 0}, Pos{r2, m.lineLen(r2)}, true
	case len(c.name) == 2 && (c.name[0] == 'i' || c.name[0] == 'a') && c.name != "ge" && c.name != "gE":
		var ok bool
		from, to, lw, ok = m.textObject(c.name, cur, c.count)
		if !ok {
			return Result{Handled: true}
		}
	default:
		mc := c
		if c.op == "c" && (c.name == "w" || c.name == "W") && m.class(cur, false) != clsBlank {
			// cw acts like ce on a word.
			mc.name = map[string]string{"w": "e", "W": "E"}[c.name]
		}
		dest, kind, ok := m.motion(mc, cur, true)
		if !ok {
			return Result{Handled: true}
		}
		from, to = order(cur, dest)
		switch kind {
		case inclusive:
			to = m.after(to)
		case linewise:
			from, to, lw = Pos{from.Row, 0}, Pos{to.Row, m.lineLen(to.Row)}, true
		}
	}
	if c.op != "y" {
		m.startChange(c.keys, c.count)
	}
	m.applyOperator(c.op, from, to, lw)
	if m.mode == Normal {
		if c.op != "y" {
			m.finishRecording()
			m.endGroup()
		}
		m.clampNormal()
	}
	return Result{Handled: true}
}

// applyOperator runs an operator over [from, to) (whole lines when lw).
func (m *Machine) applyOperator(op string, from, to Pos, lw bool) {
	t := m.t
	switch op {
	case "y":
		m.reg = Register{Clip: t.Copy(from, to), Linewise: lw}
		if lw {
			t.SetCursor(Pos{from.Row, t.Cursor().Col})
		} else {
			t.SetCursor(from)
		}
	case "d":
		if lw {
			m.deleteLines(from.Row, to.Row)
			return
		}
		m.reg = Register{Clip: t.Delete(from, to)}
		t.SetCursor(from)
	case "c":
		if lw {
			m.changeLines(from.Row, to.Row)
			return
		}
		m.reg = Register{Clip: t.Delete(from, to)}
		t.SetCursor(from)
		m.enterInsert(1, false)
	case ">", "<":
		for r := from.Row; r <= to.Row; r++ {
			if op == ">" {
				if m.lineLen(r) > 0 {
					t.InsertText(Pos{r, 0}, indentUnit)
				}
				continue
			}
			k := 0
			for k < len(indentUnit) && k < m.lineLen(r) && m.t.Grapheme(r, k) == " " {
				k++
			}
			if k == 0 && m.lineLen(r) > 0 && m.t.Grapheme(r, 0) == "\t" {
				k = 1
			}
			if k > 0 {
				t.Delete(Pos{r, 0}, Pos{r, k})
			}
		}
		t.SetCursor(Pos{from.Row, m.firstNonBlank(from.Row)})
	case "~", "u", "U":
		f := toggleCase
		if op == "u" {
			f = unicode.ToLower
		} else if op == "U" {
			f = unicode.ToUpper
		}
		m.mapCase(from, to, f)
		t.SetCursor(from)
	}
}

// deleteLines deletes whole lines r1..r2 into the register.
func (m *Machine) deleteLines(r1, r2 int) {
	t := m.t
	m.reg = Register{Clip: t.Copy(Pos{r1, 0}, Pos{r2, m.lineLen(r2)}), Linewise: true}
	switch {
	case r2 < m.last():
		t.Delete(Pos{r1, 0}, Pos{r2 + 1, 0})
		t.SetCursor(Pos{r1, m.firstNonBlank(r1)})
	case r1 > 0:
		t.Delete(Pos{r1 - 1, m.lineLen(r1 - 1)}, Pos{r2, m.lineLen(r2)})
		t.SetCursor(Pos{r1 - 1, m.firstNonBlank(r1 - 1)})
	default:
		t.Delete(Pos{0, 0}, Pos{r2, m.lineLen(r2)})
		t.SetCursor(Pos{0, 0})
	}
}

// changeLines replaces lines r1..r2 with one empty line and starts insert.
func (m *Machine) changeLines(r1, r2 int) {
	t := m.t
	m.reg = Register{Clip: t.Copy(Pos{r1, 0}, Pos{r2, m.lineLen(r2)}), Linewise: true}
	t.Delete(Pos{r1, 0}, Pos{r2, m.lineLen(r2)})
	t.SetCursor(Pos{r1, 0})
	m.enterInsert(1, false)
}

// put pastes the register (p after, P before) n times.
func (m *Machine) put(before bool, n int) {
	t := m.t
	if m.reg.Clip == nil {
		return
	}
	cur := t.Cursor()
	if m.reg.Linewise {
		var at Pos
		if before {
			at = Pos{cur.Row, 0}
		} else {
			at = t.InsertText(Pos{cur.Row, m.lineLen(cur.Row)}, "\n")
		}
		first := at.Row
		p := at
		for i := 0; i < n; i++ {
			p = t.Insert(p, m.reg.Clip)
			if i < n-1 || before {
				p = t.InsertText(p, "\n")
			}
		}
		t.SetCursor(Pos{first, m.firstNonBlank(first)})
		return
	}
	at := cur
	if !before && m.lineLen(cur.Row) > 0 {
		at.Col++
	}
	p := at
	for i := 0; i < n; i++ {
		p = t.Insert(p, m.reg.Clip)
	}
	if p.Col > 0 {
		p.Col--
	}
	t.SetCursor(p)
}

// join joins count lines starting at row r with single spaces.
func (m *Machine) join(r, count int) {
	t := m.t
	for i := 1; i < count && r < m.last(); i++ {
		ln := m.lineLen(r)
		next := r + 1
		k := 0
		for k < m.lineLen(next) && isBlank(m.t.Grapheme(next, k)) {
			k++
		}
		t.Delete(Pos{r, ln}, Pos{next, k})
		sep := " "
		if ln == 0 || m.lineLen(r) == ln || isBlank(m.t.Grapheme(r, ln-1)) || m.t.Grapheme(r, ln) == ")" {
			sep = ""
		}
		if sep != "" {
			t.InsertText(Pos{r, ln}, sep)
		}
		t.SetCursor(Pos{r, ln})
	}
}

func toggleCase(r rune) rune {
	if unicode.IsUpper(r) {
		return unicode.ToLower(r)
	}
	return unicode.ToUpper(r)
}

// mapCase rewrites [from, to) with f applied to each rune, keeping chips.
func (m *Machine) mapCase(from, to Pos, f func(rune) rune) {
	t := m.t
	for r := from.Row; r <= to.Row; r++ {
		c0, c1 := 0, m.lineLen(r)
		if r == from.Row {
			c0 = from.Col
		}
		if r == to.Row {
			c1 = to.Col
		}
		for c := c0; c < c1; c++ {
			g := t.Grapheme(r, c)
			if g == ChipGrapheme {
				continue
			}
			ng := strings.Map(f, g)
			if ng != g {
				t.Delete(Pos{r, c}, Pos{r, c + 1})
				t.InsertText(Pos{r, c}, ng)
			}
		}
	}
}

// repeat is ".": replay the last change, with a new count if given.
func (m *Machine) repeat(count int) {
	if len(m.dot) == 0 || m.replaying {
		return
	}
	n := m.dotCount
	if count > 0 {
		n = count
	}
	keys := m.dot
	if n > 0 {
		keys = append(countKeys(n), keys...)
	}
	m.replaying = true
	defer func() { m.replaying = false }()
	for _, k := range keys {
		m.Feed(k)
	}
	if m.mode == Insert || m.mode == Replace {
		m.Feed(escKey)
	}
}

// ---- visual mode ----

func (m *Machine) execVisual(c command) Result {
	t := m.t
	cur := t.Cursor()
	from, to, lw, _ := m.Selection()
	switch c.name {
	case "v", "V":
		want := Visual
		if c.name == "V" {
			want = VisualLine
		}
		if m.mode == want {
			m.SetMode(Normal)
		} else {
			m.mode = want
		}
		return Result{Handled: true}
	case "o", "O":
		m.anchor, cur = cur, m.anchor
		t.SetCursor(cur)
		return Result{Handled: true}
	case "y", "Y":
		if c.name == "Y" {
			from, to, lw = Pos{from.Row, 0}, Pos{to.Row, m.lineLen(to.Row)}, true
		}
		m.mode = Normal
		m.applyOperator("y", from, to, lw)
		t.SetCursor(from)
		m.clampNormal()
		return Result{Handled: true}
	}

	if len(c.name) == 2 && (c.name[0] == 'i' || c.name[0] == 'a') {
		of, ot, olw, ok := m.textObject(c.name, cur, c.count)
		if ok {
			if olw && m.mode == Visual {
				m.mode = VisualLine
			}
			if m.anchor == cur || of.less(m.anchor) {
				m.anchor = of
			}
			end := ot
			if end.Col > 0 {
				end.Col--
			}
			t.SetCursor(end)
		}
		return Result{Handled: true}
	}

	switch c.name {
	case "d", "x", "X", "D", "c", "s", "C", "S", "R", ">", "<", "~", "u", "U", "J", "p", "P", "r", "I", "A":
	default:
		// A motion moves the cursor and extends the selection.
		dest, _, ok := m.motion(c, cur, false)
		if ok {
			m.goal = dest.Col
			t.SetCursor(dest)
		}
		return Result{Handled: true}
	}

	m.startChange(nil, 0)
	m.recording = false // visual changes are not dot-repeatable here
	m.mode = Normal
	switch c.name {
	case "X", "D", "C", "S", "R":
		from, to, lw = Pos{from.Row, 0}, Pos{to.Row, m.lineLen(to.Row)}, true
	}
	switch c.name {
	case "d", "x", "X", "D":
		m.applyOperator("d", from, to, lw)
	case "c", "s", "C", "S", "R":
		m.applyOperator("c", from, to, lw)
	case ">", "<", "~", "u", "U":
		m.applyOperator(c.name, from, to, lw)
	case "J":
		m.join(from.Row, max(to.Row-from.Row+1, 2))
	case "r":
		for r := from.Row; r <= to.Row; r++ {
			c0, c1 := 0, m.lineLen(r)
			if !lw && r == from.Row {
				c0 = from.Col
			}
			if !lw && r == to.Row {
				c1 = to.Col
			}
			if c1 > c0 {
				t.Delete(Pos{r, c0}, Pos{r, c1})
				t.InsertText(Pos{r, c0}, strings.Repeat(c.arg, c1-c0))
			}
		}
		t.SetCursor(from)
	case "p", "P":
		reg := m.reg
		removed := t.Delete(from, to)
		t.SetCursor(from)
		if reg.Clip != nil {
			switch {
			case reg.Linewise && !lw:
				p := t.InsertText(from, "\n")
				p = t.Insert(p, reg.Clip)
				t.InsertText(p, "\n")
				t.SetCursor(Pos{from.Row + 1, m.firstNonBlank(from.Row + 1)})
			case lw:
				t.Insert(Pos{from.Row, 0}, reg.Clip)
				t.SetCursor(Pos{from.Row, m.firstNonBlank(from.Row)})
			default:
				end := t.Insert(from, reg.Clip)
				if end.Col > 0 {
					end.Col--
				}
				t.SetCursor(end)
			}
		}
		m.reg = Register{Clip: removed, Linewise: lw}
	case "I":
		t.SetCursor(from)
		m.enterInsert(1, false)
	case "A":
		t.SetCursor(to)
		m.enterInsert(1, false)
	}
	if m.mode == Normal {
		m.endGroup()
		m.clampNormal()
	}
	return Result{Handled: true}
}
