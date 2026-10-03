package diffview

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/aymanbagabas/go-udiff"
	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/KaitouKid1412/mantle/pkg/render"
)

// Hunk is one hunk of a unified diff. Its JSON form matches the engine's
// tool_use_result.structuredPatch entries, so those decode into []Hunk
// directly. Each line starts with ' ', '-' or '+' ('\' lines, "no newline at
// end of file", are ignored).
type Hunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// DefaultContext is the number of unchanged lines FromStrings keeps around a
// change.
const DefaultContext = 3

// FromStrings diffs two texts line by line and returns unified hunks with
// context lines of context (negative means DefaultContext).
func FromStrings(old, new string, context int) []Hunk {
	if old == new {
		return nil
	}
	if context < 0 {
		context = DefaultContext
	}
	edits := udiff.Lines(old, new)
	u, err := udiff.ToUnifiedDiff("old", "new", old, edits, context)
	if err != nil {
		return nil
	}
	hunks := make([]Hunk, 0, len(u.Hunks))
	delta := 0 // new-side line offset; udiff's ToLine drifts after merged edits
	for _, h := range u.Hunks {
		out := Hunk{OldStart: h.FromLine, NewStart: h.FromLine + delta}
		for _, l := range h.Lines {
			text := strings.TrimSuffix(l.Content, "\n")
			switch l.Kind {
			case udiff.Delete:
				out.Lines = append(out.Lines, "-"+text)
				out.OldLines++
			case udiff.Insert:
				out.Lines = append(out.Lines, "+"+text)
				out.NewLines++
			default:
				out.Lines = append(out.Lines, " "+text)
				out.OldLines++
				out.NewLines++
			}
		}
		delta += out.NewLines - out.OldLines
		hunks = append(hunks, out)
	}
	return hunks
}

// Counts returns the number of added and removed lines.
func Counts(hunks []Hunk) (added, removed int) {
	for _, h := range hunks {
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "+"):
				added++
			case strings.HasPrefix(l, "-"):
				removed++
			}
		}
	}
	return added, removed
}

// Options controls Render.
type Options struct {
	// Width is the total width in cells.
	Width int
	// Palette resolves theme tokens; nil renders without colours.
	Palette render.Palette
	// Filename and Lang select syntax highlighting (Lang wins).
	Filename, Lang string
	// Highlighter highlights code; nil uses render.DefaultHighlighter.
	Highlighter *render.Highlighter
	// NoHighlight disables syntax highlighting (syntaxHighlightingDisabled).
	NoHighlight bool
	// NoWordDiff disables word-level highlights within changed line pairs.
	NoWordDiff bool
	// Dimmed renders a rejected edit: muted colours, no highlights.
	Dimmed bool
	// Context folds runs of unchanged lines longer than 2*Context+1 inside a
	// hunk into a marker. Zero shows every line the hunks contain.
	Context int
	// MaxLines caps the output; the last line becomes a "+N lines" footer.
	// Zero means no cap.
	MaxLines int
	// NoGutter hides line numbers.
	NoGutter bool
}

type lineKind uint8

const (
	kindContext lineKind = iota
	kindRemoved
	kindAdded
	kindFold // folded unchanged lines; num holds the count
	kindGap  // between hunks
)

// row is one diff line prepared for rendering.
type row struct {
	kind  lineKind
	num   int
	text  string
	segs  []render.Segment // syntax-highlighted text
	words [][2]int         // byte ranges to emphasise
}

type styles struct {
	addBg, remBg         render.Style
	addWord, remWord     render.Style
	addMark, remMark     render.Style
	gutter, fold, footer render.Style
	text                 render.Style
}

