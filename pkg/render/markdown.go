package render

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// MarkdownOptions controls markdown rendering.
type MarkdownOptions struct {
	// Width is the number of columns available. Code blocks and tables may use
	// all of it.
	Width int
	// MaxProseWidth caps the width of paragraphs, headings, lists and quotes
	// (the maxProseWidth setting). Zero means no cap.
	MaxProseWidth int
	// Palette resolves theme tokens; nil renders without colours.
	Palette Palette
	// Highlighter highlights fenced code; nil uses DefaultHighlighter.
	Highlighter *Highlighter
	// NoHighlight disables syntax highlighting (syntaxHighlightingDisabled).
	NoHighlight bool
	// NoHyperlinks drops OSC 8 sequences; link URLs are still shown as text.
	NoHyperlinks bool
	// KeepLineBreaks renders a single newline inside a paragraph as a line
	// break (Claude Code shows answers line by line) instead of a space.
	KeepLineBreaks bool
	// TabWidth and TabColumn place tab stops in code blocks: every TabWidth
	// columns (default 4) counted from the screen edge, where the rendering
	// starts at column TabColumn. A terminal printing a raw tab uses 8.
	TabWidth, TabColumn int
}

var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

func parseMarkdown(src []byte) ast.Node {
	return mdParser.Parse(text.NewReader(src))
}

// Markdown renders markdown source to styled lines at o.Width. Top-level
// blocks are separated by one blank line. Engine text is untrusted: escape
// sequences and control characters in src are removed first.
func Markdown(src string, o MarkdownOptions) []string {
	return JoinBlocks(MarkdownBlocks(src, o))
}

// MarkdownBlocks renders src and returns the lines of each top-level block
// separately (no separators). Blocks that render nothing are empty.
func MarkdownBlocks(src string, o MarkdownOptions) [][]string {
	b := []byte(sanitizeMarkdown(src))
	doc := parseMarkdown(b)
	r := newMDRenderer(b, o)
	var out [][]string
	for c := doc.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.block(c, r.rootCtx()))
	}
	return out
}

// JoinBlocks joins rendered blocks with one blank line between non-empty ones.
func JoinBlocks(blocks [][]string) []string {
	var out []string
	for _, b := range blocks {
		if len(b) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, b...)
	}
	return out
}

type mdStyles struct {
	h1, h2, h3, h4 Style
	code           Style
	link           Style
	url            Style
	quoteBar       Style
	quote          Style
	rule           Style
	marker         Style
	border         Style
	header         Style
	html           Style
	checked        Style
}

type mdRenderer struct {
	src []byte
	o   MarkdownOptions
	st  mdStyles
	hl  *Highlighter

	// openCode is the fenced code block still being streamed (its last line
	// may be partial), or nil.
	openCode ast.Node
	// checkMarker means the next task checkbox is already shown as the list
	// marker; trimSpace then drops the space that followed it.
	checkMarker bool
	trimSpace   bool
}

func newMDRenderer(src []byte, o MarkdownOptions) *mdRenderer {
	p := o.Palette
	if p == nil {
		p = NoColor
	}
	if o.Width < 1 {
		o.Width = 1
	}
	r := &mdRenderer{src: src, o: o}
	if !o.NoHighlight {
		r.hl = o.Highlighter
		if r.hl == nil {
			r.hl = DefaultHighlighter
		}
	}
	r.st = mdStyles{
		h1:       Style{Bold: true, Underline: true},
		h2:       Style{Bold: true},
		h3:       Style{Bold: true, Italic: true},
		h4:       Style{Italic: true},
		code:     Fg(p, TokPermission),
		link:     Fg(p, TokPermission),
		url:      Fg(p, TokInactive),
		quoteBar: Fg(p, TokSubtle),
		quote:    Fg(p, TokInactive).Merge(Style{Italic: true}),
		rule:     Fg(p, TokSubtle),
		marker:   Style{},
		border:   Fg(p, TokSubtle),
		header:   Style{Bold: true},
		html:     Fg(p, TokInactive),
		checked:  Fg(p, TokSuccess),
	}
	return r
}

