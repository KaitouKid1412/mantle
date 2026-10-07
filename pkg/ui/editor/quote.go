package editor

import "strings"

// SetQuote sets the quote block at the top of the buffer: the leading run of
// "> " lines plus the one blank line after it. An existing block is replaced
// by text, or text is inserted at the top when there is none; empty text
// removes the block. Lines of text without a ">" prefix get "> ". The rest of
// the buffer (chips included) is kept and the cursor goes to the end. It is
// one undo step.
func (e *Editor) SetQuote(text string) {
	end, ok := e.quoteEnd()
	f := quoteFragment(text)
	if !ok && f == nil {
		return
	}
	e.checkpoint(editOther)
	if ok {
		e.deleteRange(Pos{}, end)
	}
	e.cur = Pos{}
	e.insertFragment(f)
	e.cur = e.endPos()
	e.afterEdit()
}

// Quote returns the quote block's lines without its trailing blank line, or
// "" when the buffer has none.
func (e *Editor) Quote() string {
	n := e.quoteLines()
	parts := make([]string, n)
	for i := range n {
		parts[i] = e.Line(i)
	}
	return strings.Join(parts, "\n")
}

func isQuoteLine(s string) bool { return s == ">" || strings.HasPrefix(s, "> ") }

// quoteLines counts the leading quote lines.
func (e *Editor) quoteLines() int {
	n := 0
	for n < len(e.lines) && isQuoteLine(cellsText(e.lines[n].cells)) {
		n++
	}
	return n
}

// quoteEnd is where the quote block ends: after its blank line when there is
// one, else at the start of the next line, else at the end of the buffer.
func (e *Editor) quoteEnd() (Pos, bool) {
	n := e.quoteLines()
	switch {
	case n == 0:
		return Pos{}, false
	case n == len(e.lines):
		return e.endPos(), true
	case len(e.lines[n].cells) == 0 && n+1 < len(e.lines):
		return Pos{n + 1, 0}, true
	default:
		return Pos{n, 0}, true
	}
}

// quoteFragment is text as quote lines plus a blank line, ending at the start
// of the line that follows; nil for empty text.
func quoteFragment(text string) fragment {
	text = strings.TrimRight(normalizeNewlines(text), "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var f fragment
	for _, l := range strings.Split(text, "\n") {
		switch {
		case isQuoteLine(l):
		case l == "":
			l = ">"
		default:
			l = "> " + l
		}
		f = append(f, toCells(l))
	}
	return append(f, nil, nil)
}
