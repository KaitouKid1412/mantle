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

// frame draws a box of exactly width cells around body lines (already wrapped to
// width-4), with the title in the top edge: ╭─ Title ───╮.
func frame(title string, body []string, width int, kind string, st Styles) string {
	if width < 8 {
		width = 8
	}
	bs := st.border(kind)
	inner := width - 2
	top := render(bs, "╭"+strings.Repeat("─", inner)+"╮")
	if title != "" {
		t := ansi.Truncate(" "+title+" ", inner-1, "… ")
		top = render(bs, "╭─") + render(st.Title, t) +
			render(bs, strings.Repeat("─", max(0, inner-1-ansi.StringWidth(t)))+"╮")
	}
	var b strings.Builder
	b.WriteString(top)
	for _, l := range body {
		w := ansi.StringWidth(l)
		if w > inner-2 {
			l = ansi.Truncate(l, inner-2, "…")
			w = ansi.StringWidth(l)
		}
		b.WriteString("\n")
		b.WriteString(render(bs, "│"))
		b.WriteString(" ")
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", inner-2-w))
		b.WriteString(" ")
		b.WriteString(render(bs, "│"))
	}
	b.WriteString("\n")
	b.WriteString(render(bs, "╰"+strings.Repeat("─", inner)+"╯"))
	return b.String()
}

// bodyWidth is the usable text width inside a frame of the given width.
func bodyWidth(width int) int { return max(4, max(8, width)-4) }

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
