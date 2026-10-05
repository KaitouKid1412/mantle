package dialogs

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PermissionContext is what the host knows beyond the request itself.
type PermissionContext struct {
	// Engine labels a non-main engine ("Builder mod-a3f"); "" for the main session.
	Engine string
	// Agent names the background subagent asking (resolved from agent_id).
	Agent string
	// Cwd shortens paths inside the working directory.
	Cwd string
	// OfferAuto adds "Yes, and switch to auto mode" (auto mode available and not active).
	OfferAuto bool
	// Index and Total show "(1 of 3)" when several requests are queued; Total 0 hides it.
	Index, Total int
	// Diff renders an edit preview (old → new text of path) at width, capped at
	// maxLines; nil uses a plain line diff. Part B plugs in pkg/ui/diffview.
	Diff func(old, new, path string, width, maxLines int) []string
	// Markdown renders markdown (question previews) at width; nil shows it as text.
	Markdown func(src string, width int) []string
}

// Deny messages the model sees. mantle's own wording.
const (
	denyMessage         = "The user denied this tool call."
	denyFeedbackMessage = "The user denied this tool call and said: "
	denyStopMessage     = "The user denied this tool call and stopped the turn."
)

const (
	permStageOptions = iota
	permStageFeedback
)

// Permission is the view-model of a `can_use_tool` prompt for an ordinary tool. Like
// Claude Code's prompt it offers Yes, at most one "Yes, and …" grant built from the
// engine's suggestions, a switch to auto mode for non-file tools, and No. No (and esc)
// deny and end the turn; tab adds instructions to the No, which deny and let Claude go
// on with them.
type Permission struct {
	req   ToolRequest
	ctx   PermissionContext
	input map[string]any
	view  toolView
	row   *suggestionRow

	opts     optionList
	stage    int
	feedback textField

	result *PermissionResult
}

// NewPermission builds the prompt for req.
func NewPermission(req ToolRequest, ctx PermissionContext) *Permission {
	p := &Permission{req: req, ctx: ctx}
	_ = json.Unmarshal(req.Input, &p.input)
	if p.input == nil {
		p.input = map[string]any{}
	}
	p.view = describeTool(req, p.input, ctx)
	p.feedback = textField{placeholder: "and tell Claude what to do differently"}
	p.row = buildSuggestionRow(req, p.view.fileTool, ctx.Cwd)

	items := []option{{id: "yes", label: "Yes"}}
	if p.row != nil {
		items = append(items, option{id: "always", label: p.row.label, hint: p.row.hint})
	}
	if ctx.OfferAuto && !p.view.fileTool {
		items = append(items, option{id: "auto", label: "Yes, and switch to auto mode", hint: "· auto mode answers prompts like this one for you"})
	}
	items = append(items, option{id: "no", label: "No"})
	p.opts = optionList{items: items}
	if req.DefaultToNo {
		p.opts.focus("no")
	}
	return p
}

// Request returns the request this prompt answers.
func (p *Permission) Request() ToolRequest { return p.req }

// Done reports whether the prompt has been answered.
func (p *Permission) Done() bool { return p.result != nil }

// Response is the answer to send; nil until Done.
func (p *Permission) Response() *PermissionResult { return p.result }

// Deny answers the prompt with a plain denial (used when the host must close it, for
// example on exit). It is never an approval.
func (p *Permission) Deny() *PermissionResult {
	if p.result == nil {
		p.finishDeny(denyMessage, false)
	}
	return p.result
}

func (p *Permission) allow(updates []PermissionUpdate, class string) {
	p.result = &PermissionResult{
		Behavior:               "allow",
		UpdatedInput:           p.req.Input,
		UpdatedPermissions:     updates,
		ToolUseID:              p.req.ToolUseID,
		DecisionClassification: class,
	}
}

func (p *Permission) finishDeny(msg string, interrupt bool) {
	p.result = &PermissionResult{
		Behavior:               "deny",
		Message:                msg,
		Interrupt:              interrupt,
		ToolUseID:              p.req.ToolUseID,
		DecisionClassification: ClassUserReject,
	}
}

// deny answers No: with instructions Claude goes on with them, without the turn ends.
func (p *Permission) deny() (bool, Effect) {
	if fb := strings.TrimSpace(p.feedback.Value()); fb != "" {
		p.finishDeny(denyFeedbackMessage+fb, false)
	} else {
		p.finishDeny(denyStopMessage, true)
	}
	return true, Answered
}

func (p *Permission) choose(id string) (bool, Effect) {
	switch id {
	case "yes":
		p.allow(nil, ClassUserTemporary)
	case "always":
		class := ClassUserTemporary
		for _, s := range p.row.updates {
			if s.Destination != "session" {
				class = ClassUserPermanent
			}
		}
		p.allow(append([]PermissionUpdate(nil), p.row.updates...), class)
	case "auto":
		p.allow([]PermissionUpdate{SetModeUpdate("auto", "session")}, ClassUserTemporary)
	case "no":
		return p.deny()
	default:
		return false, None
	}
	return true, Answered
}