// mdCtx is the layout context of a block.
type mdCtx struct {
	width int   // columns for code and tables
	prose int   // columns for prose
	base  Style // inherited inline style
	depth int   // ordered-list nesting depth (numbering style)
}

func (r *mdRenderer) rootCtx() mdCtx {
	c := mdCtx{width: r.o.Width, prose: r.o.Width}
	if m := r.o.MaxProseWidth; m > 0 && m < c.prose {
		c.prose = m
	}
	return c
}

func (c mdCtx) indent(n int) mdCtx {
	c.width = max(c.width-n, 1)
	c.prose = max(c.prose-n, 1)
	return c
}

func (r *mdRenderer) block(n ast.Node, c mdCtx) []string {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return r.wrapProse(r.inlineString(n, c.base), c)
	case *ast.Heading:
		st := r.st.h4
		switch n.Level {
		case 1:
			st = r.st.h1
		case 2:
			st = r.st.h2
		case 3:
			st = r.st.h3
		}
		return r.wrapProse(r.inlineString(n, c.base.Merge(st)), c)
	case *ast.ThematicBreak:
		return []string{r.st.rule.Render(strings.Repeat("─", c.prose))}
	case *ast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = string(n.Info.Segment.Value(r.src))
		}
		return r.code(r.rawLines(n), lang, n == r.openCode, c)
	case *ast.CodeBlock:
		return r.code(r.rawLines(n), "", false, c)
	case *ast.Blockquote:
		bar := r.st.quoteBar.Render("│") + " "
		inner := c.indent(2)
		inner.base = c.base.Merge(r.st.quote)
		lines := r.children(n, inner, true)
		for i, l := range lines {
			if l == "" {
				lines[i] = r.st.quoteBar.Render("│")
			} else {
				lines[i] = bar + l
			}
		}
		return lines
	case *ast.List:
		return r.list(n, c)
	case *extast.Table:
		return r.table(n, c)
	case *ast.HTMLBlock:
		raw := strings.TrimRight(r.rawLines(n), "\n")
		if n.HasClosure() {
			raw += "\n" + strings.TrimRight(string(n.ClosureLine.Value(r.src)), "\n")
		}
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		return WrapWith(r.st.html.Render(raw), WrapOptions{Width: c.prose})
	case *ast.LinkReferenceDefinition:
		return nil
	}
	if n.HasChildren() {
		return r.children(n, c, true)
	}
	if raw := strings.TrimRight(r.rawLines(n), "\n"); raw != "" {
		return WrapWith(raw, WrapOptions{Width: c.prose})
	}
	return nil
}

// children renders the child blocks of n, optionally with blank lines between.
func (r *mdRenderer) children(n ast.Node, c mdCtx, sep bool) []string {
	var out []string
	for ch := n.FirstChild(); ch != nil; ch = ch.NextSibling() {
		lines := r.block(ch, c)
		if len(lines) == 0 {
			continue
		}
		if sep && len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	return out
}

func (r *mdRenderer) wrapProse(s string, c mdCtx) []string {
	if s == "" {
		return nil
	}
	return WrapWith(s, WrapOptions{Width: c.prose})
}

// rawLines returns the source lines of a leaf block.
func (r *mdRenderer) rawLines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(r.src))
	}
	return b.String()
}

// code renders a code block: highlighted, hard-wrapped at the full width.
// While a block is streaming, its complete lines are highlighted together
// (and cached) and the partial last line on its own, so each delta does not
// re-lex the whole block.
func (r *mdRenderer) code(src, lang string, open bool, c mdCtx) []string {
	src = strings.TrimSuffix(src, "\n")
	if src == "" && !open {
		return nil
	}
	var lines []string
	p := r.o.Palette
	if p == nil {
		p = NoColor
	}
	if open {
		head, tail := "", src
		if i := strings.LastIndexByte(src, '\n'); i >= 0 {
			head, tail = src[:i], src[i+1:]
			lines = r.hl.Highlight(head, lang, "", p)
		}
		if tail != "" || head == "" {
			lines = append(lines, r.hl.Highlight(tail, lang, "", p)...)
		} else {
			lines = append(lines, "")
		}
	} else {
		lines = r.hl.Highlight(src, lang, "", p)
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, WrapWith(l, WrapOptions{Width: c.width, Hard: true,
			TabWidth: r.o.TabWidth, TabStart: r.o.TabColumn + r.o.Width - c.width})...)
	}
	return out
}

