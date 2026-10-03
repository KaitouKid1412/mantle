package dialogs

import (
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// diffLine is one line of a preview diff.
type diffLine struct {
	kind byte // '+', '-', ' ', or '~' for a hunk separator
	text string
}

// diffPreview computes a line diff of old → new with a little context. It is the
// fallback until plan 03's pkg/ui/diffview is wired in.
func diffPreview(old, new string, context int) []diffLine {
	if old == new {
		return nil
	}
	if old != "" && !strings.HasSuffix(old, "\n") {
		old += "\n"
	}
	if new != "" && !strings.HasSuffix(new, "\n") {
		new += "\n"
	}
	edits := udiff.Strings(old, new)
	u, err := udiff.ToUnifiedDiff("a", "b", old, edits, context)
	if err != nil {
		return plainDiff(old, new)
	}
	var out []diffLine
	for i, h := range u.Hunks {
		if i > 0 {
			out = append(out, diffLine{kind: '~'})
		}
		for _, l := range h.Lines {
			text := strings.TrimSuffix(l.Content, "\n")
			switch l.Kind {
			case udiff.Delete:
				out = append(out, diffLine{'-', text})
			case udiff.Insert:
				out = append(out, diffLine{'+', text})
			default:
				out = append(out, diffLine{' ', text})
			}
		}
	}
	return out
}

func plainDiff(old, new string) []diffLine {
	var out []diffLine
	for _, l := range strings.Split(strings.TrimSuffix(old, "\n"), "\n") {
		out = append(out, diffLine{'-', l})
	}
	for _, l := range strings.Split(strings.TrimSuffix(new, "\n"), "\n") {
		out = append(out, diffLine{'+', l})
	}
	return out
}

// addedPreview shows new content (a Write) as all-added lines.
func addedPreview(content string) []diffLine {
	var out []diffLine
	for _, l := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		out = append(out, diffLine{'+', l})
	}
	return out
}

// renderDiff draws diff lines at width, truncated to maxLines.
func renderDiff(lines []diffLine, width, maxLines int, st Styles) []string {
	var out []string
	for _, d := range lines {
		if d.kind == '~' {
			out = append(out, render(st.Dim, "  ⋯"))
			continue
		}
		prefix := string(d.kind) + " "
		style := st.Code
		switch d.kind {
		case '+':
			style = st.DiffAdd
		case '-':
			style = st.DiffDel
		}
		for _, l := range wrapIndent(prefix, Sanitize(d.text), width) {
			out = append(out, render(style, l))
		}
	}
	return truncateLines(out, maxLines, st)
}
