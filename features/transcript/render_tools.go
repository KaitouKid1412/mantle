package transcript

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
	"github.com/KaitouKid1412/mantle/pkg/ui/diffview"
)

// diffCap caps diffs in the normal view (the ctrl+o viewer shows all).
const diffCap = 30

// toolUse returns an item's tool call (nil if the item is not one).
func toolUse(it *ext.Item) *proto.ToolUse {
	tu, _ := it.Data.(*proto.ToolUse)
	return tu
}

func resultText(it *ext.Item) string {
	if it.Result == nil {
		return ""
	}
	return it.Result.Content.PlainText()
}

func structured(it *ext.Item, v any) bool {
	if it.Result == nil || len(it.Result.Structured) == 0 {
		return false
	}
	return json.Unmarshal(it.Result.Structured, v) == nil
}

// toolFrame renders the shared parts of a tool call: the header, then the error,
// interruption or running line, or body() for a successful result.
func (f *Feature) toolFrame(rc ext.RenderCtx, it *ext.Item, name, args string, body func() ([]string, bool)) ext.Block {
	return f.toolFrameOpts(rc, it, name, args, body, false)
}

// toolFrameOpts is toolFrame; showRejected also runs body for a rejected call
// (Edit and Write show the change that was turned down, dimmed).
func (f *Feature) toolFrameOpts(rc ext.RenderCtx, it *ext.Item, name, args string, body func() ([]string, bool), showRejected bool) ext.Block {
	st := stylesFor(rc)
	lines := header(rc, st, bulletStyle(st, it.State), name, args)
	if !verbose(rc) && len(lines) > 3 {
		lines = append(lines[:3:3], dotIndent+st.dim.Render("…"))
	}
	collapsible := false
	switch {
	case it.State == ext.Failed:
		text := strings.TrimSpace(resultText(it))
		if rejected(text) {
			lines = append(lines, result(rc, st.dim, "Rejected")...)
			if body != nil && showRejected {
				// Some renderers show the rejected change dimmed.
				if more, hid := body(); len(more) > 0 {
					lines = append(lines, more...)
					collapsible = hid
				}
			}
			break
		}
		out, hid := output(rc, st.err, stripErrorTags(text), true)
		if len(out) == 0 {
			out = result(rc, st.err, "Error")
		}
		lines = append(lines, out...)
		collapsible = hid
	case it.State == ext.Interrupted:
		lines = append(lines, result(rc, st.err, interruptHint)...)
	case it.Result == nil:
		if f.store.Waiting(it.ID) {
			lines = append(lines, result(rc, st.dim, "Waiting…")...)
		} else if run := f.runningLine(rc, it); run != "" {
			lines = append(lines, result(rc, st.dim, run)...)
		}
	case body != nil:
		more, hid := body()
		lines = append(lines, more...)
		collapsible = hid
	}
	return ext.Block{Lines: lines, Collapsible: collapsible}
}

// runningLine is the "Running… 12s" line of a tool still in progress (only
// once a progress heartbeat arrived).
func (f *Feature) runningLine(rc ext.RenderCtx, it *ext.Item) string {
	info := f.store.Tool(it.ID)
	if info == nil || info.Elapsed <= 0 {
		return ""
	}
	return "Running… " + formatDuration(info.Elapsed)
}

// rejected recognises the engine's "user said no" tool results.
func rejected(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "doesn't want to proceed") || strings.Contains(t, "tool use was rejected") ||
		strings.Contains(t, "user rejected")
}

var errorTags = strings.NewReplacer("<tool_use_error>", "", "</tool_use_error>", "", "<error>", "", "</error>", "")

func stripErrorTags(s string) string { return strings.TrimSpace(errorTags.Replace(s)) }

// ---- Bash ----

type bashInput struct {
	Command         string `json:"command"`
	Description     string `json:"description"`
	RunInBackground bool   `json:"run_in_background"`
}

type bashOutput struct {
	Stdout           string `json:"stdout"`
	Stderr           string `json:"stderr"`
	Interrupted      bool   `json:"interrupted"`
	IsImage          bool   `json:"isImage"`
	BackgroundTaskID string `json:"backgroundTaskId"`
	ReturnCodeInterp string `json:"returnCodeInterpretation"`
	NoOutputExpected bool   `json:"noOutputExpected"`
}