// ---- lists ----

func (r *mdRenderer) list(l *ast.List, c mdCtx) []string {
	var markers []string
	var checks []int // 0 none, 1 unchecked, 2 checked
	i := 0
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		m := "-"
		if l.IsOrdered() {
			m = orderedMarker(l.Start+i, c.depth) + "."
		}
		chk := 0
		if !l.IsOrdered() {
			if cb := taskCheckBox(it); cb != nil {
				chk = 1
				m = "☐"
				if cb.IsChecked {
					chk = 2
					m = "☒"
				}
			}
		}
		markers = append(markers, m)
		checks = append(checks, chk)
		i++
	}
	mw := 0
	for _, m := range markers {
		mw = max(mw, Width(m))
	}
	mw++ // space after the marker

	inner := c.indent(mw)
	if l.IsOrdered() {
		inner.depth = c.depth + 1
	}
	pad := strings.Repeat(" ", mw)
	var out []string
	i = 0
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		r.checkMarker = checks[i] != 0
		lines := r.children(it, inner, !l.IsTight)
		r.checkMarker, r.trimSpace = false, false
		if !l.IsTight && i > 0 {
			out = append(out, "")
		}
		mst := r.st.marker
		if checks[i] == 2 {
			mst = r.st.checked
		}
		marker := mst.Render(markers[i]) + strings.Repeat(" ", mw-Width(markers[i]))
		if len(lines) == 0 {
			out = append(out, strings.TrimRight(marker, " "))
		}
		for j, line := range lines {
			switch {
			case j == 0:
				out = append(out, marker+line)
			case line == "":
				out = append(out, "")
			default:
				out = append(out, pad+line)
			}
		}
		i++
	}
	return out
}

// taskCheckBox returns the GFM task checkbox that starts a list item, if any.
func taskCheckBox(item ast.Node) *extast.TaskCheckBox {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	cb, _ := first.FirstChild().(*extast.TaskCheckBox)
	return cb
}

// orderedMarker numbers list items: 1, 2, 3 at the top level, then a, b, c,
// then i, ii, iii, repeating with depth.
func orderedMarker(n, depth int) string {
	switch depth % 3 {
	case 1:
		return alphaNumber(n)
	case 2:
		return romanNumber(n)
	}
	return strconv.Itoa(n)
}

