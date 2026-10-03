package render

import (
	"container/list"
	"hash/maphash"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Highlighting limits: above either, code is shown unhighlighted (chroma's
// regex lexers get slow on large inputs).
const (
	DefaultHighlightMaxLines = 2000
	DefaultHighlightMaxBytes = 200 << 10
)

// syntaxClass is a highlight class; each maps to one theme syntax token.
type syntaxClass uint8

const (
	clsPlain syntaxClass = iota
	clsKeyword
	clsString
	clsNumber
	clsComment
	clsFunction
	clsType
	clsVariable
	clsConstant
	clsOperator
	clsPunctuation
	clsTag
	clsAttribute
	clsBuiltin
	clsInserted
	clsDeleted
	clsHeading
	clsEmph
	clsStrong
	clsPrompt
	numClasses
)

func (c syntaxClass) style(p Palette) Style {
	switch c {
	case clsKeyword:
		return Fg(p, TokSyntaxKeyword)
	case clsString:
		return Fg(p, TokSyntaxString)
	case clsNumber:
		return Fg(p, TokSyntaxNumber)
	case clsComment:
		return Fg(p, TokSyntaxComment).Merge(Style{Italic: true})
	case clsFunction:
		return Fg(p, TokSyntaxFunction)
	case clsType:
		return Fg(p, TokSyntaxType)
	case clsVariable:
		return Fg(p, TokSyntaxVariable)
	case clsConstant:
		return Fg(p, TokSyntaxConstant)
	case clsOperator:
		return Fg(p, TokSyntaxOperator)
	case clsPunctuation:
		return Fg(p, TokSyntaxPunctuation)
	case clsTag:
		return Fg(p, TokSyntaxTag)
	case clsAttribute:
		return Fg(p, TokSyntaxAttribute)
	case clsBuiltin:
		return Fg(p, TokSyntaxBuiltin)
	case clsInserted:
		return Fg(p, TokDiffAdded)
	case clsDeleted:
		return Fg(p, TokDiffRemoved)
	case clsHeading:
		return Fg(p, TokSyntaxKeyword).Merge(Style{Bold: true})
	case clsEmph:
		return Fg(p, TokSyntaxPlain).Merge(Style{Italic: true})
	case clsStrong:
		return Fg(p, TokSyntaxPlain).Merge(Style{Bold: true})
	case clsPrompt:
		return Fg(p, TokInactive)
	}
	return Fg(p, TokSyntaxPlain)
}

// classify maps a chroma token type onto a highlight class.
func classify(t chroma.TokenType) syntaxClass {
	switch t {
	case chroma.KeywordType:
		return clsType
	case chroma.KeywordConstant:
		return clsConstant
	case chroma.NameFunction, chroma.NameFunctionMagic, chroma.NameDecorator:
		return clsFunction
	case chroma.NameClass, chroma.NameNamespace, chroma.NameException:
		return clsType
	case chroma.NameBuiltin, chroma.NameBuiltinPseudo:
		return clsBuiltin
	case chroma.NameTag:
		return clsTag
	case chroma.NameAttribute, chroma.NameProperty:
		return clsAttribute
	case chroma.NameConstant, chroma.NameEntity, chroma.NameLabel:
		return clsConstant
	case chroma.OperatorWord:
		return clsKeyword
	case chroma.CommentPreproc, chroma.CommentPreprocFile:
		return clsKeyword
	case chroma.GenericInserted:
		return clsInserted
	case chroma.GenericDeleted:
		return clsDeleted
	case chroma.GenericHeading, chroma.GenericSubheading:
		return clsHeading
	case chroma.GenericEmph:
		return clsEmph
	case chroma.GenericStrong:
		return clsStrong
	case chroma.GenericPrompt:
		return clsPrompt
	}
	switch {
	case t.InSubCategory(chroma.NameVariable):
		return clsVariable
	case t.InCategory(chroma.Keyword):
		return clsKeyword
	case t.InSubCategory(chroma.LiteralString):
		return clsString
	case t.InSubCategory(chroma.LiteralNumber):
		return clsNumber
	case t.InCategory(chroma.Literal):
		return clsConstant
	case t.InCategory(chroma.Operator):
		return clsOperator
	case t.InCategory(chroma.Punctuation):
		return clsPunctuation
	case t.InCategory(chroma.Comment):
		return clsComment
	}
	return clsPlain
}

// span is a run of source text with one highlight class.
type span struct {
	cls  syntaxClass
	text string
}

// Highlighter highlights code with chroma and maps the result onto theme
// syntax tokens. Lexing results are cached by (content hash, language), so
// colours always come from the palette passed to Highlight and a theme change
// never needs a cache flush. Safe for concurrent use.
type Highlighter struct {
	// MaxLines and MaxBytes disable highlighting above these sizes. Zero means
	// the defaults.
	MaxLines, MaxBytes int
	// MaxEntries bounds the cache (default 256 entries).
	MaxEntries int

	mu     sync.Mutex
	lexers map[string]chroma.Lexer
	cache  map[hlKey]*list.Element
	order  *list.List // front = most recent
	hits   int
	misses int
}

type hlKey struct {
	hash uint64
	n    int
	lang string
}

type hlEntry struct {
	key   hlKey
	lines [][]span
}

var hlSeed = maphash.MakeSeed()

// DefaultHighlighter is shared by the markdown renderer and diff views.
var DefaultHighlighter = &Highlighter{}

// LexerFor returns the chroma lexer name for a fence info string or a file
// name, or "" when none matches. A language wins over a file name.
func (h *Highlighter) LexerFor(lang, filename string) string {
	if l := h.lexer(lang, filename); l != nil {
		return l.Config().Name
	}
	return ""
}

func (h *Highlighter) lexer(lang, filename string) chroma.Lexer {
	lang = normLang(lang)
	key := lang
	if key == "" {
		if filename == "" {
			return nil
		}
		key = "file:" + strings.ToLower(filepath.Base(filename))
	}
	h.mu.Lock()
	l, ok := h.lexers[key]
	h.mu.Unlock()
	if ok {
		return l
	}
	switch {
	case lang != "":
		l = lexers.Get(lang)
		if l == nil {
			l = lexers.Match("x." + lang)
		}
	default:
		l = lexers.Match(filepath.Base(filename))
	}
	if l != nil {
		if name := strings.ToLower(l.Config().Name); name == "plaintext" || name == "text" {
			l = nil
		} else {
			l = chroma.Coalesce(l)
		}
	}
	h.mu.Lock()
	if h.lexers == nil {
		h.lexers = map[string]chroma.Lexer{}
	}
	h.lexers[key] = l
	h.mu.Unlock()
	return l
}

// normLang reduces a fence info string to a language name: the first word,
// lower-cased, without braces or a leading dot ("{.python}" → "python").
func normLang(info string) string {
	info = strings.TrimSpace(info)
	if i := strings.IndexAny(info, " \t,{"); i == 0 {
		info = strings.TrimLeft(info, "{. \t")
	}
	if i := strings.IndexAny(info, " \t,{}"); i >= 0 {
		info = info[:i]
	}
	info = strings.TrimPrefix(info, ".")
	return strings.ToLower(info)
}

// Highlight returns code as styled lines, one per source line (not wrapped,
// tabs kept). lang is a fence info string; filename is used when lang is
// empty. Unknown languages, oversized input and a disabled highlighter
// (h == nil) give unstyled lines in the plain syntax colour.
func (h *Highlighter) Highlight(code, lang, filename string, p Palette) []string {
	code = strings.TrimSuffix(code, "\n")
	spans := h.spans(code, lang, filename)
	if spans == nil {
		plain := Fg(p, TokSyntaxPlain)
		lines := strings.Split(code, "\n")
		for i, l := range lines {
			lines[i] = plain.Render(l)
		}
		return lines
	}
	var styles [numClasses]string
	var have [numClasses]bool
	out := make([]string, len(spans))
	var b strings.Builder
	for i, line := range spans {
		b.Reset()
		for _, sp := range line {
			if !have[sp.cls] {
				styles[sp.cls] = sp.cls.style(p).Open()
				have[sp.cls] = true
			}
			if open := styles[sp.cls]; open != "" {
				b.WriteString(open)
				b.WriteString(sp.text)
				b.WriteString("\x1b[m")
			} else {
				b.WriteString(sp.text)
			}
		}
		out[i] = b.String()
	}
	return out
}

// Segment is a run of text in one style.
type Segment struct {
	Text  string
	Style Style
}

// Segments highlights like Highlight but returns styled runs per line, so a
// caller can overlay backgrounds (diff word highlights) before rendering.
func (h *Highlighter) Segments(code, lang, filename string, p Palette) [][]Segment {
	code = strings.TrimSuffix(code, "\n")
	spans := h.spans(code, lang, filename)
	if spans == nil {
		plain := Fg(p, TokSyntaxPlain)
		lines := strings.Split(code, "\n")
		out := make([][]Segment, len(lines))
		for i, l := range lines {
			if l != "" {
				out[i] = []Segment{{Text: l, Style: plain}}
			}
		}
		return out
	}
	var styles [numClasses]Style
	var have [numClasses]bool
	out := make([][]Segment, len(spans))
	for i, line := range spans {
		segs := make([]Segment, len(line))
		for j, sp := range line {
			if !have[sp.cls] {
				styles[sp.cls] = sp.cls.style(p)
				have[sp.cls] = true
			}
			segs[j] = Segment{Text: sp.text, Style: styles[sp.cls]}
		}
		out[i] = segs
	}
	return out
}

// RenderSegments renders styled runs as one string.
func RenderSegments(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Style.Render(s.Text))
	}
	return b.String()
}