func (f *Feature) renderBashTool(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in bashInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	cmd := strings.TrimSpace(in.Command)
	if !verbose(rc) {
		if i := strings.IndexByte(cmd, '\n'); i >= 0 {
			cmd = cmd[:i] + " …"
		}
	}
	return f.toolFrame(rc, it, "Bash", cmd, func() ([]string, bool) {
		st := stylesFor(rc)
		var out bashOutput
		if !structured(it, &out) {
			out.Stdout = resultText(it)
		}
		if out.BackgroundTaskID != "" {
			return result(rc, st.dim, "Running in the background (id: "+clean(out.BackgroundTaskID)+")"), false
		}
		if out.IsImage {
			return result(rc, st.dim, "[Image]"), false
		}
		lines, hid1 := output(rc, render.Style{}, out.Stdout, true)
		errLines, hid2 := output(rc, st.err, out.Stderr, len(lines) == 0)
		lines = append(lines, errLines...)
		if out.Interrupted {
			lines = append(lines, result(rc, st.dim, "Interrupted")...)
		}
		if out.ReturnCodeInterp != "" {
			lines = append(lines, result(rc, st.dim, clean(out.ReturnCodeInterp))...)
		}
		if len(lines) == 0 {
			lines = result(rc, st.dim, "Done")
		}
		return fixFirst(lines), hid1 || hid2
	})
}

// fixFirst makes sure only the first line carries the ⎿ glyph.
func fixFirst(lines []string) []string {
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], resultIndent) {
			lines[i] = resultHang + strings.TrimPrefix(lines[i], resultIndent)
		}
	}
	return lines
}

// ---- Read ----

type readInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	Pages    string `json:"pages"`
}

type readOutput struct {
	Type string `json:"type"`
	File struct {
		FilePath   string `json:"filePath"`
		Content    string `json:"content"`
		NumLines   int    `json:"numLines"`
		StartLine  int    `json:"startLine"`
		TotalLines int    `json:"totalLines"`
		NumPages   int    `json:"numPages"`
	} `json:"file"`
}

func (f *Feature) renderRead(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in readInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	args := fileLink(relPath(f.cwd, in.FilePath), in.FilePath)
	switch {
	case in.Pages != "":
		args += " · pages " + in.Pages
	case in.Offset > 0 && in.Limit > 0:
		args += fmt.Sprintf(" · lines %d-%d", in.Offset, in.Offset+in.Limit-1)
	case in.Limit > 0:
		args += fmt.Sprintf(" · first %d lines", in.Limit)
	}
	return f.toolFrame(rc, it, "Read", args, func() ([]string, bool) {
		st := stylesFor(rc)
		var out readOutput
		structured(it, &out)
		switch out.Type {
		case "image":
			return result(rc, st.dim, "Read image"), false
		case "pdf":
			return result(rc, st.dim, "Read PDF"), false
		case "notebook":
			return result(rc, st.dim, "Read notebook"), false
		}
		n := out.File.NumLines
		if n == 0 {
			n = strings.Count(strings.TrimRight(resultText(it), "\n"), "\n") + 1
		}
		summary := "Read " + plural(n, "line", "lines")
		if !verbose(rc) {
			return result(rc, st.dim, summary+" (ctrl+o to expand)"), true
		}
		lines := result(rc, st.dim, summary)
		if out.File.Content != "" {
			lines = append(lines, f.codePreview(rc, out.File.Content, in.FilePath, max(out.File.StartLine, 1), 0)...)
		}
		return lines, false
	})
}

// codePreview renders numbered, highlighted file content under the result
// gutter, capped at limit lines (0 = no cap).
func (f *Feature) codePreview(rc ext.RenderCtx, content, filename string, start, limit int) []string {
	content = strings.TrimRight(clean(content), "\n")
	if content == "" {
		return nil
	}
	hl := render.DefaultHighlighter
	if f.cfg.noHighlight {
		hl = nil
	}
	src := hl.Highlight(content, "", filename, paletteOf(rc.Theme))
	hidden := 0
	if limit > 0 && len(src) > limit {
		hidden = len(src) - limit
		src = src[:limit]
	}
	gw := len(strconv.Itoa(start + len(src) - 1))
	dim := stylesFor(rc).dim
	width := max(rc.Width-len(resultHang), 1)
	var out []string
	for i, l := range src {
		num := strconv.Itoa(start + i)
		gutter := dim.Render(strings.Repeat(" ", gw-len(num))+num) + " "
		out = append(out, render.WrapWith(l, render.WrapOptions{Width: width, First: gutter, Rest: strings.Repeat(" ", gw+1), Hard: true})...)
	}
	out = indentLines(out, false)
	if hidden > 0 {
		out = append(out, resultHang+moreLines(rc, hidden))
	}
	return out
}