func alphaNumber(n int) string {
	if n < 1 {
		return strconv.Itoa(n)
	}
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('a' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

func romanNumber(n int) string {
	if n < 1 || n > 3999 {
		return strconv.Itoa(n)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// ---- inline ----

// inlineBuf accumulates styled text, merging adjacent runs of equal style.
type inlineBuf struct {
	b    strings.Builder
	open string
	run  strings.Builder
}

func (ib *inlineBuf) text(st Style, s string) {
	if s == "" {
		return
	}
	open := st.Open()
	if open != ib.open {
		ib.flush()
		ib.open = open
	}
	ib.run.WriteString(s)
}

func (ib *inlineBuf) raw(s string) {
	ib.flush()
	ib.b.WriteString(s)
}

func (ib *inlineBuf) flush() {
	if ib.run.Len() == 0 {
		return
	}
	if ib.open != "" {
		ib.b.WriteString(ib.open)
		ib.b.WriteString(ib.run.String())
		ib.b.WriteString("\x1b[m")
	} else {
		ib.b.WriteString(ib.run.String())
	}
	ib.run.Reset()
}

func (ib *inlineBuf) String() string {
	ib.flush()
	return ib.b.String()
}

func (r *mdRenderer) inlineString(n ast.Node, st Style) string {
	var ib inlineBuf
	r.inline(n, st, &ib)
	return ib.String()
}

func (r *mdRenderer) inline(n ast.Node, st Style, ib *inlineBuf) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			v := c.Segment.Value(r.src)
			s := ""
			if c.IsRaw() {
				s = string(v)
			} else {
				s = unescapeText(v)
			}
			if r.trimSpace {
				s = strings.TrimLeft(s, " ")
				r.trimSpace = s == ""
			}
			ib.text(st, s)
			switch {
			case c.HardLineBreak():
				ib.text(st, "\n")
			case c.SoftLineBreak():
				if r.o.KeepLineBreaks {
					ib.text(st, "\n")
				} else {
					ib.text(st, " ")
				}
			}
		case *ast.String:
			ib.text(st, Sanitize(string(c.Value), SanitizeOptions{}))
		case *ast.CodeSpan:
			var b strings.Builder
			for t := c.FirstChild(); t != nil; t = t.NextSibling() {
				if tx, ok := t.(*ast.Text); ok {
					b.Write(bytes.ReplaceAll(tx.Segment.Value(r.src), []byte("\n"), []byte(" ")))
				}
			}
			ib.text(st.Merge(r.st.code), b.String())
		case *ast.Emphasis:
			e := Style{Italic: true}
			switch {
			case c.Level == 2:
				e = Style{Bold: true}
			case c.Level > 2:
				e = Style{Bold: true, Italic: true}
			}
			r.inline(c, st.Merge(e), ib)
		case *extast.Strikethrough:
			r.inline(c, st.Merge(Style{Strike: true}), ib)
		case *ast.Link:
			url := cleanURL(string(c.Destination))
			label := r.inlineString(c, st.Merge(r.st.link))
			r.link(ib, st, url, label)
		case *ast.Image:
			url := cleanURL(string(c.Destination))
			alt := Strip(r.inlineString(c, Style{}))
			if alt == "" {
				alt = "image"
			}
			r.link(ib, st, url, st.Merge(r.st.link).Render("["+alt+"]"))
		case *ast.AutoLink:
			label := Sanitize(string(c.Label(r.src)), SanitizeOptions{})
			url := cleanURL(string(c.URL(r.src)))
			if c.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(url), "mailto:") {
				url = "mailto:" + url
			}
			r.linkOnly(ib, url, st.Merge(r.st.link).Render(label))
		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < c.Segments.Len(); i++ {
				seg := c.Segments.At(i)
				b.Write(seg.Value(r.src))
			}
			h := Sanitize(b.String(), SanitizeOptions{})
			if isBreakTag(h) {
				ib.text(st, "\n")
			} else {
				ib.text(st.Merge(r.st.html), h)
			}
		case *extast.TaskCheckBox:
			if r.checkMarker {
				r.checkMarker, r.trimSpace = false, true
				continue
			}
			if c.IsChecked {
				ib.text(st, "☒ ")
			} else {
				ib.text(st, "☐ ")
			}
		default:
			r.inline(c, st, ib)
		}
	}
}

// link renders link text as an OSC 8 hyperlink and, when the visible text is
// not the URL itself, the URL after it.
func (r *mdRenderer) link(ib *inlineBuf, st Style, url, label string) {
	if url == "" {
		ib.raw(label)
		return
	}
	r.linkOnly(ib, url, label)
	if plain := Strip(label); plain != url && "mailto:"+plain != url {
		ib.text(st, " ")
		r.linkOnly(ib, url, st.Merge(r.st.url).Render("("+url+")"))
	}
}

func (r *mdRenderer) linkOnly(ib *inlineBuf, url, label string) {
	if r.o.NoHyperlinks || url == "" {
		ib.raw(label)
		return
	}
	ib.raw(Link(url, label))
}

// cleanURL removes characters that could break out of an OSC 8 sequence.
func cleanURL(u string) string {
	u = Sanitize(u, SanitizeOptions{})
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(u))
}

func isBreakTag(h string) bool {
	h = strings.ToLower(strings.ReplaceAll(h, " ", ""))
	return h == "<br>" || h == "<br/>"
}

// unescapeText resolves backslash escapes and entity references, then removes
// any control characters an entity may have produced.
func unescapeText(v []byte) string {
	if bytes.IndexByte(v, '\\') < 0 && bytes.IndexByte(v, '&') < 0 {
		return string(v)
	}
	v = util.UnescapePunctuations(v)
	v = util.ResolveNumericReferences(v)
	v = util.ResolveEntityNames(v)
	return Sanitize(string(v), SanitizeOptions{})
}

