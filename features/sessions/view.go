package sessions

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// wrapLines word-wraps styled lines to width (0 = no wrapping) and joins them.
func wrapLines(lines []string, width int) string {
	if width <= 0 {
		return strings.Join(lines, "\n")
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, ansi.Wrap(l, width, ""))
	}
	return strings.Join(out, "\n")
}

// fit cuts a styled line to width cells, with an ellipsis when it cut.
func fit(s string, width int) string {
	if width <= 0 || ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// pad right-pads a styled string with spaces to width cells.
func pad(s string, width int) string {
	if w := ansi.StringWidth(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// visibleWidth is the width of a styled string in cells.
func visibleWidth(s string) int { return ansi.StringWidth(s) }

// oneLine collapses whitespace runs (newlines included) to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// relTime is a compact age: "just now", "5m ago", "3h ago", "2d ago", or a date.
func relTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case t.Year() == now.Year():
		return t.Local().Format("Jan 2")
	}
	return t.Local().Format("Jan 2, 2006")
}

// dateGroup is the picker's section for a time: Today, Yesterday, This week, This
// month or Older (local dates).
func dateGroup(now, t time.Time) string {
	y1, m1, d1 := now.Local().Date()
	y2, m2, d2 := t.Local().Date()
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	days := int(day(y1, m1, d1).Sub(day(y2, m2, d2)).Hours() / 24)
	switch {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days < 7:
		return "This week"
	case days < 30:
		return "This month"
	}
	return "Older"
}

// shortPath replaces the home directory prefix with "~".
func shortPath(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}
