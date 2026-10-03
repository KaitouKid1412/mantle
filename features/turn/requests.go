package turn

import (
	"encoding/json"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/features/turn/mode"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// request is one CLI-originated prompt waiting for the user: a can_use_tool request
// (permission, AskUserQuestion, ExitPlanMode) or an elicitation.
type request struct {
	engineID, requestID string
	dialogID            string
	perm                *ext.PermissionMsg
	ctrl                *ext.ControlRequestMsg
	vm                  dialogs.Model
}

// requestQueue shows one prompt at a time, in arrival order, across all engines.
type requestQueue struct {
	waiting []*request
	open    *request
}

// advanceMsg opens the next queued prompt after the current one closed.
type advanceMsg struct{}

func (st *state) setupRequests(r ext.Registrar) {
	for _, id := range []string{DialogPermission, DialogAskUserQuestion, DialogPlanApproval, DialogElicitation} {
		r.AddDialog(id, st.requestDialog(id))
	}
	ext.Subscribe(r, "turn.permission", st.onPermission)
	ext.Subscribe(r, "turn.controlRequest", st.onControlRequest)
	ext.Subscribe(r, "turn.controlCancel", st.onCancel)
	ext.Subscribe(r, "turn.advance", func(c ext.Ctx, _ advanceMsg) tea.Cmd { return st.openNext(c) })
}

func (st *state) onPermission(c ext.Ctx, m ext.PermissionMsg) tea.Cmd {
	req := &request{engineID: engineKey(m.EngineID), requestID: m.RequestID, perm: &m}
	switch m.Req.ToolName {
	case proto.ToolAskUserQuestion:
		req.dialogID = DialogAskUserQuestion
	case proto.ToolExitPlanMode:
		req.dialogID = DialogPlanApproval
	default:
		req.dialogID = DialogPermission
	}
	return st.enqueue(c, req)
}

func (st *state) onControlRequest(c ext.Ctx, m ext.ControlRequestMsg) tea.Cmd {
	switch m.Subtype {
	case proto.SubElicitation:
		return st.enqueue(c, &request{engineID: engineKey(m.EngineID), requestID: m.RequestID, ctrl: &m, dialogID: DialogElicitation})
	case proto.SubRequestUserDialog:
		// mantle declares no supportedDialogKinds, so this only arrives from a confused
		// engine; refuse it instead of leaving the engine waiting.
		if m.Reply != nil {
			return m.Reply(nil, errors.New("mantle does not support this dialog kind"))
		}
	}
	return nil
}

func (st *state) enqueue(c ext.Ctx, r *request) tea.Cmd {
	st.reqs.waiting = append(st.reqs.waiting, r)
	if st.reqs.open == nil {
		return st.openNext(c)
	}
	st.refreshPosition(c)
	return nil
}

func (st *state) refreshPosition(c ext.Ctx) {
	o := st.reqs.open
	if o == nil {
		return
	}
	if p, ok := o.vm.(dialogs.Positioner); ok {
		p.SetPosition(1, 1+len(st.reqs.waiting))
		c.Invalidate(o.dialogID)
	}
}

// openNext shows the oldest waiting prompt, if none is open.
func (st *state) openNext(c ext.Ctx) tea.Cmd {
	if st.reqs.open != nil || len(st.reqs.waiting) == 0 {
		return nil
	}
	r := st.reqs.waiting[0]
	st.reqs.waiting = st.reqs.waiting[1:]
	if err := st.buildVM(c, r); err != nil {
		// A malformed AskUserQuestion: refuse it so the turn can go on.
		return tea.Sequence(st.replyDeny(r, "mantle could not show this question: "+err.Error(), false), ext.Msg(advanceMsg{}))
	}
	st.reqs.open = r
	st.refreshPosition(c)
	return c.OpenDialog(r.dialogID, r)
}

func (st *state) permContext(c ext.Ctx, r *request) dialogs.PermissionContext {
	pc := dialogs.PermissionContext{
		Engine: engineLabel(r.engineID),
		Cwd:    st.session(c, r.engineID).Cwd,
	}
	pc.Diff, pc.Markdown = renderHooks(c)
	if r.perm != nil {
		pc.Agent = st.agentLabel(r.perm.Req.AgentID)
		avail := st.availability(c, r.engineID)
		cur := mode.Normalize(mode.Mode(st.currentMode(c, r.engineID)))
		pc.OfferAuto = avail.Auto && cur != mode.Auto && !r.perm.Req.DefaultToNo
	}
	return pc
}

func (st *state) buildVM(c ext.Ctx, r *request) error {
	pc := st.permContext(c, r)
	switch r.dialogID {
	case DialogElicitation:
		var req dialogs.ElicitationRequest
		if err := json.Unmarshal(r.ctrl.Request, &req); err != nil {
			return err
		}
		r.vm = dialogs.NewElicitation(req, pc)
		return nil
	}
	tr := toToolRequest(r.perm.Req)
	switch r.dialogID {
	case DialogAskUserQuestion:
		vm, err := dialogs.NewAskQuestion(tr, pc)
		if err != nil {
			return err
		}
		r.vm = vm
	case DialogPlanApproval:
		avail := st.availability(c, r.engineID)
		r.vm = dialogs.NewPlanApproval(tr, dialogs.PlanContext{PermissionContext: pc, AutoAvailable: avail.Auto, BypassAvailable: avail.Bypass, RenderPlan: pc.Markdown})
	default:
		r.vm = dialogs.NewPermission(tr, pc)
	}
	return nil
}

// requestDialog is the factory for the prompt dialogs. args is the *request the queue
// opened; mods replacing a dialog get the same value.
func (st *state) requestDialog(id string) ext.DialogFactory {
	return func(c ext.Ctx, args any) (ext.Dialog, error) {
		r, ok := args.(*request)
		if !ok || r.vm == nil {
			return nil, errors.New(id + ": opened without a pending request")
		}
		contexts := []string{ext.ContextConfirmation}
		if id == DialogAskUserQuestion {
			contexts = []string{ext.ContextTabs, ext.ContextConfirmation}
		}
		d := &vmDialog{id: id, vm: r.vm, contexts: contexts, place: ext.PlaceInline}
		d.onDone = func(c ext.Ctx) tea.Cmd { return st.finish(c, r) }
		d.onCycle = func(c ext.Ctx) tea.Cmd { return st.cycleMode(c, r.engineID) }
		d.onInterrupt = func(c ext.Ctx) tea.Cmd { return st.interruptRequest(c, r) }
		d.result = func() any { return st.responseOf(r) }
		return d, nil
	}
}

type permResponder interface {
	Response() *dialogs.PermissionResult
}

func (st *state) responseOf(r *request) any {
	switch vm := r.vm.(type) {
	case permResponder:
		if res := vm.Response(); res != nil {
			return toProtoResult(res)
		}
	case *dialogs.Elicitation:
		if res := vm.Response(); res != nil {
			return *res
		}
	}
	return nil
}

// finish sends the open prompt's answer, closes its dialog and opens the next one.
func (st *state) finish(c ext.Ctx, r *request) tea.Cmd {
	var reply tea.Cmd
	switch vm := r.vm.(type) {
	case permResponder:
		if res := vm.Response(); res != nil && r.perm != nil && r.perm.Reply != nil {
			reply = r.perm.Reply(toProtoResult(res))
		}
	case *dialogs.Elicitation:
		if res := vm.Response(); res != nil && r.ctrl != nil && r.ctrl.Reply != nil {
			reply = r.ctrl.Reply(res, nil)
		}
	}
	return st.close(c, r, reply)
}

func (st *state) close(c ext.Ctx, r *request, reply tea.Cmd) tea.Cmd {
	if st.reqs.open == r {
		st.reqs.open = nil
	}
	return tea.Sequence(reply, c.CloseDialog(r.dialogID), ext.Msg(advanceMsg{}))
}

// interruptRequest answers the open prompt with "no, and stop" (ctrl+c in a dialog).
func (st *state) interruptRequest(c ext.Ctx, r *request) tea.Cmd {
	if r.ctrl != nil {
		var reply tea.Cmd
		if r.ctrl.Reply != nil {
			reply = r.ctrl.Reply(dialogs.ElicitationResult{Action: "cancel"}, nil)
		}
		return st.close(c, r, reply)
	}
	return st.close(c, r, st.replyDeny(r, "The user interrupted the turn.", true))
}

func (st *state) replyDeny(r *request, msg string, interrupt bool) tea.Cmd {
	if r.perm == nil || r.perm.Reply == nil {
		if r.ctrl != nil && r.ctrl.Reply != nil {
			return r.ctrl.Reply(dialogs.ElicitationResult{Action: "cancel"}, nil)
		}
		return nil
	}
	return r.perm.Reply(r.perm.Req.Deny(msg, interrupt))
}

// onCancel closes the dialog of a withdrawn request without replying.
func (st *state) onCancel(c ext.Ctx, m ext.ControlCancelMsg) tea.Cmd {
	eng := engineKey(m.EngineID)
	if o := st.reqs.open; o != nil && o.engineID == eng && o.requestID == m.RequestID {
		return st.close(c, o, nil)
	}
	st.removeWaiting(func(r *request) bool { return r.engineID == eng && r.requestID == m.RequestID })
	st.refreshPosition(c)
	return nil
}

func (st *state) removeWaiting(drop func(*request) bool) {
	kept := st.reqs.waiting[:0]
	for _, r := range st.reqs.waiting {
		if !drop(r) {
			kept = append(kept, r)
		}
	}
	st.reqs.waiting = kept
}

// dropRequests forgets every prompt of an engine that went away; nobody can answer them.
func (st *state) dropRequests(c ext.Ctx, engineID string) tea.Cmd {
	st.removeWaiting(func(r *request) bool { return r.engineID == engineID })
	if o := st.reqs.open; o != nil && o.engineID == engineID {
		return st.close(c, o, nil)
	}
	st.refreshPosition(c)
	return nil
}

// engineLabel names a non-main engine for attribution ("Builder mod-a3f").
func engineLabel(id string) string {
	switch {
	case id == "" || id == ext.MainEngine:
		return ""
	case strings.HasPrefix(id, "builder"):
		rest := strings.TrimLeft(strings.TrimPrefix(id, "builder"), ":-/ ")
		if rest == "" {
			return "Builder"
		}
		return "Builder " + rest
	}
	return id
}

// agentLabel names the background subagent behind an agent_id.
func (st *state) agentLabel(agentID string) string {
	if agentID == "" {
		return ""
	}
	if l, ok := st.agents[agentID]; ok && l != "" {
		return l
	}
	return agentID
}

// toToolRequest converts the engine's request to the view-model's wire type.
func toToolRequest(c proto.CanUseTool) dialogs.ToolRequest {
	var tr dialogs.ToolRequest
	if b, err := json.Marshal(c); err == nil {
		_ = json.Unmarshal(b, &tr)
	}
	if tr.Input == nil {
		tr.Input = json.RawMessage("{}")
	}
	return tr
}

// toProtoResult converts a view-model answer to the engine's type.
func toProtoResult(r *dialogs.PermissionResult) proto.PermissionResult {
	out := proto.PermissionResult{
		Behavior:               r.Behavior,
		UpdatedInput:           r.UpdatedInput,
		Message:                r.Message,
		Interrupt:              r.Interrupt,
		ToolUseID:              r.ToolUseID,
		DecisionClassification: r.DecisionClassification,
	}
	for _, u := range r.UpdatedPermissions {
		pu := proto.PermissionUpdate{
			Type: u.Type, Behavior: u.Behavior, Mode: u.Mode,
			Directories: u.Directories, Destination: u.Destination,
		}
		for _, rule := range u.Rules {
			pu.Rules = append(pu.Rules, proto.PermissionRule{ToolName: rule.ToolName, RuleContent: rule.RuleContent})
		}
		out.UpdatedPermissions = append(out.UpdatedPermissions, pu)
	}
	if out.Behavior == proto.BehaviorDeny && out.Message == "" {
		out.Message = "The user denied this tool call."
	}
	return out
}
