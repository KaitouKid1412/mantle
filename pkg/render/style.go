package render

import (
	"image/color"

	"github.com/charmbracelet/x/ansi"
)

// Palette resolves theme token names to colours. A nil colour means "terminal
// default". Token names are pkg/theme's (see the Tok* constants); an adapter over
// *theme.Theme satisfies this interface, and PaletteFunc wraps any lookup.
type Palette interface {
	Color(token string) color.Color
}

// PaletteFunc adapts a function to Palette.
type PaletteFunc func(token string) color.Color

// Color implements Palette.
func (f PaletteFunc) Color(token string) color.Color { return f(token) }

// MapPalette is a fixed token → colour table, used by tests and stories.
type MapPalette map[string]color.Color

// Color implements Palette.
func (m MapPalette) Color(token string) color.Color { return m[token] }

// NoColor is a palette that resolves every token to the terminal default. Text
// attributes (bold, italic, …) still apply.
var NoColor Palette = MapPalette(nil)

// Theme token names used by the render kit. They are the strings pkg/theme
// defines, mirrored here so pkg/render has no compile-time dependency on the
// theme package.
const (
	TokText       = "text"
	TokInactive   = "inactive"
	TokSubtle     = "subtle"
	TokAccent     = "claude"
	TokPermission = "permission"
	TokSuggestion = "suggestion"
	TokSuccess    = "success"
	TokError      = "error"
	TokWarning    = "warning"
	TokPlanMode   = "planMode"

	TokDiffAdded         = "diffAdded"
	TokDiffRemoved       = "diffRemoved"
	TokDiffAddedDimmed   = "diffAddedDimmed"
	TokDiffRemovedDimmed = "diffRemovedDimmed"
	TokDiffAddedWord     = "diffAddedWord"
	TokDiffRemovedWord   = "diffRemovedWord"

	TokSyntaxKeyword     = "syntax.keyword"
	TokSyntaxString      = "syntax.string"
	TokSyntaxNumber      = "syntax.number"
	TokSyntaxComment     = "syntax.comment"
	TokSyntaxFunction    = "syntax.function"
	TokSyntaxType        = "syntax.type"
	TokSyntaxVariable    = "syntax.variable"
	TokSyntaxConstant    = "syntax.constant"
	TokSyntaxOperator    = "syntax.operator"
	TokSyntaxPunctuation = "syntax.punctuation"
	TokSyntaxTag         = "syntax.tag"
	TokSyntaxAttribute   = "syntax.attribute"
	TokSyntaxBuiltin     = "syntax.builtin"
	TokSyntaxPlain       = "syntax.plain"
)

// Style is a set of SGR attributes. The zero Style renders text unchanged.
type Style struct {
	Fg, Bg    color.Color
	Bold      bool
	Faint     bool
	Italic    bool
	Underline bool
	Strike    bool
	Reverse   bool
}

// Fg returns a Style with only a foreground colour resolved from a token.
func Fg(p Palette, token string) Style {
	if p == nil {
		return Style{}
	}
	return Style{Fg: p.Color(token)}
}

// IsZero reports whether the style changes nothing.
func (s Style) IsZero() bool {
	return s.Fg == nil && s.Bg == nil && !s.Bold && !s.Faint && !s.Italic &&
		!s.Underline && !s.Strike && !s.Reverse
}

// Merge returns s overlaid with o: o's colours win when set, attributes add up.
func (s Style) Merge(o Style) Style {
	if o.Fg != nil {
		s.Fg = o.Fg
	}
	if o.Bg != nil {
		s.Bg = o.Bg
	}
	s.Bold = s.Bold || o.Bold
	s.Faint = s.Faint || o.Faint
	s.Italic = s.Italic || o.Italic
	s.Underline = s.Underline || o.Underline
	s.Strike = s.Strike || o.Strike
	s.Reverse = s.Reverse || o.Reverse
	return s
}

// Open returns the SGR sequence that turns the style on, or "" for the zero style.
func (s Style) Open() string {
	if s.IsZero() {
		return ""
	}
	a := ansi.Style{}
	if s.Bold {
		a = a.Bold()
	}
	if s.Faint {
		a = a.Faint()
	}
	if s.Italic {
		a = a.Italic(true)
	}
	if s.Underline {
		a = a.Underline(true)
	}
	if s.Strike {
		a = a.Strikethrough(true)
	}
	if s.Reverse {
		a = a.Reverse(true)
	}
	if s.Fg != nil {
		a = a.ForegroundColor(s.Fg)
	}
	if s.Bg != nil {
		a = a.BackgroundColor(s.Bg)
	}
	return a.String()
}

// Render wraps text in the style: open sequence, text, reset. Each call is
// self-contained, so rendered runs can be concatenated freely.
func (s Style) Render(text string) string {
	if text == "" {
		return ""
	}
	open := s.Open()
	if open == "" {
		return text
	}
	return open + text + ansi.ResetStyle
}

// Link wraps already-styled text in an OSC 8 hyperlink.
func Link(url, text string) string {
	if url == "" || text == "" {
		return text
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}
