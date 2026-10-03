package theme

import (
	"image/color"
	"maps"
	"slices"

	"charm.land/lipgloss/v2"
)

// Theme maps tokens to colours. Themes are values: copy one with Clone or With before
// changing it. The host owns the active theme and hands out a pointer through
// ext.Ctx.Theme(); treat that pointer as read-only.
type Theme struct {
	// Name is the settings value that selects this theme: "dark", "light",
	// "dark-daltonized", "light-daltonized", "dark-ansi", "light-ansi", or
	// "custom:<name>" for a theme loaded from ~/.claude/themes.
	Name string
	// Dark is true for themes meant for dark terminal backgrounds.
	Dark bool
	// ANSI is true when the palette uses only the 16 terminal colours, so it follows
	// the user's terminal colour scheme.
	ANSI bool
	// Base names the theme this one extends (custom themes); empty for built-ins.
	Base string
	// Colors holds every token's colour. Custom themes may carry keys mantle does not
	// define as constants; they are kept so components can look them up by name.
	Colors map[Token]color.Color
}

// Color returns the colour for a token. Missing tokens fall back to Text, then to nil
// (the terminal's default foreground).
func (t *Theme) Color(tok Token) color.Color {
	if t == nil {
		return nil
	}
	if c, ok := t.Colors[tok]; ok {
		return c
	}
	return t.Colors[Text]
}

// Has reports whether the theme defines a token itself (no fallback).
func (t *Theme) Has(tok Token) bool {
	if t == nil {
		return false
	}
	_, ok := t.Colors[tok]
	return ok
}

// Fg returns a style with the token as foreground colour.
func (t *Theme) Fg(tok Token) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Color(tok))
}

// Bg returns a style with the token as background colour.
func (t *Theme) Bg(tok Token) lipgloss.Style {
	return lipgloss.NewStyle().Background(t.Color(tok))
}

// Paint renders s in the token's foreground colour.
func (t *Theme) Paint(tok Token, s string) string {
	return t.Fg(tok).Render(s)
}

// Clone returns a deep copy.
func (t Theme) Clone() Theme {
	t.Colors = maps.Clone(t.Colors)
	if t.Colors == nil {
		t.Colors = map[Token]color.Color{}
	}
	return t
}

// With returns a copy with the given tokens overridden.
func (t Theme) With(overrides map[Token]color.Color) Theme {
	c := t.Clone()
	maps.Copy(c.Colors, overrides)
	return c
}

// Missing returns the defined tokens (Tokens) this theme does not set, sorted.
func (t *Theme) Missing() []Token {
	var out []Token
	for _, tok := range Tokens {
		if !t.Has(tok) {
			out = append(out, tok)
		}
	}
	slices.Sort(out)
	return out
}