func newStyles(o Options) styles {
	p := o.Palette
	if p == nil {
		p = render.NoColor
	}
	bg := func(tok string) render.Style { return render.Style{Bg: p.Color(tok)} }
	s := styles{
		addBg:   bg(render.TokDiffAdded),
		remBg:   bg(render.TokDiffRemoved),
		addWord: bg(render.TokDiffAddedWord),
		remWord: bg(render.TokDiffRemovedWord),
		gutter:  render.Fg(p, render.TokInactive),
		fold:    render.Fg(p, render.TokSubtle),
		footer:  render.Fg(p, render.TokInactive),
		text:    render.Fg(p, render.TokText),
	}
	if o.Dimmed {
		s.addBg = bg(render.TokDiffAddedDimmed)
		s.remBg = bg(render.TokDiffRemovedDimmed)
		s.text = render.Fg(p, render.TokInactive)
	}
	// Without background colours (NoColor palettes, ANSI-less themes) keep
	// the change visible through the marker and word attributes.
	if s.addBg.Bg == nil {
		s.addMark = render.Fg(p, render.TokSuccess)
	}
	if s.remBg.Bg == nil {
		s.remMark = render.Fg(p, render.TokError)
	}
	if s.addWord.Bg == nil {
		s.addWord = render.Style{Underline: true}
	}
	if s.remWord.Bg == nil {
		s.remWord = render.Style{Underline: true}
	}
	return s
}

// Render renders hunks as styled lines at o.Width: a line-number gutter,
// a +/- marker, coloured backgrounds for changed lines, word-level highlights
// within changed line pairs, a separator between hunks, and an optional cap.
func Render(hunks []Hunk, o Options) []string {
	if o.Width < 1 {
		o.Width = 1
	}
	rows := prepare(hunks, o)
	if len(rows) == 0 {
		return nil
	}
	st := newStyles(o)

	gw := 0
	if !o.NoGutter {
		for _, r := range rows {
			if r.kind <= kindAdded {
				gw = max(gw, len(strconv.Itoa(r.num)))
			}
		}
	}
	prefixW := 2 // marker + space
	if gw > 0 {
		prefixW += gw + 1
	}
	if prefixW >= o.Width { // too narrow for a gutter
		gw, prefixW = 0, 2
	}

	var out []string
	for i, r := range rows {
		lines := renderRow(r, gw, o, st)
		if o.MaxLines > 0 && len(out)+len(lines) > o.MaxLines {
			keep := o.MaxLines - 1
			hidden := countLines(rows[i:])
			if len(out) > keep {
				out = out[:keep]
				hidden++
			} else if room := keep - len(out); room > 0 {
				out = append(out, lines[:room]...)
				if r.kind <= kindAdded {
					hidden-- // partly shown
				}
			}
			footer := strings.Repeat(" ", max(prefixW-2, 0)) + "… +" + strconv.Itoa(max(hidden, 1)) + " lines"
			return append(out, render.Truncate(st.footer.Render(footer), o.Width, "…"))
		}
		out = append(out, lines...)
	}
	return out
}

// countLines counts the diff lines rows stand for.
func countLines(rows []row) int {
	n := 0
	for _, r := range rows {
		switch {
		case r.kind <= kindAdded:
			n++
		case r.kind == kindFold:
			n += r.num
		}
	}
	return n
}

func renderRow(r row, gw int, o Options, st styles) []string {
	switch r.kind {
	case kindGap:
		return []string{st.fold.Render(strings.Repeat(" ", gw) + " ...")}
	case kindFold:
		label := "⋯ " + strconv.Itoa(r.num) + " unchanged line"
		if r.num != 1 {
			label += "s"
		}
		return []string{render.Truncate(st.fold.Render(strings.Repeat(" ", gw)+" "+label), o.Width, "…")}
	}

	var bg, word, mark render.Style
	marker := " "
	switch r.kind {
	case kindRemoved:
		bg, word, mark, marker = st.remBg, st.remWord, st.remMark, "-"
	case kindAdded:
		bg, word, mark, marker = st.addBg, st.addWord, st.addMark, "+"
	}

	gutter := ""
	if gw > 0 {
		n := strconv.Itoa(r.num)
		gutter = strings.Repeat(" ", gw-len(n)) + n + " "
	}
	first := st.gutter.Render(gutter) + bg.Merge(mark).Render(marker+" ")
	rest := strings.Repeat(" ", len(gutter)) + bg.Render("  ")

	content := renderContent(r, bg, word, st.text, o.Dimmed)
	lines := render.WrapWith(content, render.WrapOptions{Width: o.Width, First: first, Rest: rest, Hard: true})
	if bg.Bg != nil {
		// extend the background to the full width
		for i, l := range lines {
			if w := render.Width(l); w < o.Width {
				lines[i] = l + bg.Render(strings.Repeat(" ", o.Width-w))
			}
		}
	}
	return lines
}