// ---- Write / Edit / MultiEdit ----

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
	Edits      []struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	} `json:"edits"`
}

type patchOutput struct {
	Type            string           `json:"type"` // Write: create | update
	FilePath        string           `json:"filePath"`
	StructuredPatch []diffview.Hunk  `json:"structuredPatch"`
	Content         string           `json:"content"`
	OriginalFile    *json.RawMessage `json:"originalFile"`
}

func (f *Feature) diffLines(rc ext.RenderCtx, hunks []diffview.Hunk, filename string, dimmed, gutter bool) ([]string, bool) {
	o := diffview.Options{
		Width:       max(rc.Width-len(resultHang), 1),
		Palette:     paletteOf(rc.Theme),
		Filename:    filename,
		NoHighlight: f.cfg.noHighlight,
		Dimmed:      dimmed,
		NoGutter:    !gutter,
	}
	if !verbose(rc) {
		o.MaxLines = diffCap
	}
	lines := diffview.Render(hunks, o)
	hidden := o.MaxLines > 0 && len(lines) == o.MaxLines && strings.Contains(render.Strip(lines[len(lines)-1]), "… +")
	return indentLines(lines, false), hidden
}

func changeSummary(path string, added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, plural(added, "addition", "additions"))
	}
	if removed > 0 {
		parts = append(parts, plural(removed, "removal", "removals"))
	}
	if len(parts) == 0 {
		return "No changes to " + path
	}
	return "Updated " + path + " with " + strings.Join(parts, " and ")
}

func (f *Feature) renderWrite(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in writeInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	var out patchOutput
	structured(it, &out)
	path := relPath(f.cwd, in.FilePath)
	name := "Write"
	if out.Type == "update" {
		name = "Update"
	}
	return f.toolFrameOpts(rc, it, name, fileLink(path, in.FilePath), func() ([]string, bool) {
		st := stylesFor(rc)
		if it.State == ext.Failed { // rejected: show what would have been written
			hunks := diffview.FromStrings("", in.Content, 0)
			return f.diffLines(rc, hunks, in.FilePath, true, true)
		}
		if out.Type == "update" && len(out.StructuredPatch) > 0 {
			a, r := diffview.Counts(out.StructuredPatch)
			lines := result(rc, st.text, changeSummary(path, a, r))
			d, hid := f.diffLines(rc, out.StructuredPatch, in.FilePath, false, true)
			return append(lines, d...), hid
		}
		content := in.Content
		if content == "" {
			content = out.Content
		}
		n := 0
		if content != "" {
			n = strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
		}
		lines := result(rc, st.text, "Wrote "+plural(n, "line", "lines")+" to "+path)
		limit := 10
		if verbose(rc) {
			limit = 0
		}
		preview := f.codePreview(rc, content, in.FilePath, 1, limit)
		return append(lines, preview...), limit > 0 && n > limit
	}, true)
}

func (f *Feature) renderEdit(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in editInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	path := relPath(f.cwd, in.FilePath)
	return f.toolFrameOpts(rc, it, "Update", fileLink(path, in.FilePath), func() ([]string, bool) {
		st := stylesFor(rc)
		if it.State == ext.Failed {
			var hunks []diffview.Hunk
			if len(in.Edits) > 0 {
				for _, e := range in.Edits {
					hunks = append(hunks, diffview.FromStrings(e.OldString, e.NewString, 0)...)
				}
			} else {
				hunks = diffview.FromStrings(in.OldString, in.NewString, 0)
			}
			return f.diffLines(rc, hunks, in.FilePath, true, false)
		}
		var out patchOutput
		structured(it, &out)
		hunks := out.StructuredPatch
		gutter := true
		if len(hunks) == 0 {
			hunks = diffview.FromStrings(in.OldString, in.NewString, 0)
			gutter = false
		}
		a, r := diffview.Counts(hunks)
		lines := result(rc, st.text, changeSummary(path, a, r))
		d, hid := f.diffLines(rc, hunks, in.FilePath, false, gutter)
		return append(lines, d...), hid
	}, true)
}

