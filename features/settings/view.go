package settings

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// These helpers draw the settings panels until plan 01's pkg/ui widgets land; panels
// call only frame, listLines and hintLine, so the swap is local.

const (
	cursorMark  = "❯"
	currentMark = "✔"
)

// frame draws a titled panel: a rounded border in the suggestion colour, the title in
// bold, an optional dim subtitle, the body and a dim hint line.
func frame(t *theme.Theme, width int, title, subtitle string, body []string, hint string) string {
	if width < 8 {
		width = 8
	}
	inner := width - 4 // border and one column of padding each side
	var lines []string
	lines = append(lines, fit(lipgloss.NewStyle().Bold(true).Render(title), inner))
	if subtitle != "" {
		for _, l := range wrap(subtitle, inner) {
			lines = append(lines, t.Paint(theme.Inactive, l))
		}
	}
	lines = append(lines, "")
	for _, l := range body {
		lines = append(lines, fit(l, inner))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Color(theme.Suggestion)).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines, "\n"))
	if hint == "" {
		return box
	}
	return box + "\n" + fit(" "+t.Paint(theme.Inactive, hint), width)
}

// fit truncates a styled line to width cells.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// wrap soft-wraps plain text to width.
func wrap(s string, width int) []string {
	if width <= 0 {
		return nil
	}
	return strings.Split(ansi.Wordwrap(s, width, ""), "\n")
}

// listRow is one row of a picker list.
type listRow struct {
	Label    string
	Detail   string // dim text after the label
	Right    string // already styled, appended after the detail
	Current  bool   // shows the current-value mark
	Disabled bool
}

// listLines renders rows with the cursor, numbering and a scroll window of at most
// maxRows rows (0 = all). The label column is as wide as the widest label.
func listLines(t *theme.Theme, width int, rows []listRow, cursor, maxRows int) []string {
	if len(rows) == 0 {
		return nil
	}
	start, end := window(len(rows), cursor, maxRows)
	labelW := 0
	numW := len(fmt.Sprint(len(rows)))
	for _, r := range rows {
		if w := ansi.StringWidth(r.Label); w > labelW {
			labelW = w
		}
	}
	if max := width / 2; labelW > max {
		labelW = max
	}
	var out []string
	if start > 0 {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		r := rows[i]
		mark := "  "
		if i == cursor {
			mark = t.Paint(theme.Suggestion, cursorMark) + " "
		}
		num := fmt.Sprintf("%*d. ", numW, i+1)
		label := fit(r.Label, labelW)
		label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
		switch {
		case r.Disabled:
			label = t.Paint(theme.Subtle, label)
			num = t.Paint(theme.Subtle, num)
		case i == cursor:
			label = t.Paint(theme.Suggestion, label)
		}
		line := mark + num + label
		if r.Current {
			line += " " + t.Paint(theme.Success, currentMark)
		} else {
			line += "  "
		}
		if r.Detail != "" {
			line += "  " + t.Paint(theme.Inactive, r.Detail)
		}
		if r.Right != "" {
			line += "  " + r.Right
		}
		out = append(out, fit(line, width))
	}
	if end < len(rows) {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("  ↓ %d more", len(rows)-end)))
	}
	return out
}

// window picks the visible slice of n rows around cursor.
func window(n, cursor, maxRows int) (start, end int) {
	if maxRows <= 0 || n <= maxRows {
		return 0, n
	}
	start = cursor - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > n {
		start = n - maxRows
	}
	return start, start + maxRows
}

// hintLine joins "key action" pairs: hintLine("enter", "select", "esc", "cancel").
func hintLine(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i] == "" {
			continue
		}
		parts = append(parts, pairs[i]+" to "+pairs[i+1])
	}
	return strings.Join(parts, " · ")
}

// keyName is the first key bound to an action in a context, or def.
func keyName(keys []string, def string) string {
	k := def
	if len(keys) > 0 {
		k = keys[0]
	}
	if k == "escape" {
		return "esc"
	}
	return k
}

// listHeight is how many list rows fit a dialog: the area's height (or the terminal's)
// minus the frame, title, hints and a little air, never fewer than 3.
func listHeight(areaMax, termH, chrome int) int {
	h := areaMax
	if h <= 0 {
		h = termH - 1
	}
	h -= chrome
	if h < 3 {
		h = 3
	}
	return h
}