// openFeedback focuses No and opens its instructions field (tab).
func (p *Permission) openFeedback() {
	p.opts.focus("no")
	p.stage = permStageFeedback
}

// HandleKey implements Model.
func (p *Permission) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if p.Done() {
		return false, None
	}
	if p.stage == permStageFeedback {
		switch k.Keystroke() {
		case "enter":
			return p.deny()
		case "esc", "tab":
			p.stage = permStageOptions
			return true, None
		}
		return p.feedback.HandleKey(k), None
	}
	a, digit := keyAct(k)
	if digit > 0 {
		if !p.opts.pick(digit) {
			return true, None
		}
		return p.choose(p.opts.selected().id)
	}
	return p.do(a)
}

func (p *Permission) do(a act) (bool, Effect) {
	switch a {
	case actUp:
		p.opts.move(-1)
	case actDown:
		p.opts.move(1)
	case actYes:
		return p.choose(p.opts.selected().id)
	case actNo:
		// Esc ends the turn, as in Claude Code: deny and interrupt.
		p.finishDeny(denyStopMessage, true)
		return true, Answered
	case actNextField:
		p.openFeedback()
	case actCycleMode:
		if p.row != nil && p.row.session {
			return p.choose("always")
		}
		return true, CycleMode
	default:
		return false, None
	}
	return true, None
}

// Action implements Model.
func (p *Permission) Action(id string) (bool, Effect) {
	if p.Done() {
		return false, None
	}
	if p.stage != permStageOptions {
		switch actionAct(id) {
		case actYes:
			return p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		case actNo, actNextField:
			p.stage = permStageOptions
			return true, None
		}
		return false, None
	}
	return p.do(actionAct(id))
}

// HandlePaste implements Model.
func (p *Permission) HandlePaste(s string) (bool, Effect) {
	if p.stage != permStageFeedback {
		return false, None
	}
	p.feedback.insert(s)
	return true, None
}

// SetPosition implements Positioner.
func (p *Permission) SetPosition(index, total int) { p.ctx.Index, p.ctx.Total = index, total }

// Title is the frame title, with the queue counter.
func (p *Permission) Title() string { return p.view.title + counter(p.ctx.Index, p.ctx.Total) }

// attribution names who is asking when it isn't the main session's own agent.
func attribution(engine, agent string) string {
	switch {
	case engine != "" && agent != "":
		return SanitizeLine(engine) + " · subagent " + SanitizeLine(agent) + " is asking"
	case engine != "":
		return SanitizeLine(engine) + " is asking"
	case agent != "":
		return "Subagent " + SanitizeLine(agent) + " is asking"
	}
	return ""
}

// View implements Model: the title, a tip, the header, the command or diff between
// dashed rules, the question, the options and the key hints.
func (p *Permission) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	if a := attribution(p.ctx.Engine, p.ctx.Agent); a != "" {
		body = append(body, styleLines(st.Accent, wrap(a, w))...)
	}
	if p.ctx.OfferAuto && !p.view.fileTool {
		body = append(body, styleLines(st.Dim, wrap("Tip: auto mode can answer prompts like this one for you; pick \"switch to auto mode\" below", w))...)
	}
	if p.view.header != nil {
		body = append(body, p.view.header(w, st)...)
	}
	if p.view.block != nil {
		body = append(body, dashRule(width, st))
		body = append(body, p.view.block(w, st)...)
		body = append(body, dashRule(width, st))
	}
	if p.req.DecisionReason != "" {
		body = append(body, styleLines(st.Dim, wrap("Why: "+SanitizeLine(string(p.req.DecisionReason)), w))...)
	}
	if r := p.req.MatchedAskRule; r != nil {
		rule := RuleString(PermissionRule{ToolName: r.ToolName, RuleContent: r.RuleContent})
		body = append(body, styleLines(st.Dim, wrap("Matches ask rule "+SanitizeLine(rule)+" ("+SanitizeLine(r.Source)+")", w))...)
	}
	question := p.view.question
	if p.req.Title != "" {
		question = SanitizeLine(p.req.Title)
	}
	body = append(body, styleLines(st.Text, wrap(question, w))...)
	body = append(body, p.opts.lines(w, st, true)...)
	if p.stage == permStageFeedback {
		body = append(body, p.feedback.lines(w, st, true, "     ")...)
	}
	body = append(body, "")
	if p.stage == permStageFeedback {
		body = append(body, render(st.Dim, "Enter to send · Esc to go back"))
	} else {
		body = append(body, render(st.Dim, "Esc to cancel · Tab to amend"))
	}
	return frame(p.Title(), body, width, p.view.kind, st)
}
