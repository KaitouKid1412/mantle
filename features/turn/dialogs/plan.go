package dialogs

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PlanContext adds mode availability to PermissionContext for plan approval.
type PlanContext struct {
	PermissionContext
	AutoAvailable   bool
	BypassAvailable bool
	// PlanLines is how many plan lines are visible at once (0 = 20).
	PlanLines int
	// RenderPlan renders the plan markdown at a width; nil shows the text as-is. Part B
	// plugs in pkg/render.
	RenderPlan func(markdown string, width int) []string
}

const (
	planKeepMessage     = "The user rejected the plan and wants to keep planning."
	planFeedbackMessage = "The user rejected the plan and wants to keep planning. Their feedback: "
)

// PlanApproval is the view-model of an ExitPlanMode prompt.
type PlanApproval struct {
	req    ToolRequest
	ctx    PlanContext
	input  map[string]json.RawMessage
	plan   string
	edited bool
	scroll int

	opts     optionList
	feedback textField
	keep     bool // feedback field open
	result   *PermissionResult
}

// NewPlanApproval builds the prompt for an ExitPlanMode request.
func NewPlanApproval(req ToolRequest, ctx PlanContext) *PlanApproval {
	p := &PlanApproval{req: req, ctx: ctx}
	_ = json.Unmarshal(req.Input, &p.input)
	if p.input == nil {
		p.input = map[string]json.RawMessage{}
	}
	_ = json.Unmarshal(p.input["plan"], &p.plan)
	var items []option
	if ctx.AutoAvailable {
		items = append(items, option{id: "auto", label: "Yes, and use auto mode"})
	}
	items = append(items, option{id: "acceptEdits", label: "Yes, and auto-accept edits"})
	if ctx.BypassAvailable {
		items = append(items, option{id: "bypassPermissions", label: "Yes, and bypass permissions"})
	}
	items = append(items,
		option{id: "default", label: "Yes, and manually approve edits"},
		option{id: "keep", label: "No, keep planning"},
	)
	p.opts = optionList{items: items}
	p.feedback = textField{placeholder: "Tell Claude what to change", multiline: true}
	return p
}

// Done reports whether the prompt has been answered.
func (p *PlanApproval) Done() bool { return p.result != nil }

// Response is the answer to send; nil until Done.
func (p *PlanApproval) Response() *PermissionResult { return p.result }

// Plan returns the (possibly edited) plan text.
func (p *PlanApproval) Plan() string { return p.plan }

// PlanFilePath is the plan file the engine wrote, if any.
func (p *PlanApproval) PlanFilePath() string {
	var s string
	_ = json.Unmarshal(p.input["planFilePath"], &s)
	return s
}

// EditText is the text to open in $EDITOR for ctrl+g.
func (p *PlanApproval) EditText() string { return p.plan }

// SetEditedText replaces the plan after an external edit.
func (p *PlanApproval) SetEditedText(s string) {
	if s != p.plan {
		p.plan = s
		p.edited = true
		p.scroll = 0
	}
}

// Deny answers "keep planning" without feedback.
func (p *PlanApproval) Deny() *PermissionResult {
	if p.result == nil {
		p.deny(planKeepMessage)
	}
	return p.result
}

func (p *PlanApproval) deny(msg string) {
	p.result = &PermissionResult{Behavior: "deny", Message: msg, ToolUseID: p.req.ToolUseID, DecisionClassification: ClassUserReject}
}

func (p *PlanApproval) approve(mode string) (bool, Effect) {
	input := p.req.Input
	if p.edited {
		in := map[string]json.RawMessage{}
		for k, v := range p.input {
			in[k] = v
		}
		in["plan"], _ = json.Marshal(p.plan)
		input, _ = json.Marshal(in)
	}
	p.result = &PermissionResult{
		Behavior:               "allow",
		UpdatedInput:           input,
		UpdatedPermissions:     []PermissionUpdate{SetModeUpdate(mode, "session")},
		ToolUseID:              p.req.ToolUseID,
		DecisionClassification: ClassUserTemporary,
	}
	return true, Answered
}

func (p *PlanApproval) choose(id string) (bool, Effect) {
	if id == "keep" {
		p.keep = true
		return true, None
	}
	return p.approve(id)
}