// ---- Glob / Grep ----

type searchInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	Type       string `json:"type"`
	OutputMode string `json:"output_mode"`
}

type searchOutput struct {
	Mode       string   `json:"mode"`
	Filenames  []string `json:"filenames"`
	NumFiles   int      `json:"numFiles"`
	NumLines   int      `json:"numLines"`
	NumMatches int      `json:"numMatches"`
	Content    string   `json:"content"`
	Truncated  bool     `json:"truncated"`
}

func (f *Feature) searchArgs(in searchInput) string {
	q := func(s string) string { return `"` + s + `"` }
	args := "pattern: " + q(in.Pattern)
	if in.Path != "" {
		args += ", path: " + q(relPath(f.cwd, in.Path))
	}
	if in.Glob != "" {
		args += ", glob: " + q(in.Glob)
	}
	if in.Type != "" {
		args += ", type: " + q(in.Type)
	}
	return args
}

func (f *Feature) renderSearch(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in searchInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	return f.toolFrame(rc, it, "Search", f.searchArgs(in), func() ([]string, bool) {
		st := stylesFor(rc)
		var out searchOutput
		if !structured(it, &out) {
			return output(rc, render.Style{}, resultText(it), true)
		}
		var summary string
		switch {
		case out.Mode == "content":
			summary = "Found " + plural(max(out.NumLines, strings.Count(out.Content, "\n")), "line", "lines")
		case out.Mode == "count":
			summary = fmt.Sprintf("Found %s across %s", plural(out.NumMatches, "match", "matches"), plural(out.NumFiles, "file", "files"))
		default:
			n := out.NumFiles
			if n == 0 {
				n = len(out.Filenames)
			}
			summary = "Found " + plural(n, "file", "files")
		}
		if out.Truncated {
			summary += " (truncated)"
		}
		if !verbose(rc) {
			if out.NumFiles+out.NumLines+len(out.Filenames) > 0 {
				summary += " (ctrl+o to expand)"
			}
			return result(rc, st.dim, summary), true
		}
		lines := result(rc, st.dim, summary)
		body := out.Content
		if body == "" {
			var rel []string
			for _, p := range out.Filenames {
				rel = append(rel, relPath(f.cwd, p))
			}
			body = strings.Join(rel, "\n")
		}
		more, _ := output(rc, render.Style{}, body, false)
		return append(lines, more...), false
	})
}

// ---- WebFetch / WebSearch ----

type fetchInput struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

type fetchOutput struct {
	Bytes    int64  `json:"bytes"`
	Code     int    `json:"code"`
	CodeText string `json:"codeText"`
	Result   string `json:"result"`
}

func (f *Feature) renderFetch(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in fetchInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	return f.toolFrame(rc, it, "Fetch", in.URL, func() ([]string, bool) {
		st := stylesFor(rc)
		var out fetchOutput
		if !structured(it, &out) {
			return output(rc, render.Style{}, resultText(it), true)
		}
		summary := "Received " + formatBytes(out.Bytes)
		if out.Code != 0 {
			summary += fmt.Sprintf(" (%d %s)", out.Code, clean(out.CodeText))
		}
		lines := result(rc, st.dim, strings.TrimSpace(summary))
		if verbose(rc) && out.Result != "" {
			more, _ := output(rc, render.Style{}, out.Result, false)
			lines = append(lines, more...)
		}
		return lines, !verbose(rc) && out.Result != ""
	})
}

type webSearchInput struct {
	Query string `json:"query"`
}

type webSearchOutput struct {
	Query           string            `json:"query"`
	Results         []json.RawMessage `json:"results"`
	DurationSeconds float64           `json:"durationSeconds"`
}