// ---- tables ----

func (r *mdRenderer) table(t *extast.Table, c mdCtx) []string {
	var rows [][]string
	var aligns []extast.Alignment
	aligns = append(aligns, t.Alignments...)
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		st := c.base
		if _, ok := row.(*extast.TableHeader); ok {
			st = st.Merge(r.st.header)
		}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			cells = append(cells, r.inlineString(cell, st))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil
	}
	ncol := 0
	for _, row := range rows {
		ncol = max(ncol, len(row))
	}
	for i := range rows {
		for len(rows[i]) < ncol {
			rows[i] = append(rows[i], "")
		}
	}
	for len(aligns) < ncol {
		aligns = append(aligns, extast.AlignNone)
	}

	natural := make([]int, ncol)
	for _, row := range rows {
		for i, cell := range row {
			natural[i] = max(natural[i], Width(cell))
		}
	}
	widths := append([]int(nil), natural...)
	minW := make([]int, ncol)
	sum, minSum := 0, 0
	for i, w := range natural {
		widths[i] = max(w, 1)
		minW[i] = min(widths[i], 3)
		sum += widths[i]
		minSum += minW[i]
	}
	frame := 3*ncol + 1
	if minSum+frame > c.width {
		return r.tableVertical(rows, c)
	}
	for excess := sum + frame - c.width; excess > 0; excess-- {
		k := -1
		for i := range widths {
			if widths[i] > minW[i] && (k < 0 || widths[i] > widths[k]) {
				k = i
			}
		}
		if k < 0 {
			break
		}
		widths[k]--
	}

	b := r.st.border
	hline := func(l, m, rr string) string {
		var s strings.Builder
		s.WriteString(l)
		for i, w := range widths {
			if i > 0 {
				s.WriteString(m)
			}
			s.WriteString(strings.Repeat("─", w+2))
		}
		s.WriteString(rr)
		return b.Render(s.String())
	}
	bar := b.Render("│")
	out := []string{hline("┌", "┬", "┐")}
	for ri, row := range rows {
		cellLines := make([][]string, ncol)
		height := 1
		for i, cell := range row {
			cellLines[i] = Wrap(cell, widths[i])
			height = max(height, len(cellLines[i]))
		}
		for li := 0; li < height; li++ {
			var s strings.Builder
			s.WriteString(bar)
			for i := range row {
				text := ""
				if li < len(cellLines[i]) {
					text = cellLines[i][li]
				}
				s.WriteString(" ")
				s.WriteString(alignCell(text, widths[i], aligns[i]))
				s.WriteString(" ")
				s.WriteString(bar)
			}
			out = append(out, s.String())
		}
		if ri == 0 && len(rows) > 1 {
			out = append(out, hline("├", "┼", "┤"))
		}
	}
	out = append(out, hline("└", "┴", "┘"))
	return out
}

func alignCell(s string, width int, a extast.Alignment) string {
	gap := width - Width(s)
	if gap <= 0 {
		return s
	}
	switch a {
	case extast.AlignRight:
		return strings.Repeat(" ", gap) + s
	case extast.AlignCenter:
		l := gap / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", gap-l)
	}
	return s + strings.Repeat(" ", gap)
}

// tableVertical lays a table out as "header: value" lines per row when its
// columns cannot fit side by side.
func (r *mdRenderer) tableVertical(rows [][]string, c mdCtx) []string {
	header := rows[0]
	var out []string
	for ri, row := range rows[1:] {
		if ri > 0 {
			out = append(out, r.st.rule.Render(strings.Repeat("─", min(c.prose, 20))))
		}
		for i, cell := range row {
			label := Strip(header[i])
			if label == "" {
				label = strconv.Itoa(i + 1)
			}
			out = append(out, WrapWith(r.st.header.Render(label+":")+" "+cell, WrapOptions{Width: c.prose, Rest: "  "})...)
		}
	}
	if len(rows) == 1 {
		out = append(out, WrapWith(strings.Join(header, " · "), WrapOptions{Width: c.prose})...)
	}
	return out
}