func (p *PlanApproval) visible() int {
	if p.ctx.PlanLines > 0 {
		return p.ctx.PlanLines
	}
	return 20
}

// HandleKey implements Model.
func (p *PlanApproval) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if p.Done() {
		return false, None
	}
	if p.keep {
		switch k.Keystroke() {
		case "enter":
			msg := planKeepMessage
			if fb := strings.TrimSpace(p.feedback.Value()); fb != "" {
				msg = planFeedbackMessage + fb
			}
			p.deny(msg)
			return true, Answered
		case "esc":
			p.keep = false
			return true, None
		}
		return p.feedback.HandleKey(k), None
	}
	a, digit := keyAct(k)
	if digit > 0 {
		if p.opts.pick(digit) {
			return p.choose(p.opts.selected().id)
		}
		return true, None
	}
	return p.do(a)
}

func (p *PlanApproval) do(a act) (bool, Effect) {
	switch a {
	case actUp:
		p.opts.move(-1)
	case actDown:
		p.opts.move(1)
	case actYes:
		return p.choose(p.opts.selected().id)
	case actNo:
		p.deny(planKeepMessage)
		return true, Answered
	case actEdit:
		return true, EditExternal
	case actPageDown:
		p.scroll += p.visible() - 1
	case actPageUp:
		p.scroll = max(0, p.scroll-(p.visible()-1))
	case actCycleMode:
		// shift+tab picks the first "Yes" option, like cycling out of plan mode.
		return p.choose(p.opts.items[0].id)
	default:
		return false, None
	}
	return true, None
}

// Action implements Model.
func (p *PlanApproval) Action(id string) (bool, Effect) {
	if p.Done() {
		return false, None
	}
	if p.keep {
		switch actionAct(id) {
		case actYes:
			return p.HandleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		case actNo:
			p.keep = false
			return true, None
		}
		return false, None
	}
	return p.do(actionAct(id))
}

// HandlePaste implements Model.
func (p *PlanApproval) HandlePaste(s string) (bool, Effect) {
	if !p.keep {
		return false, None
	}
	p.feedback.insert(s)
	return true, None
}

// View implements Model.
func (p *PlanApproval) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	if a := attribution(p.ctx.Engine, p.ctx.Agent); a != "" {
		body = append(body, styleLines(st.Accent, wrap(a, w))...)
	}
	body = append(body, styleLines(st.Text, wrap("Here is Claude's plan:", w))...)
	text := strings.TrimRight(Sanitize(p.plan), "\n ")
	var planLines []string
	if p.ctx.RenderPlan != nil {
		planLines = p.ctx.RenderPlan(text, w-2)
	} else {
		planLines = wrap(text, w-2)
	}
	vis := p.visible()
	maxScroll := max(0, len(planLines)-vis)
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	end := min(len(planLines), p.scroll+vis)
	if p.scroll > 0 {
		body = append(body, render(st.Dim, "  ↑ "+itoa(p.scroll)+" more lines (pgup)"))
	}
	for _, l := range planLines[p.scroll:end] {
		body = append(body, "  "+l)
	}
	if end < len(planLines) {
		body = append(body, render(st.Dim, "  ↓ "+itoa(len(planLines)-end)+" more lines (pgdn)"))
	}
	if p.edited {
		body = append(body, render(st.Dim, "  (edited)"))
	}
	body = append(body, "")
	body = append(body, styleLines(st.Text, wrap("Would you like to proceed?", w))...)
	body = append(body, p.opts.lines(w, st, !p.keep)...)
	if p.keep {
		body = append(body, "")
		body = append(body, p.feedback.lines(w, st, true, "> ")...)
		body = append(body, render(st.Dim, "enter to send · esc to go back"))
	} else {
		body = append(body, styleLines(st.Dim, wrap("ctrl+g to edit the plan · esc to keep planning", w))...)
	}
	title := "Ready to code?"
	if p.ctx.Total > 1 {
		title += " (" + itoa(p.ctx.Index) + " of " + itoa(p.ctx.Total) + ")"
	}
	return frame(title, body, width, "planMode", st)
}