func (f *Feature) renderWebSearch(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in webSearchInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	return f.toolFrame(rc, it, "Web Search", `"`+in.Query+`"`, func() ([]string, bool) {
		st := stylesFor(rc)
		var out webSearchOutput
		if !structured(it, &out) {
			return output(rc, render.Style{}, resultText(it), true)
		}
		var links []string
		searches := 0
		for _, r := range out.Results {
			var block struct {
				Content []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
				} `json:"content"`
			}
			if json.Unmarshal(r, &block) == nil && block.Content != nil {
				searches++
				for _, c := range block.Content {
					links = append(links, render.Link(c.URL, clean(c.Title)))
				}
			}
		}
		summary := "Did " + plural(max(searches, 1), "search", "searches")
		if out.DurationSeconds > 0 {
			summary += " in " + formatDuration(secondsToDuration(out.DurationSeconds))
		}
		lines := result(rc, st.dim, summary)
		if verbose(rc) {
			for _, l := range links {
				lines = append(lines, render.WrapWith(l, render.WrapOptions{Width: rc.Width, First: resultHang + "· ", Rest: resultHang + "  "})...)
			}
		}
		return lines, !verbose(rc) && len(links) > 0
	})
}

// ---- Agent / Task ----

type agentInput struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type"`
	Background   bool   `json:"run_in_background"`
}

type agentOutput struct {
	Status            string `json:"status"`
	TotalDurationMS   int64  `json:"totalDurationMs"`
	TotalTokens       int64  `json:"totalTokens"`
	TotalToolUseCount int64  `json:"totalToolUseCount"`
}

func (f *Feature) renderAgent(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	var in agentInput
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	name := in.SubagentType
	if name == "" || name == "general-purpose" {
		name = "Agent"
	}
	lines := header(rc, st, bulletStyle(st, it.State), name, oneLine(in.Description))
	kids := rc.Children
	var tools []*ext.Item
	for _, k := range kids {
		if toolUse(k) != nil {
			tools = append(tools, k)
		}
	}
	info := f.store.Tool(it.ID)
	var out agentOutput
	structured(it, &out)

	if verbose(rc) {
		// Every nested item, rendered and indented.
		for _, k := range kids {
			r := f.renderNested(rc, k, max(rc.Width-len(resultHang), 10))
			lines = append(lines, indentLines(r, false)...)
		}
		if t := strings.TrimSpace(resultText(it)); t != "" && it.State.Finished() {
			md := render.Markdown(t, f.mdOptions(rc, rc.Width-len(resultHang)))
			lines = append(lines, indentLines(md, true)...)
		}
	}
	switch {
	case it.State == ext.Failed:
		o, _ := output(rc, st.err, stripErrorTags(resultText(it)), true)
		lines = append(lines, o...)
	case it.State == ext.Interrupted:
		lines = append(lines, result(rc, st.dim, "Interrupted")...)
	case it.State.Finished() && (out.Status == "async_launched" || (info != nil && info.Async && info.Status == "")):
		lines = append(lines, result(rc, st.dim, "Running in the background")...)
	case it.State.Finished():
		lines = append(lines, result(rc, st.dim, "Done ("+agentStats(out, info, len(tools), it)+")")...)
	case !verbose(rc):
		// Running: the last few tool calls, oldest first.
		const show = 3
		start := max(len(tools)-show, 0)
		var recent []string
		for _, k := range tools[start:] {
			recent = append(recent, f.toolTitle(rc, k))
		}
		if len(recent) == 0 {
			recent = append(recent, st.dim.Render("Starting…"))
		}
		for i, r := range recent {
			first, rest := resultHang, resultHang
			if i == 0 {
				first = resultIndent
			}
			lines = append(lines, truncLines(render.WrapWith(r, render.WrapOptions{Width: rc.Width, First: first, Rest: rest}), rc.Width)[0])
		}
		if start > 0 {
			lines = append(lines, resultHang+st.dim.Render(fmt.Sprintf("+%s (ctrl+o to expand)", plural(start, "more tool use", "more tool uses"))))
		}
	}
	return ext.Block{Lines: lines, Collapsible: len(kids) > 0}
}

func agentStats(out agentOutput, info *ToolInfo, tools int, it *ext.Item) string {
	uses, tokens, dur := out.TotalToolUseCount, out.TotalTokens, out.TotalDurationMS
	if info != nil && info.Task != nil {
		uses = max(uses, info.Task.ToolUses)
		tokens = max(tokens, info.Task.TotalTokens)
		dur = max(dur, info.Task.DurationMS)
	}
	uses = max(uses, int64(tools))
	parts := []string{plural(int(uses), "tool use", "tool uses")}
	if tokens > 0 {
		parts = append(parts, formatTokens(tokens)+" tokens")
	}
	if dur > 0 {
		parts = append(parts, formatDuration(msToDuration(dur)))
	} else if !it.Start.IsZero() && it.End.After(it.Start) {
		parts = append(parts, formatDuration(it.End.Sub(it.Start)))
	}
	return strings.Join(parts, " · ")
}