// renderContent styles a line's text: syntax colours (or the text colour),
// the line background, and word emphasis on the changed ranges.
func renderContent(r row, bg, word, text render.Style, dimmed bool) string {
	segs := r.segs
	if segs == nil || dimmed {
		segs = []render.Segment{{Text: r.text, Style: text}}
	}
	var b strings.Builder
	for _, s := range overlay(segs, r.words) {
		style := bg.Merge(s.Style)
		if s.marked {
			style = style.Merge(word)
		}
		b.WriteString(style.Render(s.Text))
	}
	return b.String()
}

type markedSegment struct {
	render.Segment
	marked bool
}

// overlay splits segments at the given byte ranges and marks the parts the
// ranges cover.
func overlay(segs []render.Segment, ranges [][2]int) []markedSegment {
	out := make([]markedSegment, 0, len(segs)+2*len(ranges))
	pos, ri := 0, 0
	for _, s := range segs {
		text := s.Text
		for text != "" {
			for ri < len(ranges) && ranges[ri][1] <= pos {
				ri++
			}
			n := len(text)
			marked := false
			if ri < len(ranges) {
				lo, hi := ranges[ri][0], ranges[ri][1]
				if pos >= lo {
					marked = true
					n = min(n, hi-pos)
				} else {
					n = min(n, lo-pos)
				}
			}
			out = append(out, markedSegment{render.Segment{Text: text[:n], Style: s.Style}, marked})
			text = text[n:]
			pos += n
		}
	}
	return out
}

// prepare turns hunks into rows: numbering, sanitising, highlighting, word
// ranges and context folding.
func prepare(hunks []Hunk, o Options) []row {
	var hl *render.Highlighter
	if !o.NoHighlight && !o.Dimmed {
		hl = o.Highlighter
		if hl == nil {
			hl = render.DefaultHighlighter
		}
	}
	p := o.Palette
	if p == nil {
		p = render.NoColor
	}
	var rows []row
	for hi, h := range hunks {
		if hi > 0 {
			rows = append(rows, row{kind: kindGap})
		}
		var hr []row
		oldNo, newNo := h.OldStart, h.NewStart
		var oldSide, newSide []string
		var oldIdx, newIdx []int // per hunk row: index into its side
		for _, l := range h.Lines {
			if l == "" {
				l = " "
			}
			text := render.Sanitize(l[1:], render.SanitizeOptions{})
			text = strings.ReplaceAll(text, "\n", " ")
			switch l[0] {
			case '-':
				hr = append(hr, row{kind: kindRemoved, num: oldNo, text: text})
				oldIdx = append(oldIdx, len(oldSide))
				newIdx = append(newIdx, -1)
				oldSide = append(oldSide, text)
				oldNo++
			case '+':
				hr = append(hr, row{kind: kindAdded, num: newNo, text: text})
				oldIdx = append(oldIdx, -1)
				newIdx = append(newIdx, len(newSide))
				newSide = append(newSide, text)
				newNo++
			case '\\':
				continue
			default:
				hr = append(hr, row{kind: kindContext, num: newNo, text: text})
				oldIdx = append(oldIdx, len(oldSide))
				newIdx = append(newIdx, len(newSide))
				oldSide = append(oldSide, text)
				newSide = append(newSide, text)
				oldNo++
				newNo++
			}
		}
		if hl != nil && (o.Lang != "" || o.Filename != "") {
			oldSegs := hl.Segments(strings.Join(oldSide, "\n"), o.Lang, o.Filename, p)
			newSegs := hl.Segments(strings.Join(newSide, "\n"), o.Lang, o.Filename, p)
			for i := range hr {
				switch {
				case hr[i].kind == kindRemoved && oldIdx[i] < len(oldSegs):
					hr[i].segs = oldSegs[oldIdx[i]]
				case hr[i].kind != kindRemoved && newIdx[i] >= 0 && newIdx[i] < len(newSegs):
					hr[i].segs = newSegs[newIdx[i]]
				}
			}
		}
		if !o.NoWordDiff && !o.Dimmed {
			pairWords(hr)
		}
		if o.Context > 0 {
			hr = fold(hr, o.Context)
		}
		rows = append(rows, hr...)
	}
	return rows
}

