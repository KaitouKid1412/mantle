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
	permStageAmend
)

// Permission is the view-model of a `can_use_tool` prompt for an ordinary tool.
type Permission struct {
	req   ToolRequest
	ctx   PermissionContext
	input map[string]any
	view  toolView

	opts     optionList
	setModes []PermissionUpdate // setMode suggestions, one option each
	always   []PermissionUpdate // other suggestions, offered together

	stage    int
	feedback textField
	amend    textField
	amendErr string

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
	p.feedback = textField{placeholder: "Tell Claude what to do instead", multiline: true}
	p.amend = textField{multiline: true}

	if !req.SuppressAlwaysAllowRule {
		for _, s := range req.PermissionSuggestions {
			if s.Type == "setMode" {
				p.setModes = append(p.setModes, s)
			} else {
				p.always = append(p.always, s)
			}
		}
	}
	items := []option{{id: "yes", label: "Yes"}}
	if len(p.always) > 0 {
		parts := make([]string, len(p.always))
		for i, s := range p.always {
			parts[i] = suggestionPhrase(s, ctx.Cwd)
		}
		items = append(items, option{id: "always", label: "Yes, and " + strings.Join(parts, "; ")})
	}
	for i, s := range p.setModes {
		o := option{id: "mode:" + itoa(i), label: "Yes, and " + suggestionPhrase(s, ctx.Cwd)}
		if i == 0 {
			o.hint = "(shift+tab)"
		}
		items = append(items, o)
	}
	if ctx.OfferAuto {
		items = append(items, option{id: "auto", label: "Yes, and switch to auto mode"})
	}
	items = append(items,
		option{id: "no", label: "No, and tell Claude what to do differently"},
		option{id: "stop", label: "No, and stop"},
	)
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

func (p *Permission) allow(input json.RawMessage, updates []PermissionUpdate, class string) {
	if input == nil {
		input = p.req.Input
	}
	p.result = &PermissionResult{
		Behavior:               "allow",
		UpdatedInput:           input,
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

func (p *Permission) choose(id string) (bool, Effect) {
	switch {
	case id == "yes":
		p.allow(nil, nil, ClassUserTemporary)
	case id == "always":
		class := ClassUserTemporary
		for _, s := range p.always {
			if s.Destination != "session" {
				class = ClassUserPermanent
			}
		}
		p.allow(nil, append([]PermissionUpdate(nil), p.always...), class)
	case strings.HasPrefix(id, "mode:"):
		i := 0
		for _, c := range strings.TrimPrefix(id, "mode:") {
			i = i*10 + int(c-'0')
		}
		p.allow(nil, []PermissionUpdate{p.setModes[i]}, ClassUserTemporary)
	case id == "auto":
		p.allow(nil, []PermissionUpdate{SetModeUpdate("auto", "session")}, ClassUserTemporary)
	case id == "no":
		p.stage = permStageFeedback
		return true, None
	case id == "stop":
		p.finishDeny(denyStopMessage, true)
	default:
		return false, None
	}
	return true, Answered
}

// amendText is what Tab puts in the amend field: the command for shell tools, the input
// JSON for everything else.
func (p *Permission) amendText() string {
	if p.req.ToolName == "Bash" || p.req.ToolName == "PowerShell" {
		if c, ok := p.input["command"].(string); ok {
			return c
		}
	}
	return prettyJSON(p.req.Input)
}

func (p *Permission) submitAmend() (bool, Effect) {
	text := p.amend.Value()
	var updated json.RawMessage
	if p.req.ToolName == "Bash" || p.req.ToolName == "PowerShell" {
		if strings.TrimSpace(text) == "" {
			p.amendErr = "The command can't be empty."
			return true, None
		}
		in := map[string]any{}
		for k, v := range p.input {
			in[k] = v
		}
		in["command"] = text
		updated, _ = json.Marshal(in)
	} else {
		var v map[string]any
		if err := json.Unmarshal([]byte(text), &v); err != nil || v == nil {
			p.amendErr = "Not a JSON object: fix it or press esc to go back."
			return true, None
		}
		updated, _ = json.Marshal(v)
	}
	p.allow(updated, nil, ClassUserTemporary)
	return true, Answered
}

// HandleKey implements Model.
func (p *Permission) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if p.Done() {
		return false, None
	}
	switch p.stage {
	case permStageFeedback:
		switch k.Keystroke() {
		case "enter":
			msg := denyMessage
			if fb := strings.TrimSpace(p.feedback.Value()); fb != "" {
				msg = denyFeedbackMessage + fb
			}
			p.finishDeny(msg, false)
			return true, Answered
		case "esc":
			p.stage = permStageOptions
			return true, None
		}
		return p.feedback.HandleKey(k), None
	case permStageAmend:
		switch k.Keystroke() {
		case "enter":
			return p.submitAmend()
		case "esc":
			p.stage, p.amendErr = permStageOptions, ""
			return true, None
		}
		if p.amend.HandleKey(k) {
			p.amendErr = ""
			return true, None
		}
		return false, None
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
		p.finishDeny(denyMessage, false)
		return true, Answered
	case actNextField:
		p.stage = permStageAmend
		p.amend.SetValue(p.amendText())
	case actCycleMode:
		if len(p.setModes) > 0 {
			return p.choose("mode:0")
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
		case actNo:
			return p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		}
		return false, None
	}
	return p.do(actionAct(id))
}

// HandlePaste implements Model.
func (p *Permission) HandlePaste(s string) (bool, Effect) {
	switch p.stage {
	case permStageFeedback:
		p.feedback.insert(s)
	case permStageAmend:
		p.amend.insert(s)
		p.amendErr = ""
	default:
		return false, None
	}
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

// View implements Model.
func (p *Permission) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	if a := attribution(p.ctx.Engine, p.ctx.Agent); a != "" {
		body = append(body, styleLines(st.Accent, wrap(a, w))...)
	}
	body = append(body, p.view.body(w, st)...)
	if p.req.BlockedPath != "" {
		body = append(body, styleLines(st.Warning, wrapIndent("  ", "Outside the allowed directories: "+displayPath(p.req.BlockedPath, ""), w))...)
	}
	if p.req.DecisionReason != "" {
		body = append(body, styleLines(st.Dim, wrapIndent("  ", "Why: "+SanitizeLine(string(p.req.DecisionReason)), w))...)
	}
	if r := p.req.MatchedAskRule; r != nil {
		rule := RuleString(PermissionRule{ToolName: r.ToolName, RuleContent: r.RuleContent})
		body = append(body, styleLines(st.Dim, wrapIndent("  ", "Matches ask rule "+SanitizeLine(rule)+" ("+SanitizeLine(r.Source)+")", w))...)
	}
	body = append(body, "")
	question := p.view.question
	if p.req.Title != "" {
		question = SanitizeLine(p.req.Title)
	}
	body = append(body, styleLines(st.Text, wrap(question, w))...)
	body = append(body, p.opts.lines(w, st, p.stage == permStageOptions)...)

	switch p.stage {
	case permStageFeedback:
		body = append(body, "")
		body = append(body, p.feedback.lines(w, st, true, "> ")...)
		body = append(body, render(st.Dim, "enter to send · esc to go back"))
	case permStageAmend:
		body = append(body, "")
		body = append(body, render(st.Dim, "Amend the input, then press enter to run it:"))
		body = append(body, styleLines(st.Code, p.amend.lines(w, st, true, "> "))...)
		if p.amendErr != "" {
			body = append(body, styleLines(st.Error, wrap(p.amendErr, w))...)
		}
		body = append(body, render(st.Dim, "enter to run · esc to go back"))
	default:
		body = append(body, render(st.Dim, "esc to deny · tab to amend · shift+tab to change mode"))
	}
	return frame(p.Title(), body, width, p.view.kind, st)
}