// spans lexes code into per-line spans, using the cache. It returns nil when
// the code should not be highlighted.
func (h *Highlighter) spans(code, lang, filename string) [][]span {
	if h == nil {
		return nil
	}
	maxLines, maxBytes := h.MaxLines, h.MaxBytes
	if maxLines <= 0 {
		maxLines = DefaultHighlightMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultHighlightMaxBytes
	}
	if len(code) > maxBytes || strings.Count(code, "\n") >= maxLines {
		return nil
	}
	lex := h.lexer(lang, filename)
	if lex == nil {
		return nil
	}
	key := hlKey{hash: maphash.String(hlSeed, code), n: len(code), lang: lex.Config().Name}
	h.mu.Lock()
	if el, ok := h.cache[key]; ok {
		h.order.MoveToFront(el)
		h.hits++
		lines := el.Value.(*hlEntry).lines
		h.mu.Unlock()
		return lines
	}
	h.misses++
	h.mu.Unlock()

	lines := lexLines(lex, code)
	if lines == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cache == nil {
		h.cache = map[hlKey]*list.Element{}
		h.order = list.New()
	}
	if _, ok := h.cache[key]; !ok {
		h.cache[key] = h.order.PushFront(&hlEntry{key: key, lines: lines})
		limit := h.MaxEntries
		if limit <= 0 {
			limit = 256
		}
		for h.order.Len() > limit {
			old := h.order.Back()
			h.order.Remove(old)
			delete(h.cache, old.Value.(*hlEntry).key)
		}
	}
	return lines
}

// Stats returns cache hits and misses (for tests and benchmarks).
func (h *Highlighter) Stats() (hits, misses int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hits, h.misses
}

func lexLines(lex chroma.Lexer, code string) [][]span {
	it, err := lex.Tokenise(nil, code)
	if err != nil {
		return nil
	}
	lines := [][]span{nil}
	add := func(cls syntaxClass, text string) {
		cur := &lines[len(lines)-1]
		if n := len(*cur); n > 0 && (*cur)[n-1].cls == cls {
			(*cur)[n-1].text += text
			return
		}
		*cur = append(*cur, span{cls, text})
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
		cls := classify(tok.Type)
		text := tok.Value
		for {
			i := strings.IndexByte(text, '\n')
			if i < 0 {
				break
			}
			if i > 0 {
				add(cls, text[:i])
			}
			lines = append(lines, nil)
			text = text[i+1:]
		}
		if text != "" {
			add(cls, text)
		}
	}
	// Lexers may add a trailing newline: keep exactly one entry per source line.
	want := strings.Count(code, "\n") + 1
	for len(lines) < want {
		lines = append(lines, nil)
	}
	return lines[:want]
}