// toolTitle is a one-line "Name(args)" for a nested tool call.
func (f *Feature) toolTitle(rc ext.RenderCtx, it *ext.Item) string {
	r := f.renderNested(rc, it, 1000)
	if len(r) == 0 {
		return ""
	}
	// Drop the dot of the nested header.
	return strings.TrimPrefix(render.Strip(r[0]), glyphDot+" ")
}

// renderNested renders a child item with the resolved renderer at a width.
func (f *Feature) renderNested(rc ext.RenderCtx, it *ext.Item, width int) []string {
	nrc := rc
	nrc.Width = width
	nrc.Children = f.store.Children(it.ID)
	r := f.rendererFor(it.Key)
	if r == nil {
		return nil
	}
	return r(nrc, it).Lines
}

// ---- TodoWrite and task tools ----

type todo struct {
	Content    string `json:"content"`
	Subject    string `json:"subject"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
	ID         string `json:"id"`
}

func todoLine(rc ext.RenderCtx, t todo) string {
	st := stylesFor(rc)
	text := clean(t.Content)
	if text == "" {
		text = clean(t.Subject)
	}
	switch t.Status {
	case "completed":
		return st.dim.Render("☒ ") + st.dim.Merge(render.Style{Strike: true}).Render(text)
	case "in_progress":
		return st.bold.Render("◼ " + text)
	}
	return "☐ " + text
}

// Task and todo tools have no transcript row in the normal view: the task
// list above the prompt (plan 07) shows them. The ctrl+o viewer and verbose
// view still list them.
func taskRowHidden(rc ext.RenderCtx) bool { return !verbose(rc) }

func (f *Feature) renderTodos(rc ext.RenderCtx, it *ext.Item) ext.Block {
	if taskRowHidden(rc) {
		return ext.Block{}
	}
	var in struct {
		Todos []todo `json:"todos"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	return f.toolFrame(rc, it, "Update Todos", "", func() ([]string, bool) {
		var lines []string
		for i, t := range in.Todos {
			first, rest := resultHang, resultHang+"  "
			if i == 0 {
				first = resultIndent
			}
			lines = append(lines, render.WrapWith(todoLine(rc, t), render.WrapOptions{Width: rc.Width, First: first, Rest: rest})...)
		}
		return lines, false
	})
}

func (f *Feature) renderTaskTool(rc ext.RenderCtx, it *ext.Item) ext.Block {
	if taskRowHidden(rc) {
		return ext.Block{}
	}
	tu := toolUse(it)
	var in struct {
		todo
		TaskID string `json:"taskId"`
	}
	if tu != nil {
		decodeInput(tu.Input, &in)
	}
	name := "Task"
	args := ""
	if tu != nil {
		switch tu.Name {
		case "TaskCreate":
			name, args = "Create Task", oneLine(in.Subject)
		case "TaskUpdate":
			name, args = "Update Task", "#"+in.TaskID
		case "TaskList":
			name = "List Tasks"
		case "TaskGet":
			name, args = "Get Task", "#"+in.TaskID
		}
	}
	return f.toolFrame(rc, it, name, args, func() ([]string, bool) {
		if tu != nil && tu.Name == "TaskUpdate" && in.Status != "" {
			subject := in.Subject
			if subject == "" {
				subject = "#" + in.TaskID
			}
			return result(rc, render.Style{}, todoLine(rc, todo{Content: subject, Status: in.Status})), false
		}
		if tu != nil && tu.Name == "TaskCreate" {
			return result(rc, render.Style{}, todoLine(rc, todo{Content: in.Subject, Status: "pending"})), false
		}
		return output(rc, stylesFor(rc).dim, resultText(it), true)
	})
}

// ---- Plan mode, questions ----

