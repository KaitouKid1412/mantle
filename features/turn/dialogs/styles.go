package dialogs

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Styles are the looks the dialogs use. Part B fills them from pkg/theme tokens;
// PlainStyles (all zero) renders plain text for goldens.
type Styles struct {
	Border   lipgloss.Style // frame lines
	Title    lipgloss.Style // dialog title
	Text     lipgloss.Style
	Dim      lipgloss.Style // secondary text, hints
	Code     lipgloss.Style // commands, paths, JSON
	Selected lipgloss.Style // focused option
	Accent   lipgloss.Style // pointer, counters
	Error    lipgloss.Style
	Warning  lipgloss.Style
	DiffAdd  lipgloss.Style
	DiffDel  lipgloss.Style
	Cursor   lipgloss.Style // character under a text field's cursor
	// BorderColor names the frame's purpose; the host maps it to a theme token
	// ("permission", "planMode", "warning", "error").
	BorderColor func(kind string) lipgloss.Style
}

// PlainStyles renders without any escape sequences.
func PlainStyles() Styles { return Styles{} }

func (st Styles) border(kind string) lipgloss.Style {
	if st.BorderColor != nil {
		return st.BorderColor(kind)
	}
	return st.Border
}

// render applies s to text unless s is the zero style (keeps goldens free of escapes).
func render(s lipgloss.Style, text string) string {
	if text == "" {
		return ""
	}
	return s.Render(text)
}

// wrap word-wraps plain text to width cells, hard-breaking words that don't fit, and
// returns the lines. Empty input gives one empty line.
func wrap(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(para, width, ""), "\n")...)
	}
	for i, l := range out {
		out[i] = strings.TrimRight(l, " ")
	}
	return out
}

// wrapIndent wraps with a first-line prefix and a hanging indent of the prefix's width.
func wrapIndent(prefix, s string, width int) []string {
	pw := ansi.StringWidth(prefix)
	lines := wrap(s, width-pw)
	pad := strings.Repeat(" ", pw)
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else if lines[i] != "" {
			lines[i] = pad + lines[i]
		}
	}
	return lines
}

// styleLines applies s to each line.
func styleLines(s lipgloss.Style, lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = render(s, l)
	}
	return out
}

// flush marks a body line that frame must not indent (rules that span the full width,
// unindented question layouts).
const flush = "\x01"

// frame draws an inline dialog the way Claude Code does: a full-width rule in the
// dialog's colour, the title, then the body indented by one column. No side or bottom
// border, so the dialog reads as part of the prompt area. Body lines starting with
// flush are written without the indent. Every line fits width.
func frame(title string, body []string, width int, kind string, st Styles) string {
	if width < 8 {
		width = 8
	}
	var b strings.Builder
	b.WriteString(render(st.border(kind), strings.Repeat("─", width)))
	if title != "" {
		ts := st.Title
		if st.BorderColor != nil {
			ts = st.BorderColor(kind).Bold(true)
		}
		b.WriteString("\n ")
		b.WriteString(render(ts, ansi.Truncate(title, width-1, "…")))
	}
	for _, l := range body {
		line := " " + l
		if raw, ok := strings.CutPrefix(l, flush); ok {
			line = raw
		} else if l == "" {
			line = ""
		}
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "…")
		}
		b.WriteString("\n")
		b.WriteString(line)
	}
	return b.String()
}

// bodyWidth is the usable text width inside a frame of the given width.
func bodyWidth(width int) int { return max(4, max(8, width)-2) }

// dashRule is a full-width dashed separator inside a frame (around commands and
// diffs).
func dashRule(width int, st Styles) string {
	return flush + render(st.Dim, strings.Repeat("╌", max(8, width)))
}

// solidRule is a full-width solid separator inside a frame.
func solidRule(width int, st Styles) string {
	return flush + render(st.Dim, strings.Repeat("─", max(8, width)))
}

// truncateLines keeps at most n lines, replacing the rest with a dim "… +k lines".
func truncateLines(lines []string, n int, st Styles) []string {
	if n <= 0 || len(lines) <= n {
		return lines
	}
	more := len(lines) - (n - 1)
	out := append([]string(nil), lines[:n-1]...)
	return append(out, render(st.Dim, "… +"+itoa(more)+" lines"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
