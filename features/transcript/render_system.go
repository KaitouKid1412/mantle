package transcript

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// doneVerbs open the turn duration line; one is picked per turn.
var doneVerbs = []string{"Worked", "Cooked", "Brewed", "Crafted", "Churned", "Baked", "Simmered", "Tinkered"}

func pickVerb(seed string) string {
	h := fnv.New32a()
	h.Write([]byte(seed))
	return doneVerbs[h.Sum32()%uint32(len(doneVerbs))]
}

// interruptHint follows the interrupt marker: the next prompt tells Claude
// what to do instead.
const interruptHint = "Interrupted · tell Claude what to do instead"

// renderResult draws the end of a turn: the interrupt line (attached to the
// item above it, see attached), errors, denied calls the transcript does not
// already show, and the duration line.
func (f *Feature) renderResult(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	r, _ := it.Data.(*proto.Result)
	if r == nil {
		return ext.Block{}
	}
	var head, body []string
	switch {
	case r.Interrupted():
		if !f.store.InterruptShown(r.UUID) {
			head = result(rc, st.err, interruptHint)
		}
	case r.IsError && f.store.ErrorShown(r.UUID):
	case r.IsError:
		msg := strings.TrimSpace(r.Result)
		if len(r.Errors) > 0 {
			msg = strings.Join(r.Errors, "\n")
		}
		if msg == "" {
			msg = "The turn ended with an error (" + r.Subtype + ")"
		}
		o, _ := output(rc, st.err, msg, true)
		body = append(body, o...)
	}
	for _, d := range r.PermissionDenials {
		if t := f.store.Get(d.ToolUseID); t != nil && (t.State == ext.Failed || t.State == ext.Interrupted) {
			continue // the tool row already shows the denial
		}
		args := argSummary(d.ToolInput, rc.Width)
		text := "Denied: " + d.ToolName
		if args != "" {
			text += "(" + args + ")"
		}
		body = append(body, truncLines(render.WrapWith(st.dim.Render(clean(text)), render.WrapOptions{Width: rc.Width, First: st.err.Render(glyphDot) + " ", Rest: dotIndent}), rc.Width)...)
	}
	if n := f.store.WaitingAgents(r.UUID); n > 0 && r.NumTurns > 0 {
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, st.dim.Render(glyphThought+" Waiting for "+plural(n, "background agent", "background agents")+" to finish"))
	} else if f.cfg.showTurnDuration && r.NumTurns > 0 && r.TerminalReason != proto.TerminalAbortedStreaming && (r.Interrupted() || !r.IsError) {
		line := glyphThought + " " + pickVerb(r.UUID+it.ID) + " for " + formatDuration(msToDuration(r.DurationMS))
		if t := f.formatClock(it.End); t != "" {
			line += " · done " + t
		}
		if len(body) > 0 {
			body = append(body, "")
		}
		body = append(body, st.dim.Render(line))
	}
	lines := head
	if len(head) > 0 && len(body) > 0 {
		lines = append(lines, "")
	}
	return ext.Block{Lines: append(lines, body...)}
}

// attached reports whether an item's first line continues the item above it
// (no blank line between): the interrupt line of an interrupted turn.
// prev is the item above it ("" item when none). Attached are the interrupt
// line of an interrupted turn and a local command's output under its prompt.
func (f *Feature) attached(it, prev *ext.Item) bool {
	if it.Key == ext.KeySystemLocalCommand {
		return prev != nil && prev.Key == ext.KeyUserPrompt
	}
	r, ok := it.Data.(*proto.Result)
	return ok && it.Key == KeyResult && r.Interrupted() && !f.store.InterruptShown(r.UUID)
}

