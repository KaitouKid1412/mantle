package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Frame draws body in a rounded box of exactly width cells, with an optional title in
// the top border, coloured with the border token (theme.Permission for permission
// prompts, theme.PlanMode for plan approval, …). Body lines are clipped to fit.
//
//	╭─ Title ───────────╮
//	│ body               │
//	╰────────────────────╯
func Frame(t *theme.Theme, border theme.Token, title, body string, width int) string {
	if width < 4 {
		width = 4
	}
	bc := t.Fg(border)
	inner := width - 4 // "│ " + content + " │"
	var b strings.Builder
	if title != "" {
		label := " " + ansi.Truncate(title, max(0, width-5), "…") + " "
		lw := ansi.StringWidth(label)
		b.WriteString(bc.Render("╭─"))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(t.Color(border)).Render(label))
		b.WriteString(bc.Render(strings.Repeat("─", max(0, width-3-lw)) + "╮"))
	} else {
		b.WriteString(bc.Render("╭" + strings.Repeat("─", width-2) + "╮"))
	}
	for _, line := range strings.Split(body, "\n") {
		line = ansi.Truncate(line, inner, "")
		pad := inner - ansi.StringWidth(line)
		b.WriteString("\n" + bc.Render("│") + " " + line + strings.Repeat(" ", pad) + " " + bc.Render("│"))
	}
	b.WriteString("\n" + bc.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return b.String()
}

// FrameInner is the content width inside a Frame of width.
func FrameInner(width int) int { return max(0, width-4) }

// Hint is one key hint: "enter" "to confirm".
type Hint struct{ Keys, Label string }

// Hints renders a key-hint bar: "enter to confirm · esc to cancel", dim, clipped to
// width. Hints with empty Keys are skipped.
func Hints(t *theme.Theme, width int, hs ...Hint) string {
	var parts []string
	for _, h := range hs {
		if h.Keys == "" {
			continue
		}
		parts = append(parts, h.Keys+" "+h.Label)
	}
	return t.Paint(theme.Inactive, ansi.Truncate(strings.Join(parts, " · "), width, "…"))
}

// Pad right-pads s with spaces to width cells (clipping when wider).
func Pad(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	return s + strings.Repeat(" ", width-w)
}
