package render

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
)

// Stream renders markdown that arrives in pieces. The source is split into
// top-level blocks; a block is closed once a later block has started (or the
// stream has finished), and closed blocks are rendered once and cached. Only
// the open tail is re-parsed and re-rendered after each Append. An unclosed
// code fence stays open until its fence closes.
//
// Block boundaries do not depend on the width, so ClosedBlocks only ever
// grows: a committer can remember how many closed blocks it has printed.
//
// A Stream is not safe for concurrent use.
type Stream struct {
	o       MarkdownOptions
	src     strings.Builder // sanitized source
	pending string          // raw suffix held back (incomplete escape, rune or CRLF)

	closedEnd int // offset in src where the open tail starts
	closed    []streamBlock
	tail      [][]string
	dirty     bool // src changed since the tail was rendered
	done      bool

	stats StreamStats
}

type streamBlock struct {
	src   string
	lines []string
	valid bool // lines match the current options
}

// StreamStats counts work done by a Stream (for tests and benchmarks).
type StreamStats struct {
	Parses        int // tail parses
	ClosedRenders int // closed blocks rendered (once each, plus option changes)
	TailRenders   int // open tail renders
}

// NewStream returns an empty stream that renders with o.
func NewStream(o MarkdownOptions) *Stream {
	return &Stream{o: o}
}

// Append adds raw engine text. Escape sequences and control characters are
// removed; an escape sequence or UTF-8 rune split across calls is reassembled.
func (s *Stream) Append(delta string) {
	if delta == "" {
		return
	}
	raw := s.pending + delta
	keep := heldSuffix(raw)
	s.pending = raw[len(raw)-keep:]
	raw = raw[:len(raw)-keep]
	if raw == "" {
		return
	}
	s.src.WriteString(sanitizeMarkdown(raw))
	s.dirty = true
}

// Finish marks the end of the text: every block is closed.
func (s *Stream) Finish() {
	if s.done {
		return
	}
	if s.pending != "" {
		s.src.WriteString(sanitizeMarkdown(s.pending))
		s.pending = ""
	}
	s.done = true
	s.dirty = true
}

// Done reports whether Finish was called.
func (s *Stream) Done() bool { return s.done }

// Source returns the sanitized source received so far.
func (s *Stream) Source() string { return s.src.String() }

// Options returns the current rendering options.
func (s *Stream) Options() MarkdownOptions { return s.o }

// SetOptions changes the rendering options (width, palette, …). Closed blocks
// are re-rendered lazily from their source.
func (s *Stream) SetOptions(o MarkdownOptions) {
	s.o = o
	for i := range s.closed {
		s.closed[i].valid = false
	}
	s.dirty = true
}

// ClosedBlocks returns the rendered closed blocks, in order. Blocks that render
// nothing are empty but still counted.
func (s *Stream) ClosedBlocks() [][]string {
	s.update()
	out := make([][]string, len(s.closed))
	for i := range s.closed {
		out[i] = s.closedLines(i)
	}
	return out
}

// NumClosed returns the number of closed blocks.
func (s *Stream) NumClosed() int {
	s.update()
	return len(s.closed)
}

// ClosedBlock returns the rendered lines of closed block i.
func (s *Stream) ClosedBlock(i int) []string {
	s.update()
	return s.closedLines(i)
}

// OpenBlocks returns the rendered blocks of the open tail (usually one).
func (s *Stream) OpenBlocks() [][]string {
	s.update()
	return s.tail
}

// Blocks returns every rendered block: the closed ones, then the open tail.
func (s *Stream) Blocks() [][]string {
	return append(s.ClosedBlocks(), s.OpenBlocks()...)
}

// Lines returns the whole rendering, blocks separated by blank lines. After
// Finish it equals Markdown(Source(), Options()).
func (s *Stream) Lines() []string {
	return JoinBlocks(s.Blocks())
}

// Stats returns work counters.
func (s *Stream) Stats() StreamStats { return s.stats }

