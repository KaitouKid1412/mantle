package transcript

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// promptCollapse is how many lines of a long prompt show in the normal view.
const promptCollapse = 12

var (
	reCommandName = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	reCommandArgs = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
	reTag         = func(tag string) *regexp.Regexp {
		return regexp.MustCompile(`(?s)<` + tag + `>(.*?)</` + tag + `>`)
	}
	reBashInput  = reTag("bash-input")
	reBashStdout = reTag("bash-stdout")
	reBashStderr = reTag("bash-stderr")
	reAnyTag     = regexp.MustCompile(`(?s)<(command-message|command-name|command-args|system-reminder)>.*?</(command-message|command-name|command-args|system-reminder)>`)
)

// promptParts extracts the visible text and image count of a user prompt item,
// whatever shape its Data has.
func promptParts(data any) (text string, images int) {
	var c proto.Content
	switch d := data.(type) {
	case *proto.User:
		c = d.Message.Content
	case *proto.ContentBlock:
		if d.Type == proto.BlockImage {
			return "", 1
		}
		return d.Text, 0
	case string:
		return d, 0
	case proto.Content:
		c = d
	default:
		return "", 0
	}
	var parts []string
	if c.Blocks == nil {
		parts = append(parts, c.Text)
	}
	for _, b := range c.Blocks {
		switch b.Type {
		case proto.BlockText:
			parts = append(parts, b.Text)
		case proto.BlockImage:
			images++
		}
	}
	return strings.Join(parts, "\n"), images
}

// displayPrompt turns slash-command markup into "/name args".
func displayPrompt(text string) string {
	if m := reCommandName.FindStringSubmatch(text); m != nil {
		name := strings.TrimSpace(m[1])
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		if a := reCommandArgs.FindStringSubmatch(text); a != nil && strings.TrimSpace(a[1]) != "" {
			name += " " + strings.TrimSpace(a[1])
		}
		return name
	}
	return strings.TrimSpace(reAnyTag.ReplaceAllString(text, ""))
}

func (f *Feature) renderPrompt(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	text, images := promptParts(it.Data)
	text = clean(displayPrompt(text))
	var chips []string
	for i := 1; i <= images; i++ {
		chips = append(chips, fmt.Sprintf("[Image\u00a0#%d]", i)) // no break inside a chip
	}
	if len(chips) > 0 {
		if text != "" {
			text += " "
		}
		text += st.perm.Render(strings.Join(chips, " "))
	}
	bg := render.Style{Bg: st.p.Color(string(tokUserBg))}
	lines := render.WrapWith(text, render.WrapOptions{Width: rc.Width, First: st.dim.Render(glyphPrompt) + " ", Rest: "  "})
	if f.cfg.showTimestamps {
		if ts := f.clockTime(it.Start); ts != "" {
			if gap := rc.Width - render.Width(lines[0]) - render.Width(ts); gap >= 2 {
				lines[0] += strings.Repeat(" ", gap) + st.dim.Render(ts)
			}
		}
	}
	collapsible := false
	if !verbose(rc) && len(lines) > promptCollapse {
		hidden := len(lines) - promptCollapse
		lines = append(lines[:promptCollapse], "  "+moreLines(rc, hidden))
		collapsible = true
	}
	return ext.Block{Lines: fillBg(lines, rc.Width, bg), Collapsible: collapsible}
}

// tokUserBg is the user message background token.
const tokUserBg = "userMessageBackground"

// fillBg paints a background behind whole lines (padded to width).
func fillBg(lines []string, width int, bg render.Style) []string {
	if bg.Bg == nil {
		return lines
	}
	open := bg.Open()
	for i, l := range lines {
		pad := max(width-render.Width(l), 0)
		// Re-apply the background after every reset inside the line.
		l = strings.ReplaceAll(l, "\x1b[m", "\x1b[m"+open)
		lines[i] = open + l + strings.Repeat(" ", pad) + "\x1b[m"
	}
	return lines
}

func (f *Feature) renderBash(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	text, _ := promptParts(it.Data)
	cmd := text
	if m := reBashInput.FindStringSubmatch(text); m != nil {
		cmd = m[1]
	}
	bang := render.Fg(st.p, "bashBorder").Render("!")
	lines := render.WrapWith(clean(strings.TrimSpace(cmd)), render.WrapOptions{Width: rc.Width, First: bang + " ", Rest: "  "})
	stdout, stderr := "", ""
	if m := reBashStdout.FindStringSubmatch(text); m != nil {
		stdout = m[1]
	}
	if m := reBashStderr.FindStringSubmatch(text); m != nil {
		stderr = m[1]
	}
	out, hid1 := output(rc, render.Style{}, stdout, true)
	lines = append(lines, out...)
	errOut, hid2 := output(rc, st.err, stderr, len(out) == 0)
	lines = append(lines, errOut...)
	switch {
	case len(out) > 0 || len(errOut) > 0:
	case stdout != "" || stderr != "" || strings.Contains(text, "<bash-stdout>"):
		lines = append(lines, result(rc, st.dim, "(no output)")...)
	case !it.State.Finished():
		lines = append(lines, result(rc, st.dim, "Running…")...) // an echo whose command still runs
	}
	return ext.Block{Lines: lines, Collapsible: hid1 || hid2}
}

// textEntry caches the streaming markdown renderer of one text item.
type textEntry struct {
	s   *render.Stream
	raw string // engine text appended so far
	key mdKey
}

// mdKey identifies the options a cached stream was rendered with (options hold
// an interface palette, so they are not compared directly).
type mdKey struct {
	width, prose      int
	noHighlight, link bool
	theme             any
}