func (f *Feature) renderExitPlan(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	var in struct {
		Plan         string `json:"plan"`
		PlanFilePath string `json:"planFilePath"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	var out struct {
		FilePath string `json:"filePath"`
	}
	structured(it, &out)
	title := "Plan ready for review"
	switch it.State {
	case ext.Done:
		title = "Exited plan mode"
	case ext.Failed:
		title = "Stayed in plan mode"
	case ext.Interrupted:
		title = "Plan review interrupted"
	}
	lines := header(rc, st, bulletStyle(st, it.State), title, "")
	if f.store.Waiting(it.ID) {
		lines = append(lines, result(rc, st.dim, "Waiting…")...)
	}
	// The plan itself is in the approval dialog; the transcript shows it only
	// at full detail (verbose, ctrl+o).
	if verbose(rc) && strings.TrimSpace(in.Plan) != "" {
		bar := st.plan.Render("│") + " "
		for _, l := range render.Markdown(in.Plan, f.mdOptions(rc, rc.Width-len(dotIndent)-2)) {
			if l == "" {
				lines = append(lines, dotIndent+st.plan.Render("│"))
				continue
			}
			lines = append(lines, dotIndent+bar+l)
		}
	}
	if path := firstNonEmpty(out.FilePath, in.PlanFilePath); path != "" && verbose(rc) {
		lines = append(lines, result(rc, st.dim, "Plan saved to "+relPath(f.cwd, path))...)
	}
	if it.State == ext.Failed {
		if t := stripErrorTags(resultText(it)); t != "" && !rejected(t) {
			o, _ := output(rc, st.dim, t, true)
			lines = append(lines, o...)
		}
	}
	return ext.Block{Lines: lines, Collapsible: strings.TrimSpace(in.Plan) != ""}
}

func (f *Feature) renderEnterPlan(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	lines := header(rc, st, bulletStyle(st, it.State), "Entered plan mode", "")
	if it.State == ext.Failed {
		o, _ := output(rc, st.err, resultText(it), true)
		lines = append(lines, o...)
	}
	return ext.Block{Lines: lines}
}

type question struct {
	Question string `json:"question"`
	Header   string `json:"header"`
}

func (f *Feature) renderAskUser(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in struct {
		Questions []question `json:"questions"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	var out struct {
		Answers map[string]any `json:"answers"`
	}
	structured(it, &out)
	title := "Asked questions"
	if len(out.Answers) > 0 {
		title = "Questions answered"
	}
	return f.toolFrame(rc, it, title, "", func() ([]string, bool) {
		st := stylesFor(rc)
		var lines []string
		for i, q := range in.Questions {
			ans := "—"
			if a, ok := out.Answers[q.Question]; ok {
				ans = answerText(a)
			}
			first := resultHang
			if i == 0 {
				first = resultIndent
			}
			text := "· " + clean(q.Question) + " " + st.dim.Render("→") + " " + st.bold.Render(clean(ans))
			lines = append(lines, render.WrapWith(text, render.WrapOptions{Width: rc.Width, First: first, Rest: resultHang + "  "})...)
		}
		if len(lines) == 0 {
			return output(rc, render.Style{}, resultText(it), true)
		}
		return lines, false
	})
}

func answerText(a any) string {
	switch v := a.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, e := range v {
			parts = append(parts, answerText(e))
		}
		return strings.Join(parts, ", ")
	}
	b, _ := json.Marshal(a)
	return string(b)
}

// ---- NotebookEdit, Skill, one-liners ----