func (s *Stream) closedLines(i int) []string {
	b := &s.closed[i]
	if !b.valid {
		b.lines = JoinBlocks(MarkdownBlocks(b.src, s.o))
		b.valid = true
		s.stats.ClosedRenders++
	}
	return b.lines
}

// update re-parses the open tail, closes finished blocks and renders the rest.
func (s *Stream) update() {
	if !s.dirty {
		return
	}
	s.dirty = false
	all := s.src.String()
	tail := []byte(all[s.closedEnd:])
	doc := parseMarkdown(tail)
	s.stats.Parses++

	var kids []ast.Node
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		kids = append(kids, c)
	}
	r := newMDRenderer(tail, s.o)

	// Everything before the last top-level block is closed; after Finish,
	// everything is.
	nclose, boundary := 0, 0
	switch {
	case s.done:
		nclose, boundary = len(kids), len(tail)
	case len(kids) > 1:
		if pos := kids[len(kids)-1].Pos(); pos > 0 {
			if start := lineStart(tail, pos); start > 0 {
				nclose, boundary = len(kids)-1, start
			}
		}
	}
	if nclose > 0 {
		start := 0
		for i := 0; i < nclose; i++ {
			end := boundary
			if i+1 < nclose {
				if p := kids[i+1].Pos(); p > 0 {
					end = lineStart(tail, p)
				}
			}
			end = max(end, start)
			s.closed = append(s.closed, streamBlock{
				src:   string(tail[start:end]),
				lines: r.block(kids[i], r.rootCtx()),
				valid: true,
			})
			s.stats.ClosedRenders++
			start = end
		}
		s.closedEnd += boundary
	}

	s.tail = s.tail[:0]
	if nclose < len(kids) {
		last := kids[len(kids)-1]
		if _, ok := last.(*ast.FencedCodeBlock); ok {
			r.openCode = last
		}
		for _, k := range kids[nclose:] {
			s.tail = append(s.tail, r.block(k, r.rootCtx()))
		}
		s.stats.TailRenders++
	}
}

// lineStart returns the offset of the start of the line containing pos.
func lineStart(b []byte, pos int) int {
	pos = min(pos, len(b))
	for pos > 0 && b[pos-1] != '\n' {
		pos--
	}
	return pos
}

// sanitizeMarkdown cleans engine text for markdown: no escapes or controls,
// and a bare \r is a line break (so the result does not depend on how the
// text was split into deltas).
func sanitizeMarkdown(s string) string {
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return Sanitize(s, SanitizeOptions{})
}

// heldSuffix returns how many trailing bytes of raw must wait for more input:
// an unterminated escape sequence, a partial UTF-8 rune or a trailing \r.
func heldSuffix(raw string) int {
	if i := strings.LastIndexByte(raw, 0x1b); i >= 0 && len(raw)-i < 4096 {
		if n := escLen(raw, i); i+n == len(raw) && !escComplete(raw[i:]) {
			return len(raw) - i
		}
	}
	// partial rune: look back at most 3 bytes for a start byte
	for k := 1; k <= 3 && k <= len(raw); k++ {
		c := raw[len(raw)-k]
		if c < 0x80 {
			break
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRuneInString(raw[len(raw)-k:]) {
				return k
			}
			break
		}
	}
	if strings.HasSuffix(raw, "\r") {
		return 1
	}
	return 0
}

// escComplete reports whether seq (which starts with ESC and runs to the end
// of the input) is a finished escape sequence.
func escComplete(seq string) bool {
	if len(seq) < 2 {
		return false
	}
	last := seq[len(seq)-1]
	switch seq[1] {
	case '[':
		return len(seq) >= 3 && last >= 0x40 && last <= 0x7e
	case ']', 'P', 'X', '^', '_':
		return (seq[1] == ']' && last == 0x07) || strings.HasSuffix(seq, "\x1b\\")
	}
	if seq[1] >= 0x20 && seq[1] <= 0x2f {
		return len(seq) >= 3 && last >= 0x30
	}
	return true
}