// formatClock formats a time of day (timeFormat, timeZone; 12-hour by default).
func (f *Feature) formatClock(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = f.zoned(t)
	if f.cfg.timeFormat == "24h" {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// zoned converts a time to the timeZone setting (local time by default).
func (f *Feature) zoned(t time.Time) time.Time {
	if f.cfg.timeZone != "" {
		if loc, err := time.LoadLocation(f.cfg.timeZone); err == nil {
			return t.In(loc)
		}
	}
	return t.Local()
}

// clockTime is formatClock for message timestamps: "" unless timestamps or a
// time format are configured.
func (f *Feature) clockTime(t time.Time) string {
	if !f.cfg.showTimestamps && f.cfg.timeFormat == "" {
		return ""
	}
	return f.formatClock(t)
}

var errorText = map[string]string{
	proto.AssistantErrAuthenticationFailed: "Not signed in, or the credentials were rejected. Run /login.",
	proto.AssistantErrBillingError:         "There is a billing problem with this account.",
	proto.AssistantErrRateLimit:            "Rate limited. Wait a moment, or check /usage.",
	proto.AssistantErrOverloaded:           "The API is overloaded. Try again shortly.",
	proto.AssistantErrInvalidRequest:       "The API rejected the request.",
	proto.AssistantErrModelNotFound:        "The selected model is not available. Pick another with /model.",
	proto.AssistantErrServerError:          "The API returned a server error.",
	proto.AssistantErrMaxOutputTokens:      "The response hit the output token limit.",
}

func (f *Feature) renderError(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	var text, cat string
	switch d := it.Data.(type) {
	case *proto.Assistant:
		cat = d.Error
		for _, b := range d.Message.Content {
			if b.Type == proto.BlockText {
				text += b.Text
			}
		}
	case string:
		text = d
	}
	head := errorText[cat]
	if head == "" {
		head = "API error"
	}
	lines := render.WrapWith(st.err.Render(head), render.WrapOptions{Width: rc.Width, First: st.err.Render(glyphDot) + " ", Rest: dotIndent})
	if t := strings.TrimSpace(text); t != "" && t != head {
		o, hid := output(rc, st.dim, t, true)
		return ext.Block{Lines: append(lines, o...), Collapsible: hid}
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderLocalCommand(rc ext.RenderCtx, it *ext.Item) ext.Block {
	var text string
	switch d := it.Data.(type) {
	case *proto.Assistant:
		for _, b := range d.Message.Content {
			if b.Type == proto.BlockText {
				text += b.Text
			}
		}
	case *proto.LocalCommandOutput:
		text = d.Content
	case *proto.User:
		text = d.Message.Content.PlainText()
	case string:
		text = d
	}
	text = strings.TrimRight(render.Sanitize(text, render.SanitizeOptions{KeepSGR: true}), "\n")
	text = strings.TrimSpace(stripTags(text, "local-command-stdout", "local-command-stderr"))
	if text == "" {
		return ext.Block{}
	}
	width := max(rc.Width-len(resultHang), 1)
	if looksLikeMarkdown(text) {
		md := render.Markdown(render.Strip(text), f.mdOptions(rc, width))
		return ext.Block{Lines: indentLines(md, true)}
	}
	var lines []string
	for _, l := range strings.Split(render.ExpandTabs(text, 4), "\n") {
		lines = append(lines, render.WrapWith(stylesFor(rc).dim.Render(l), render.WrapOptions{Width: width})...)
	}
	if strings.TrimSpace(render.Strip(text)) == "Compacted" && len(lines) == 1 {
		lines[0] += stylesFor(rc).dim.Render(" (ctrl+o for the summary)")
	}
	return ext.Block{Lines: indentLines(lines, true)}
}

// looksLikeMarkdown reports whether command output is structured markdown
// (headings, tables, fences) rather than column-aligned plain text, which
// markdown would reflow.
func looksLikeMarkdown(text string) bool {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "```") ||
			(strings.HasPrefix(l, "|") && strings.HasSuffix(l, "|")) {
			return true
		}
	}
	return false
}

func stripTags(s string, tags ...string) string {
	for _, t := range tags {
		s = strings.ReplaceAll(s, "<"+t+">", "")
		s = strings.ReplaceAll(s, "</"+t+">", "")
	}
	return s
}

func (f *Feature) renderCompact(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	line := glyphThought + " Conversation compacted (ctrl+o for history)"
	if cb, _ := it.Data.(*proto.CompactBoundary); cb != nil && verbose(rc) && cb.CompactMetadata.PreTokens > 0 {
		line += " · " + formatTokens(cb.CompactMetadata.PreTokens)
		if cb.CompactMetadata.PostTokens > 0 {
			line += " → " + formatTokens(cb.CompactMetadata.PostTokens)
		}
		line += " tokens"
	}
	return ext.Block{Lines: truncLines([]string{st.dim.Render(line)}, rc.Width)}
}

// renderCompactSummary shows the summary a compaction left, at full detail
// only (ctrl+o, verbose).
func (f *Feature) renderCompactSummary(rc ext.RenderCtx, it *ext.Item) ext.Block {
	u, _ := it.Data.(*proto.User)
	if u == nil || !verbose(rc) {
		return ext.Block{}
	}
	st := stylesFor(rc)
	lines := []string{st.dim.Render(glyphThought + " Summary of the earlier conversation")}
	md := render.Markdown(u.Message.Content.PlainText(), f.mdOptions(rc, rc.Width-len(resultHang)))
	return ext.Block{Lines: append(lines, indentLines(md, true)...)}
}

func (f *Feature) renderRetry(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	r, _ := it.Data.(*proto.APIRetry)
	if r == nil {
		return ext.Block{}
	}
	wait := msToDuration(r.RetryDelayMS)
	text := fmt.Sprintf("API error · retrying in %s (attempt %d/%d)", formatDuration(wait), r.Attempt, r.MaxRetries)
	if status := strings.Trim(string(r.ErrorStatus), `"`); status != "" && status != "null" {
		text = fmt.Sprintf("API error %s · retrying in %s (attempt %d/%d)", status, formatDuration(wait), r.Attempt, r.MaxRetries)
	}
	return ext.Block{Lines: result(rc, st.warn, text)}
}

// dialogOnlyNotices identifies engine notices that interactive Claude Code
// shows as a one-time dialog, not as a transcript line; the headless engine
// flattens them into a system/informational message. They carry no tag, so
// they are matched by the documentation link they point to.
var dialogOnlyNotices = []string{
	"code.claude.com/docs/en/auto-mode-classifier-billing", // auto mode classifier billing
}

func dialogOnlyNotice(content string) bool {
	for _, link := range dialogOnlyNotices {
		if strings.Contains(content, link) {
			return true
		}
	}
	return false
}

func (f *Feature) renderInformational(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	in, _ := it.Data.(*proto.Informational)
	if in == nil || strings.TrimSpace(in.Content) == "" {
		return ext.Block{}
	}
	if dialogOnlyNotice(in.Content) && rc.Mode != ext.FullTranscript {
		return ext.Block{} // only the ctrl+o viewer keeps it
	}
	style := st.dim
	switch in.Level {
	case "warning":
		style = st.warn
	case "suggestion":
		style = st.perm
	}
	var lines []string
	for i, l := range strings.Split(strings.TrimSpace(clean(in.Content)), "\n") {
		first := dotIndent
		if i == 0 {
			first = style.Render(glyphDot) + " "
		}
		lines = append(lines, render.WrapWith(style.Render(l), render.WrapOptions{Width: rc.Width, First: first, Rest: dotIndent})...)
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderHook(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	h, _ := it.Data.(*proto.Hook)
	if h == nil {
		return ext.Block{}
	}
	name := clean(h.HookName)
	if name == "" {
		name = clean(h.HookEvent)
	}
	switch it.State {
	case ext.Running, ext.Streaming:
		return ext.Block{Lines: truncLines([]string{st.dim.Render(glyphDot + " Running " + name + " hook…")}, rc.Width)}
	case ext.Failed:
		lines := header(rc, st, st.err, name+" hook failed", "")
		detail := h.Stderr
		if detail == "" {
			detail = h.Output
		}
		if h.ExitCode != nil {
			lines = append(lines, result(rc, st.dim, fmt.Sprintf("exit code %d", *h.ExitCode))...)
		}
		o, hid := output(rc, st.err, detail, h.ExitCode == nil)
		return ext.Block{Lines: append(lines, o...), Collapsible: hid}
	}
	// Successful hooks stay quiet unless the view is verbose.
	if !verbose(rc) {
		return ext.Block{}
	}
	lines := header(rc, st, st.ok, name+" hook", "")
	o, _ := output(rc, st.dim, firstNonEmpty(h.Output, h.Stdout), true)
	return ext.Block{Lines: append(lines, o...)}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func (f *Feature) renderRateLimit(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	ev, _ := it.Data.(*proto.RateLimitEvent)
	if ev == nil {
		return ext.Block{}
	}
	info := ev.RateLimitInfo
	var text string
	style := st.warn
	switch info.Status {
	case "rejected":
		text = "Usage limit reached"
		style = st.err
	default:
		text = "Approaching the usage limit"
		if info.Utilization > 0 {
			u := info.Utilization
			if u <= 1 {
				u *= 100
			}
			text += fmt.Sprintf(" (%.0f%% used)", u)
		}
	}
	if t := resetTime(info.ResetsAt); !t.IsZero() {
		text += " · resets " + f.zoned(t).Format("Jan 2 3:04 PM")
	}
	return ext.Block{Lines: render.WrapWith(style.Render(text), render.WrapOptions{Width: rc.Width, First: style.Render(glyphDot) + " ", Rest: dotIndent})}
}

// resetTime decodes resetsAt as unix seconds or an RFC 3339 string.
func resetTime(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return time.Unix(int64(n), 0)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (f *Feature) renderRefusal(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	m, _ := it.Data.(*proto.ModelRefusal)
	if m == nil {
		return ext.Block{}
	}
	text := "The model declined to respond"
	if m.FallbackModel != "" {
		text += "; retrying with " + clean(m.FallbackModel)
	}
	lines := render.WrapWith(st.warn.Render(text), render.WrapOptions{Width: rc.Width, First: st.warn.Render(glyphDot) + " ", Rest: dotIndent})
	if c := strings.TrimSpace(m.Content); c != "" {
		o, _ := output(rc, st.dim, c, true)
		lines = append(lines, o...)
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderDenied(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	d, _ := it.Data.(*proto.PermissionDenied)
	if d == nil {
		return ext.Block{}
	}
	lines := header(rc, st, st.err, "Denied: "+d.ToolName, "")
	reason := firstNonEmpty(d.Message, d.DecisionReason)
	if reason != "" {
		lines = append(lines, result(rc, st.dim, oneLine(reason))...)
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderToolSummary(rc ext.RenderCtx, it *ext.Item) ext.Block {
	s, _ := it.Data.(*proto.ToolUseSummary)
	if s == nil || strings.TrimSpace(s.Summary) == "" {
		return ext.Block{}
	}
	return ext.Block{Lines: result(rc, stylesFor(rc).dim, oneLine(s.Summary))}
}

func (f *Feature) renderTaskNotification(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	n, _ := it.Data.(*proto.TaskNotification)
	if n == nil {
		return ext.Block{}
	}
	dot, verb := st.ok, "finished"
	switch n.Status {
	case "failed":
		dot, verb = st.err, "failed"
	case "stopped":
		dot, verb = st.dim, "stopped"
	}
	name := "Background task " + verb
	agent := false
	if id := f.store.TaskTool(n.TaskID); id != "" {
		var in agentInput
		if t := f.store.Get(id); t != nil && toolUse(t) != nil {
			decodeInput(toolUse(t).Input, &in)
			name = "Agent \"" + oneLine(in.Description) + "\" " + verb
			agent = true
		}
	}
	if n.Usage != nil && n.Usage.DurationMS > 0 {
		name += " · " + formatDuration(msToDuration(n.Usage.DurationMS))
	}
	lines := header(rc, st, dot, name, "")
	// An agent's answer goes to Claude; the row only says it finished.
	if !agent || verbose(rc) || n.Status == "failed" {
		if s := firstNonEmpty(n.Summary, n.Reason); s != "" {
			lines = append(lines, result(rc, st.dim, oneLine(s))...)
		}
	}
	return ext.Block{Lines: lines}
}

func (f *Feature) renderNotification(rc ext.RenderCtx, it *ext.Item) ext.Block {
	n, _ := it.Data.(*proto.Notification)
	if n == nil || strings.TrimSpace(n.Text) == "" {
		return ext.Block{}
	}
	return ext.Block{Lines: result(rc, stylesFor(rc).dim, oneLine(n.Text))}
}

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

func secondsToDuration(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

func (f *Feature) renderMemoryRecall(rc ext.RenderCtx, it *ext.Item) ext.Block {
	st := stylesFor(rc)
	m, _ := it.Data.(*proto.MemoryRecall)
	if m == nil {
		return ext.Block{}
	}
	var mems []json.RawMessage
	_ = json.Unmarshal(m.Memories, &mems)
	if len(mems) == 0 {
		return ext.Block{}
	}
	lines := render.WrapWith(st.dim.Render("Recalled "+plural(len(mems), "memory", "memories")),
		render.WrapOptions{Width: rc.Width, First: render.Fg(st.p, "remember").Render(glyphDot) + " ", Rest: dotIndent})
	if !verbose(rc) {
		return ext.Block{Lines: lines, Collapsible: true}
	}
	for i, raw := range mems {
		var mem struct {
			Name, Path, Title, Content string
		}
		label := ""
		if json.Unmarshal(raw, &mem) == nil {
			label = firstNonEmpty(mem.Title, mem.Name, mem.Path, mem.Content)
		} else {
			_ = json.Unmarshal(raw, &label)
		}
		first := resultHang
		if i == 0 {
			first = resultIndent
		}
		lines = append(lines, truncLines(render.WrapWith(st.dim.Render(oneLine(label)), render.WrapOptions{Width: rc.Width, First: first, Rest: resultHang}), rc.Width)[0])
	}
	return ext.Block{Lines: lines}
}