func (f *Feature) renderNotebook(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in struct {
		NotebookPath string `json:"notebook_path"`
		CellID       string `json:"cell_id"`
		NewSource    string `json:"new_source"`
		EditMode     string `json:"edit_mode"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	var out struct {
		OldSource string `json:"old_source"`
		NewSource string `json:"new_source"`
		CellID    string `json:"cell_id"`
		Language  string `json:"language"`
	}
	structured(it, &out)
	return f.toolFrame(rc, it, "Edit Notebook", fileLink(relPath(f.cwd, in.NotebookPath), in.NotebookPath), func() ([]string, bool) {
		st := stylesFor(rc)
		cell := in.CellID
		if cell == "" {
			cell = out.CellID
		}
		verb := "Updated"
		switch in.EditMode {
		case "insert":
			verb = "Inserted"
		case "delete":
			verb = "Deleted"
		}
		lines := result(rc, st.text, verb+" cell "+clean(cell))
		newSrc := in.NewSource
		if newSrc == "" {
			newSrc = out.NewSource
		}
		if in.EditMode == "delete" {
			return lines, false
		}
		lang := out.Language
		if lang == "" {
			lang = "python"
		}
		hunks := diffview.FromStrings(out.OldSource, newSrc, 1)
		o := diffview.Options{Width: max(rc.Width-len(resultHang), 1), Palette: st.p, Lang: lang, NoHighlight: f.cfg.noHighlight}
		if !verbose(rc) {
			o.MaxLines = diffCap
		}
		return append(lines, indentLines(diffview.Render(hunks, o), false)...), false
	})
}

func (f *Feature) renderSkill(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var in struct {
		Skill   string `json:"skill"`
		Command string `json:"command"`
		Args    string `json:"args"`
	}
	if tu := toolUse(it); tu != nil {
		decodeInput(tu.Input, &in)
	}
	name := in.Skill
	if name == "" {
		name = in.Command
	}
	if in.Args != "" {
		name += " " + oneLine(in.Args)
	}
	return f.toolFrame(rc, it, "Skill", name, func() ([]string, bool) {
		return result(rc, stylesFor(rc).dim, "Loaded skill"), false
	})
}

// renderOneLiner shows a tool as its header plus the first line of its result.
func (f *Feature) renderOneLiner(rc ext.RenderCtx, it *ext.Item) ext.Block {
	tu := toolUse(it)
	name, args := "Tool", ""
	if tu != nil {
		name = tu.Name
		args = argSummary(tu.Input, rc.Width)
	}
	return f.toolFrame(rc, it, name, args, func() ([]string, bool) {
		text := strings.TrimSpace(clean(resultText(it)))
		if text == "" {
			return nil, false
		}
		first, _, more := strings.Cut(text, "\n")
		lines := truncLines(result(rc, stylesFor(rc).dim, first), rc.Width)[:1]
		return lines, more
	})
}

// renderMCP shows "server - tool (MCP)(args)" and the result text.
func (f *Feature) renderMCP(rc ext.RenderCtx, it *ext.Item) ext.Block {
	tu := toolUse(it)
	server, tool := mcpNames(it, tu)
	args := ""
	if tu != nil {
		args = argSummary(tu.Input, 0)
	}
	if !verbose(rc) && render.Width(args) > 2*rc.Width {
		args = render.Truncate(args, 2*rc.Width, "…")
	}
	return f.toolFrame(rc, it, server+" - "+tool+" (MCP)", args, func() ([]string, bool) {
		return output(rc, render.Style{}, resultText(it), true)
	})
}

// mcpNames splits an MCP tool call into server and tool names.
func mcpNames(it *ext.Item, tu *proto.ToolUse) (server, tool string) {
	if tu != nil && tu.ServerName != "" {
		return tu.ServerName, tu.Name
	}
	k := strings.TrimPrefix(string(it.Key), "tool.mcp.")
	server, tool, _ = strings.Cut(k, ".")
	if tool == "" && tu != nil {
		tool = tu.Name
	}
	return server, tool
}

// renderDefaultTool is the fallback for tools without a renderer: name, input
// summary and result text.
func (f *Feature) renderDefaultTool(rc ext.RenderCtx, it *ext.Item) ext.Block {
	tu := toolUse(it)
	if tu == nil {
		return f.renderUnknown(rc, it)
	}
	return f.toolFrame(rc, it, tu.Name, argSummary(tu.Input, 2*rc.Width), func() ([]string, bool) {
		return output(rc, render.Style{}, resultText(it), true)
	})
}

// renderUnknown is the "default" renderer for items of any unknown kind.
func (f *Feature) renderUnknown(rc ext.RenderCtx, it *ext.Item) ext.Block {
	if toolUse(it) != nil {
		return f.renderDefaultTool(rc, it)
	}
	st := stylesFor(rc)
	text := ""
	switch d := it.Data.(type) {
	case *proto.ContentBlock:
		text = d.Text
	case string:
		text = d
	case proto.Event:
		if env := d.Env(); env != nil {
			text = env.Type
			if env.Subtype != "" {
				text += "/" + env.Subtype
			}
		}
	}
	if text == "" {
		text = string(it.Key)
	}
	return ext.Block{Lines: render.WrapWith(st.dim.Render(clean(text)), render.WrapOptions{Width: rc.Width, First: st.dim.Render(glyphDot) + " ", Rest: dotIndent})}
}