func (f *Feature) mdOptions(rc ext.RenderCtx, width int) render.MarkdownOptions {
	prose := f.cfg.maxProse
	if prose > 0 {
		prose = max(prose-len(dotIndent), 1)
	}
	return render.MarkdownOptions{
		Width:          max(width, 1),
		MaxProseWidth:  prose,
		Palette:        paletteOf(rc.Theme),
		NoHighlight:    f.cfg.noHighlight,
		NoHyperlinks:   f.cfg.noLinks,
		KeepLineBreaks: true, // a single newline is a line break, as in claude
		// Code keeps a terminal's 8-column tab stops, past the 2-column margin.
		TabWidth:  8,
		TabColumn: len(dotIndent),
	}
}

// textStream returns the up-to-date markdown stream of a text-like item.
func (f *Feature) textStream(rc ext.RenderCtx, id, text string, done bool) *render.Stream {
	o := f.mdOptions(rc, rc.Width-len(dotIndent))
	key := mdKey{o.Width, o.MaxProseWidth, o.NoHighlight, o.NoHyperlinks, rc.Theme}
	e := f.texts[id]
	if e == nil || !strings.HasPrefix(text, e.raw) || (e.s.Done() && text != e.raw) {
		e = &textEntry{s: render.NewStream(o), key: key}
		f.texts[id] = e
	}
	if key != e.key {
		e.s.SetOptions(o)
		e.key = key
	}
	if len(text) > len(e.raw) {
		e.s.Append(text[len(e.raw):])
		e.raw = text
	}
	if done {
		e.s.Finish()
	}
	return e.s
}

// forget drops cached render state of an item (after it is committed).
func (f *Feature) forget(id string) { delete(f.texts, id) }

// textBlocks renders a text item as prefixed markdown blocks: the dot on the
// first line of the first non-empty block, a hanging indent elsewhere, and a
// blank line before every non-empty block after the first. Concatenated, the
// blocks are the item's rendering. nclosed counts the leading blocks that will
// not change any more.
func (f *Feature) textBlocks(rc ext.RenderCtx, it *ext.Item) (blocks [][]string, nclosed int) {
	text := ""
	if b, ok := it.Data.(*proto.ContentBlock); ok {
		text = b.Text
	}
	s := f.textStream(rc, it.ID, text, it.State.Finished())
	closed := s.ClosedBlocks()
	return prefixBlocks(rc, append(closed, s.OpenBlocks()...)), len(closed)
}

// prefixBlocks puts the dot on the first line of the first non-empty block, a
// hanging indent on every other line, and a blank line between blocks.
func prefixBlocks(rc ext.RenderCtx, all [][]string) [][]string {
	dot := stylesFor(rc).text.Render(glyphDot) + " "
	started := false
	blocks := make([][]string, len(all))
	for i, b := range all {
		if len(b) == 0 {
			continue
		}
		out := make([]string, 0, len(b)+1)
		if started {
			out = append(out, "")
		}
		for j, l := range b {
			switch {
			case j == 0 && !started:
				out = append(out, dot+l)
			case l == "":
				out = append(out, "")
			default:
				out = append(out, dotIndent+l)
			}
		}
		started = true
		blocks[i] = out
	}
	return blocks
}

// renderSendUserMessage shows a message the model addressed to the user (brief
// mode's channel) like assistant text.
func (f *Feature) renderSendUserMessage(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in struct {
		Message string `json:"message"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	if strings.TrimSpace(in.Message) == "" {
		return f.renderOneLiner(rc, it)
	}
	blocks := render.MarkdownBlocks(in.Message, f.mdOptions(rc, rc.Width-len(dotIndent)))
	return ext.Block{Lines: flatten(prefixBlocks(rc, blocks))}
}

func (f *Feature) renderText(rc ext.RenderCtx, it *ext.Item) ext.Block {
	blocks, _ := f.textBlocks(rc, it)
	var lines []string
	for _, b := range blocks {
		lines = append(lines, b...)
	}
	if it.State.Finished() {
		// Finished text never changes: drop its streaming state whatever the
		// layout (inline commit also forgets it; fullscreen caches lines itself).
		f.forget(it.ID)
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderThinking(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	b, _ := it.Data.(*proto.ContentBlock)
	think := st.dim.Merge(render.Style{Italic: true})
	head := think.Render(glyphThought + " Thinking…")
	if b != nil && b.Type == proto.BlockRedactedThinking {
		return ext.Block{Lines: []string{think.Render(glyphThought + " Thinking (redacted)")}}
	}
	if it.State.Finished() {
		label := glyphThought + " Thought"
		if d := it.End.Sub(it.Start); !it.Start.IsZero() && d >= time.Second {
			label += " for " + formatDuration(d.Truncate(time.Second))
		}
		head = think.Render(label)
	}
	show := verbose(rc) || f.cfg.showThinking
	if !show || b == nil || strings.TrimSpace(b.Thinking) == "" {
		hidden := b != nil && strings.TrimSpace(b.Thinking) != ""
		if hidden && it.State.Finished() && rc.Mode != ext.Brief {
			head += st.dim.Render(" (ctrl+o to expand)")
		}
		return ext.Block{Lines: truncLines([]string{head}, rc.Width), Collapsible: hidden}
	}
	o := f.mdOptions(rc, rc.Width-len(dotIndent))
	o.Palette = nil
	lines := []string{head}
	for _, l := range render.Markdown(b.Thinking, o) {
		if l == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, dotIndent+think.Render(render.Strip(l)))
	}
	return ext.Block{Lines: lines}
}