// pairWords pairs the removed and added lines of each change block and marks
// the words that differ.
func pairWords(rows []row) {
	for i := 0; i < len(rows); {
		if rows[i].kind != kindRemoved {
			i++
			continue
		}
		j := i
		for j < len(rows) && rows[j].kind == kindRemoved {
			j++
		}
		k := j
		for k < len(rows) && rows[k].kind == kindAdded {
			k++
		}
		for n := 0; n < min(j-i, k-j); n++ {
			a, b := &rows[i+n], &rows[j+n]
			a.words, b.words = wordRanges(a.text, b.text)
		}
		i = k
	}
}

// wordRanges returns the byte ranges of words that differ between a and b.
// Lines that are mostly rewritten get no word ranges (the whole line is the
// change).
func wordRanges(a, b string) (ra, rb [][2]int) {
	ta, tb := tokenize(a), tokenize(b)
	ids := map[string]rune{}
	enc := func(toks []string) []rune {
		rs := make([]rune, len(toks))
		for i, t := range toks {
			id, ok := ids[t]
			if !ok {
				id = rune(0xE000 + len(ids))
				ids[t] = id
			}
			rs[i] = id
		}
		return rs
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMainRunes(enc(ta), enc(tb), false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	var ia, ib, pa, pb int // token index and byte offset in a and b
	var changedA, changedB int
	for _, d := range diffs {
		n := len([]rune(d.Text))
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			for k := 0; k < n; k++ {
				pa += len(ta[ia])
				pb += len(tb[ib])
				ia++
				ib++
			}
		case diffmatchpatch.DiffDelete:
			start := pa
			for k := 0; k < n; k++ {
				pa += len(ta[ia])
				ia++
			}
			ra = appendRange(ra, start, pa, a)
			changedA += pa - start
		case diffmatchpatch.DiffInsert:
			start := pb
			for k := 0; k < n; k++ {
				pb += len(tb[ib])
				ib++
			}
			rb = appendRange(rb, start, pb, b)
			changedB += pb - start
		}
	}
	if tooMuch(changedA, a) || tooMuch(changedB, b) {
		return nil, nil
	}
	return ra, rb
}

// appendRange adds [lo, hi) unless it is whitespace only, merging with the
// previous range when only whitespace separates them.
func appendRange(rs [][2]int, lo, hi int, s string) [][2]int {
	if strings.TrimSpace(s[lo:hi]) == "" {
		return rs
	}
	if n := len(rs); n > 0 && strings.TrimSpace(s[rs[n-1][1]:lo]) == "" {
		rs[n-1][1] = hi
		return rs
	}
	return append(rs, [2]int{lo, hi})
}

func tooMuch(changed int, s string) bool {
	total := len(strings.TrimSpace(s))
	return total > 0 && changed*10 > total*6
}

// tokenize splits a line into words (letters, digits, _), runs of spaces and
// single other characters.
func tokenize(s string) []string {
	var toks []string
	start := -1
	class := 0
	cls := func(r rune) int {
		switch {
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			return 1
		case unicode.IsSpace(r):
			return 2
		}
		return 3
	}
	for i, r := range s {
		c := cls(r)
		if start >= 0 && (c != class || c == 3) {
			toks = append(toks, s[start:i])
			start = -1
		}
		if start < 0 {
			start, class = i, c
		}
	}
	if start >= 0 {
		toks = append(toks, s[start:])
	}
	return toks
}

// fold collapses long runs of unchanged lines, keeping ctx lines next to
// each change.
func fold(rows []row, ctx int) []row {
	var out []row
	for i := 0; i < len(rows); {
		if rows[i].kind != kindContext {
			out = append(out, rows[i])
			i++
			continue
		}
		j := i
		for j < len(rows) && rows[j].kind == kindContext {
			j++
		}
		keepHead, keepTail := ctx, ctx
		if i == 0 {
			keepHead = 0
		}
		if j == len(rows) {
			keepTail = 0
		}
		if n := j - i; n > keepHead+keepTail+1 {
			out = append(out, rows[i:i+keepHead]...)
			out = append(out, row{kind: kindFold, num: n - keepHead - keepTail})
			out = append(out, rows[j-keepTail:j]...)
		} else {
			out = append(out, rows[i:j]...)
		}
		i = j
	}
	return out
}
