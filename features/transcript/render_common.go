package transcript

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/render"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Glyphs of the transcript look.
const (
	glyphDot     = "⏺"                            // assistant text and tool calls
	glyphResult  = "⎿"                            // tool results
	glyphThought = "✻"                            // thinking
	resultIndent = "  " + glyphResult + " \u00a0" // as claude writes it
	glyphPrompt  = "❯"                            // user prompts
	resultHang   = "     "
	dotIndent    = "  "
)

// collapsedLines is how many lines of tool output show before "+N lines".
const collapsedLines = 3

// paletteOf adapts a theme to the render kit's palette.
func paletteOf(th *theme.Theme) render.Palette {
	if th == nil {
		return render.NoColor
	}
	return render.PaletteFunc(func(tok string) color.Color { return th.Color(theme.Token(tok)) })
}

// sty bundles the styles renderers share for one RenderCtx.
type sty struct {
	p                       render.Palette
	text, dim, subtle, bold render.Style
	ok, err, warn, accent   render.Style
	perm, plan              render.Style
}

func stylesFor(rc ext.RenderCtx) sty {
	p := paletteOf(rc.Theme)
	return sty{
		p:      p,
		text:   render.Fg(p, render.TokText),
		dim:    render.Fg(p, render.TokInactive),
		subtle: render.Fg(p, render.TokSubtle),
		bold:   render.Style{Bold: true},
		ok:     render.Fg(p, render.TokSuccess),
		err:    render.Fg(p, render.TokError),
		warn:   render.Fg(p, render.TokWarning),
		accent: render.Fg(p, render.TokAccent),
		perm:   render.Fg(p, render.TokPermission),
		plan:   render.Fg(p, render.TokPlanMode),
	}
}

func verbose(rc ext.RenderCtx) bool {
	return rc.Expanded || rc.Mode == ext.Verbose || rc.Mode == ext.FullTranscript
}

// clean neutralises untrusted text for display: no escape sequences or control
// characters, tabs expanded.
func clean(s string) string {
	return render.ExpandTabs(render.Sanitize(s, render.SanitizeOptions{}), 4)
}

// oneLine collapses text to a single line.
func oneLine(s string) string {
	s = clean(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

// bulletStyle colours a tool call's dot by state.
func bulletStyle(st sty, state ext.ItemState) render.Style {
	switch state {
	case ext.Done:
		return st.ok
	case ext.Failed:
		return st.err
	case ext.Interrupted:
		return st.dim
	}
	return st.dim
}

// header renders "⏺ Name(args)", wrapping the args under a hanging indent.
func header(rc ext.RenderCtx, st sty, dot render.Style, name, args string) []string {
	head := st.bold.Render(clean(name))
	if args != "" {
		head += "(" + applyLinks(clean(args)) + ")"
	}
	return render.WrapWith(head, render.WrapOptions{Width: rc.Width, First: dot.Render(glyphDot) + " ", Rest: dotIndent})
}

// Link markers: fileLink wraps a path in private-use runes that header turns
// into an OSC 8 hyperlink after the args are sanitized.
const (
	linkOpen  = "\uE000"
	linkMid   = "\uE001"
	linkClose = "\uE002"
)

// fileLink marks a displayed path as a link to the absolute file.
func fileLink(display, abs string) string {
	display = stripLinkMarks(display)
	if abs == "" || !filepath.IsAbs(abs) {
		return display
	}
	u := url.URL{Scheme: "file", Path: abs}
	return linkOpen + u.String() + linkMid + display + linkClose
}

func stripLinkMarks(s string) string {
	return strings.NewReplacer(linkOpen, "", linkMid, "", linkClose, "").Replace(s)
}

// applyLinks turns link markers into OSC 8 hyperlinks. Only file:// targets are
// honoured (args come from untrusted tool input).
func applyLinks(s string) string {
	for {
		i := strings.Index(s, linkOpen)
		if i < 0 {
			return s
		}
		rest := s[i+len(linkOpen):]
		j := strings.Index(rest, linkMid)
		k := strings.Index(rest, linkClose)
		if j < 0 || k < j {
			return stripLinkMarks(s)
		}
		target, text := rest[:j], rest[j+len(linkMid):k]
		link := text
		if strings.HasPrefix(target, "file://") {
			link = render.Link(target, text)
		}
		s = s[:i] + link + rest[k+len(linkClose):]
	}
}

// result renders lines under "  ⎿  ", wrapped.
func result(rc ext.RenderCtx, style render.Style, text string) []string {
	if text == "" {
		return nil
	}
	return render.WrapWith(style.Render(text), render.WrapOptions{Width: rc.Width, First: resultIndent, Rest: resultHang})
}

// indentLines prefixes pre-rendered lines with the result gutter (first line ⎿).
func indentLines(lines []string, first bool) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case i == 0 && first:
			out[i] = resultIndent + l
		case l == "":
			out[i] = ""
		default:
			out[i] = resultHang + l
		}
	}
	return out
}

// output renders tool output text under the result gutter, collapsed to a few
// lines unless the view is verbose. It reports whether lines were hidden.
func output(rc ext.RenderCtx, style render.Style, text string, first bool) ([]string, bool) {
	text = strings.TrimRight(clean(text), "\n")
	if text == "" {
		return nil, false
	}
	width := max(rc.Width-len(resultHang), 1)
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		lines = append(lines, render.WrapWith(style.Render(l), render.WrapOptions{Width: width, Hard: true})...)
	}
	hidden := 0
	if !verbose(rc) && len(lines) > collapsedLines {
		hidden = len(lines) - collapsedLines
		lines = lines[:collapsedLines]
	}
	out := indentLines(lines, first)
	if hidden > 0 {
		out = append(out, resultHang+moreLines(rc, hidden))
	}
	return out, hidden > 0
}

// moreLines is the "… +N lines (ctrl+o to expand)" marker.
func moreLines(rc ext.RenderCtx, n int) string {
	st := stylesFor(rc)
	unit := "lines"
	if n == 1 {
		unit = "line"
	}
	return st.dim.Render(fmt.Sprintf("… +%d %s (ctrl+o to expand)", n, unit))
}

// truncLines cuts every line to the width (for one-line summaries).
func truncLines(lines []string, width int) []string {
	for i, l := range lines {
		lines[i] = render.Truncate(l, width, "…")
	}
	return lines
}

// relPath shortens a path relative to the working directory.
func relPath(cwd, p string) string {
	if p == "" || cwd == "" || !filepath.IsAbs(p) {
		return p
	}
	if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// decodeInput decodes a tool's JSON input into v, ignoring errors (partial or
// unexpected input renders with what decoded).
func decodeInput(raw json.RawMessage, v any) {
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, v)
	}
}

// argSummary renders a JSON object as `key: value, key: value` (MCP and
// unknown tools), each value shortened.
func argSummary(raw json.RawMessage, maxLen int) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := oneLine(string(m[k]))
		if len(v) > 60 {
			v = v[:57] + "…"
		}
		parts = append(parts, k+": "+v)
	}
	s := strings.Join(parts, ", ")
	if maxLen > 0 && render.Width(s) > maxLen {
		s = render.Truncate(s, maxLen, "…")
	}
	return s
}

// plural formats "1 line" / "3 lines".
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// formatDuration renders a duration the way the transcript shows it: "850ms",
// "12s", "1m 6s", "2h 3m".
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// formatTokens renders a token count compactly: 950, 12.3k, 1.2M.
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
}

// formatBytes renders a byte count: 512B, 12.3KB, 1.2MB.
func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1024), ".0") + "KB"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/(1024*1024)), ".0") + "MB"
}
