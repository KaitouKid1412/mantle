package render

import (
	"strings"
)

// WrapOptions controls Wrap.
type WrapOptions struct {
	// Width is the total line width in cells, prefixes included. Values < 1 are
	// treated as 1.
	Width int
	// First prefixes the first output line and Rest every later one (a hanging
	// indent). Prefixes may be styled; their display width is subtracted from
	// Width. Every hard line break in the input starts a Rest line.
	First, Rest string
	// Hard breaks at the width regardless of word boundaries and keeps every
	// space (code and tool output). Otherwise text breaks at spaces and after
	// hyphens, words longer than a line are hard-broken, and spaces at a break
	// are dropped.
	Hard bool
	// TabWidth expands tabs (default 4).
	TabWidth int
}

// Wrap word-wraps styled text to width cells and returns the lines (without
// newlines). Styles and OSC 8 links that span a break are closed at the end of
// the line and reopened after the next line's prefix, so every line renders
// correctly on its own.
func Wrap(s string, width int) []string {
	return WrapWith(s, WrapOptions{Width: width})
}

// WrapIndent word-wraps with a first-line prefix and a hanging indent.
func WrapIndent(s string, width int, first, rest string) []string {
	return WrapWith(s, WrapOptions{Width: width, First: first, Rest: rest})
}

// HardWrap breaks text at exactly width cells, keeping all spaces.
func HardWrap(s string, width int) []string {
	return WrapWith(s, WrapOptions{Width: width, Hard: true})
}

// WrapWith wraps s according to o.
func WrapWith(s string, o WrapOptions) []string {
	if o.Width < 1 {
		o.Width = 1
	}
	if o.TabWidth <= 0 {
		o.TabWidth = 4
	}
	s = ExpandTabs(s, o.TabWidth)
	w := &wrapper{o: o, firstW: Width(o.First), restW: Width(o.Rest)}
	w.startLine()
	for li, hard := range strings.Split(s, "\n") {
		if li > 0 {
			w.breakLine()
		}
		if o.Hard {
			w.hardLine(hard)
		} else {
			w.wordLine(hard)
		}
	}
	w.finish()
	return w.lines
}

type wrapper struct {
	o             WrapOptions
	firstW, restW int

	lines []string
	cur   strings.Builder
	curW  int // content width of cur, prefix excluded
	avail int // content width available on cur
	st    lineState
}

func (w *wrapper) startLine() {
	prefix, pw := w.o.Rest, w.restW
	if len(w.lines) == 0 {
		prefix, pw = w.o.First, w.firstW
	}
	w.cur.Reset()
	w.cur.WriteString(prefix)
	w.cur.WriteString(w.st.reopen())
	w.curW = 0
	w.avail = max(w.o.Width-pw, 1)
}

func (w *wrapper) breakLine() {
	w.cur.WriteString(w.st.close())
	w.lines = append(w.lines, w.cur.String())
	w.startLine()
}

func (w *wrapper) finish() {
	w.cur.WriteString(w.st.close())
	w.lines = append(w.lines, w.cur.String())
}

// write appends text that contains no line breaks and tracks escape state.
func (w *wrapper) write(s string, width int) {
	w.cur.WriteString(s)
	w.curW += width
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			n := escLen(s, i)
			w.st.apply(s[i : i+n])
			i += n - 1
		}
	}
}

// hardLine emits one input line, breaking at exactly the available width.
func (w *wrapper) hardLine(s string) {
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			n := escLen(s, i)
			w.write(s[i:i+n], 0)
			i += n
			continue
		}
		g, gw := nextGrapheme(s, i)
		if w.curW+gw > w.avail && w.curW > 0 {
			w.breakLine()
		}
		w.write(g, gw)
		i += len(g)
	}
}

// atom is a breakable unit of a word-wrapped line: the whitespace before it
// (gap, which may also hold escape sequences) and the text (which may hold
// escape sequences too). An atom with empty text is trailing whitespace.
type atom struct {
	gap   string
	gapW  int
	text  string
	width int
}

// atoms splits one hard line into atoms. A hyphen inside a word ends an atom,
// and the next atom continues the word with an empty gap.
func atoms(s string) []atom {
	var out []atom
	var gap, text strings.Builder
	gapW, textW := 0, 0
	inWord := false

	flush := func() {
		if text.Len() == 0 && gap.Len() == 0 {
			return
		}
		out = append(out, atom{gap: gap.String(), gapW: gapW, text: text.String(), width: textW})
		gap.Reset()
		text.Reset()
		gapW, textW = 0, 0
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			n := escLen(s, i)
			if inWord {
				text.WriteString(s[i : i+n])
			} else {
				gap.WriteString(s[i : i+n])
			}
			i += n
			continue
		}
		g, gw := " ", 1
		if c != ' ' {
			g, gw = nextGrapheme(s, i)
		}
		if g == " " || g == "\u3000" { // ASCII or ideographic space
			if inWord {
				flush()
				inWord = false
			}
			gap.WriteString(g)
			gapW += gw
			i += len(g)
			continue
		}
		inWord = true
		text.WriteString(g)
		textW += gw
		i += len(g)
		if g == "-" && textW > 1 && i < len(s) && isWordByte(s[i]) {
			flush()
		}
	}
	flush()
	return out
}

func isWordByte(c byte) bool {
	return c != ' ' && c != '-' && c != 0x1b
}

// escapesOnly returns the escape sequences in s, dropping everything else.
func escapesOnly(s string) string {
	if !strings.Contains(s, "\x1b") {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			n := escLen(s, i)
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		i++
	}
	return b.String()
}

// wordLine emits one input line with word wrapping.
func (w *wrapper) wordLine(s string) {
	for k, a := range atoms(s) {
		if a.text == "" { // trailing whitespace: keep only its escapes
			w.write(escapesOnly(a.gap), 0)
			continue
		}
		gap, gapW := a.gap, a.gapW
		if w.curW == 0 && k > 0 {
			// a wrapped line never starts with spaces
			gap, gapW = escapesOnly(gap), 0
		}
		if w.curW+gapW+a.width <= w.avail {
			w.write(gap, gapW)
			w.write(a.text, a.width)
			continue
		}
		if w.curW > 0 {
			w.write(escapesOnly(gap), 0)
			w.breakLine()
		} else if gapW > 0 && gapW < w.avail {
			w.write(gap, gapW) // leading indentation that still leaves room
		} else {
			w.write(escapesOnly(gap), 0)
		}
		if a.width <= w.avail-w.curW {
			w.write(a.text, a.width)
			continue
		}
		w.hardLine(a.text)
	}
}
