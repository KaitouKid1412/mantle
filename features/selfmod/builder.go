package selfmod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// build is one /mantle request as the UI tracks it. Only the UI goroutine
// touches it; Cmds get copies of the request and hand back updated copies.
type build struct {
	ws    *sm.Workspace
	req   *sm.Request
	phase string

	model, rules string
	budget       float64

	lastTool  string
	reply     string
	cost      float64
	sessionID string
	start     time.Time
	end       time.Time
	err       string
	detail    []string

	report   *sm.Report
	version  string
	proposal *sm.ConfigProposal
	diffs    []StoryDiff

	prompt   string // sent once the builder engine attaches
	engineUp bool
	cancel   context.CancelFunc
	update   *updateState
}

func (b *build) engineID() string { return sm.BuilderEngineID(b.req.ID) }

// modID is the Mantle-Mod id the request's commits carry.
func (b *build) modID() string {
	if b.req.Kind == sm.RequestEdit && b.req.Target != "" {
		return b.req.Target
	}
	return b.req.ID
}

func (b *build) view() *BuildView {
	return &BuildView{
		ID: b.req.ID, Kind: b.req.Kind, Request: b.req.Request, Phase: b.phase,
		Round: b.req.Round, Rounds: sm.MaxRounds, LastTool: b.lastTool,
		CostUSD: b.cost, Budget: b.budget, Start: b.start, End: b.end,
		Err: b.err, Detail: b.detail, BuildID: b.version,
	}
}

func (b *build) item(now time.Time) *ext.Item {
	state := ext.Running
	switch b.phase {
	case PhaseDone, PhaseConfig, PhaseReady:
		state = ext.Done
	case PhaseFailed:
		state = ext.Failed
	}
	return &ext.Item{
		ID: "mantle.build/" + b.req.ID, EngineID: ext.MainEngine, Key: KeyBuild,
		Data: b.view(), State: state, Start: b.start, End: b.end,
	}
}

// settings

func (c *controller) budget(ctx ext.Ctx) float64 {
	switch v := ctx.Settings().Mantle(SettingMaxBudget).(type) {
	case float64:
		if v > 0 {
			return v
		}
	case int:
		if v > 0 {
			return float64(v)
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimPrefix(v, "$"), 64); err == nil && f > 0 {
			return f
		}
	}
	return sm.DefaultMaxBudgetUSD
}

func (c *controller) model(ctx ext.Ctx) string {
	if m, _ := ctx.Settings().Mantle(SettingModel).(string); strings.TrimSpace(m) != "" {
		return strings.TrimSpace(m)
	}
	return ctx.Session().Model
}

func (c *controller) confirmMode(ctx ext.Ctx) string {
	switch v, _ := ctx.Settings().Mantle(SettingConfirm).(string); v {
	case ConfirmAlways, ConfirmNever:
		return v
	}
	return ConfirmOnVisualChange
}

// starting

type startedMsg struct {
	b      *build
	retry  string // failure text when retrying
	err    error
	action string
}

// begin creates the request's worktree in a Cmd (git is slow), then starts
// its build on the UI goroutine.
func (c *controller) begin(ctx ext.Ctx, action string, create func(ws *sm.Workspace) (*sm.Request, error)) tea.Cmd {
	ws := c.workspace(ctx)
	budget, model := c.budget(ctx), c.model(ctx)
	return func() tea.Msg {
		r, err := create(ws)
		if err != nil {
			return startedMsg{err: err, action: action}
		}
		b := &build{ws: ws, req: r, budget: budget, model: model}
		if r.Kind != sm.RequestUndo {
			if b.rules, err = sm.WriteRules(ws.Layout.Build(r.ID)); err != nil {
				return startedMsg{err: err, action: action}
			}
		}
		return startedMsg{b: b}
	}
}

func (c *controller) onStarted(ctx ext.Ctx, m startedMsg) tea.Cmd {
	if m.err != nil {
		return c.notify(ctx, ext.NoticeError, "start", fmt.Sprintf("/mantle %s: %v", m.action, m.err))
	}
	b := m.b
	b.start = ctx.Clock().Now()
	c.add(b)
	switch b.req.Kind {
	case sm.RequestUndo:
		b.phase = PhaseVetting
		return tea.Batch(c.changed(ctx), c.vet(ctx, b))
	case sm.RequestUpdate:
		b.phase = PhaseUpdating
		b.update = &updateState{}
		b.detail = []string{fmt.Sprintf("%d commit(s) to re-apply", len(b.req.Pending))}
		return tea.Batch(c.changed(ctx), c.updateStep(ctx, b))
	}
	b.phase = PhaseStarting
	b.prompt = sm.BuildPrompt(b.req)
	if m.retry != "" {
		b.prompt += "\nA previous attempt in this worktree failed the checks:\n\n" + m.retry +
			"\nContinue from the worktree as it is now and fix these problems.\n"
	}
	return tea.Batch(c.changed(ctx), c.startEngine(b))
}

func (c *controller) startEngine(b *build) tea.Cmd {
	opts := sm.BuilderSpawnOpts(sm.BuilderOptions{
		RequestID: b.req.ID, Worktree: b.req.Dir, RulesFile: b.rules,
		Model: b.model, MaxBudgetUSD: b.budget,
	})
	opts.Resume = b.sessionID
	return ext.Msg(ext.EngineStartMsg{EngineID: b.engineID(), Opts: opts})
}

func (c *controller) stopEngine(b *build) tea.Cmd {
	if !b.engineUp {
		return nil
	}
	b.engineUp = false
	return ext.Msg(ext.EngineStopMsg{EngineID: b.engineID()})
}

// send gives the builder its next message, restarting it (resuming its
// session) if it is not running.
func (c *controller) send(ctx ext.Ctx, b *build, text string) tea.Cmd {
	if e := ctx.Engine(b.engineID()); e != nil && b.engineUp {
		return e.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(text)}})
	}
	b.prompt = text
	return c.startEngine(b)
}

// engine messages

func (c *controller) onAttach(ctx ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
	b := c.byEngine(m.EngineID)
	if b == nil || m.Engine == nil {
		return nil
	}
	b.engineUp = true
	if b.prompt == "" {
		return nil
	}
	p := b.prompt
	b.prompt = ""
	if b.phase == PhaseStarting {
		b.phase = PhaseBuilding
	}
	return tea.Batch(m.Engine.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(p)}}), c.changed(ctx))
}

func (c *controller) onEvent(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	if m.EngineID == ext.MainEngine || m.EngineID == "" {
		c.trackMain(m.Event)
		return nil
	}
	b := c.byEngine(m.EngineID)
	if b == nil || m.Event == nil {
		return nil
	}
	if env := m.Event.Env(); env != nil && env.SessionID != "" {
		b.sessionID = env.SessionID
	}
	switch ev := m.Event.(type) {
	case *proto.Assistant:
		for _, blk := range ev.Message.Content {
			switch blk.Type {
			case proto.BlockToolUse:
				b.lastTool = toolSummary(blk.Name, blk.Input)
			case proto.BlockText:
				if t := strings.TrimSpace(blk.Text); t != "" && ev.ParentToolUseID == "" {
					b.reply = t
				}
			}
		}
		ctx.Invalidate(ComponentID)
	case *proto.Result:
		if ev.TotalCostUSD > b.cost {
			b.cost = ev.TotalCostUSD
		}
		return c.turnDone(ctx, b, ev)
	}
	return nil
}

// toolSummary is the "last tool" shown in the progress line.
func toolSummary(name string, input json.RawMessage) string {
	var in map[string]any
	json.Unmarshal(input, &in)
	arg := ""
	for _, k := range []string{"file_path", "command", "pattern", "path", "description"} {
		if s, ok := in[k].(string); ok && s != "" {
			arg = s
			break
		}
	}
	arg = strings.Join(strings.Fields(arg), " ")
	if len(arg) > 60 {
		arg = arg[:59] + "…"
	}
	if arg == "" {
		return name
	}
	return name + " " + arg
}

func (c *controller) onExited(ctx ext.Ctx, m ext.EngineExitedMsg) tea.Cmd {
	b := c.byEngine(m.EngineID)
	if b == nil {
		return nil
	}
	wasUp := b.engineUp
	b.engineUp = false
	switch b.phase {
	case PhaseStarting, PhaseBuilding, PhaseResolving:
	default:
		return nil
	}
	msg := "the builder stopped unexpectedly"
	if m.Err != nil {
		msg = "the builder failed: " + m.Err.Error()
	} else if !wasUp {
		msg = "the builder could not start"
	}
	var detail []string
	for _, ln := range strings.Split(strings.TrimSpace(m.Stderr), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			detail = append(detail, ln)
		}
	}
	return c.fail(ctx, b, msg, detail)
}

// turnDone handles the end of a builder turn.
func (c *controller) turnDone(ctx ext.Ctx, b *build, ev *proto.Result) tea.Cmd {
	switch b.phase {
	case PhaseBuilding:
	case PhaseResolving:
		if ev.IsError {
			return c.fail(ctx, b, resultError(ev, b.budget), nil)
		}
		return c.resolved(ctx, b)
	default:
		return nil
	}
	if ev.IsError {
		return c.fail(ctx, b, resultError(ev, b.budget), nil)
	}
	reply := ev.Result
	if strings.TrimSpace(reply) == "" {
		reply = b.reply
	}
	if p, ok, err := sm.ParseConfigProposal(reply); ok {
		if err != nil {
			return c.fail(ctx, b, "the builder proposed a config change I could not read: "+err.Error(), nil)
		}
		return c.proposed(ctx, b, p)
	}
	if b.update != nil && b.update.resolving {
		return c.resolved(ctx, b)
	}
	b.phase = PhaseVetting
	return tea.Batch(c.changed(ctx), c.vet(ctx, b))
}

func resultError(ev *proto.Result, budget float64) string {
	switch ev.Subtype {
	case "error_max_budget_usd":
		return fmt.Sprintf("the builder reached its $%.2f budget; raise %s or run /mantle retry", budget, SettingMaxBudget)
	case "error_max_turns":
		return "the builder ran out of turns; run /mantle retry to continue"
	}
	msg := "the builder stopped with an error"
	if ev.Subtype != "" && ev.Subtype != "success" {
		msg += " (" + ev.Subtype + ")"
	}
	if t := strings.TrimSpace(ev.Result); t != "" {
		msg += ": " + firstLine(t)
	}
	return msg
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// vetting

type vettedMsg struct {
	id  string
	req *sm.Request
	rep *sm.Report
	err error
}

func (c *controller) vet(ctx ext.Ctx, b *build) tea.Cmd {
	pctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	ws, r := b.ws, *b.req
	return func() tea.Msg {
		rep, err := ws.Vet(pctx, &r)
		return vettedMsg{id: r.ID, req: &r, rep: rep, err: err}
	}
}

func (c *controller) onVetted(ctx ext.Ctx, m vettedMsg) tea.Cmd {
	b := c.builds[m.id]
	if b == nil || b.phase != PhaseVetting {
		return nil
	}
	b.req, b.report = m.req, m.rep
	if m.err != nil {
		return c.fail(ctx, b, "the checks could not run: "+m.err.Error(), nil)
	}
	if m.rep.OK {
		b.detail = nil
		return c.afterVet(ctx, b)
	}
	b.detail = failureLines(m.rep)
	return c.checksFailed(ctx, b, m.req.LastFailure)
}

// checksFailed sends a failed pipeline run back to the builder, or stops
// after MaxRounds (keeping the worktree).
func (c *controller) checksFailed(ctx ext.Ctx, b *build, failure string) tea.Cmd {
	if b.req.Kind == sm.RequestUndo {
		return c.fail(ctx, b, "the undo does not pass the checks; the worktree is kept", b.detail)
	}
	if b.req.Round < sm.MaxRounds {
		b.phase = PhaseBuilding
		b.lastTool = ""
		return tea.Batch(c.changed(ctx), c.send(ctx, b, sm.RetryPrompt(b.req, failure)))
	}
	return c.fail(ctx, b, fmt.Sprintf("the checks still fail after %d rounds; the worktree is kept: /mantle retry %s, or /mantle edit", sm.MaxRounds, b.req.ID), b.detail)
}

// failureLines are the progress block's detail lines for a failed report.
func failureLines(rep *sm.Report) []string {
	var out []string
	for _, res := range rep.Failed() {
		out = append(out, fmt.Sprintf("%s: %s", res.Step, res.Summary))
		for _, e := range res.Errors {
			out = append(out, "  "+e)
		}
	}
	return out
}

// afterVet decides between the visual preview and promotion.
func (c *controller) afterVet(ctx ext.Ctx, b *build) tea.Cmd {
	mode := c.confirmMode(ctx)
	if mode == ConfirmNever || c.env.stories == nil {
		return c.promote(ctx, b)
	}
	b.phase = PhasePreviewing
	before := ""
	if v, err := (launcher.Store{L: b.ws.Layout}).Current(); err == nil {
		before = v.Binary()
	}
	after, id, stories := b.report.Binary, b.req.ID, c.env.stories
	return tea.Batch(c.changed(ctx), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		diffs, err := stories(ctx, before, after)
		return previewedMsg{id: id, diffs: diffs, err: err}
	})
}

type previewedMsg struct {
	id    string
	diffs []StoryDiff
	err   error
}

func (c *controller) onPreviewed(ctx ext.Ctx, m previewedMsg) tea.Cmd {
	b := c.builds[m.id]
	if b == nil || b.phase != PhasePreviewing {
		return nil
	}
	b.diffs = m.diffs
	mode := c.confirmMode(ctx)
	if mode == ConfirmOnVisualChange && len(m.diffs) == 0 {
		return c.promote(ctx, b)
	}
	note := ""
	if m.err != nil {
		note = "The stories could not be compared: " + m.err.Error()
	}
	return ctx.OpenDialog(PreviewDialogID, previewArgs{ID: b.req.ID, Request: b.req.Request, Diffs: m.diffs, Note: note})
}

func (c *controller) onDialogClosed(ctx ext.Ctx, m ext.DialogClosedMsg) tea.Cmd {
	if m.ID != PreviewDialogID {
		return nil
	}
	res, ok := m.Result.(previewResult)
	if !ok {
		return nil
	}
	b := c.builds[res.ID]
	if b == nil || b.phase != PhasePreviewing {
		return nil
	}
	if res.Accept {
		return c.promote(ctx, b)
	}
	b.phase = PhaseReady
	return tea.Batch(c.changed(ctx), c.stopEngine(b), c.notify(ctx, ext.NoticeInfo, "kept."+b.req.ID,
		fmt.Sprintf("%s is built but not installed: /mantle promote %s to install it, /mantle discard %s to drop it", b.req.ID, b.req.ID, b.req.ID)))
}

// promotion

type promotedMsg struct {
	id  string
	req *sm.Request
	p   *sm.Promotion
	err error
}

func (c *controller) promote(ctx ext.Ctx, b *build) tea.Cmd {
	b.phase = PhasePromoting
	ws, r, rep := b.ws, *b.req, b.report
	return tea.Batch(c.changed(ctx), func() tea.Msg {
		p, err := ws.Promote(context.Background(), &r, rep)
		return promotedMsg{id: r.ID, req: &r, p: p, err: err}
	})
}

func (c *controller) onPromoted(ctx ext.Ctx, m promotedMsg) tea.Cmd {
	b := c.builds[m.id]
	if b == nil || b.phase != PhasePromoting {
		return nil
	}
	b.req = m.req
	var ce *sm.ConflictError
	switch {
	case errors.As(m.err, &ce):
		b.phase = PhaseResolving
		b.detail = append([]string{"conflicts with newer mods:"}, ce.Files...)
		return tea.Batch(c.changed(ctx), c.send(ctx, b, sm.ConflictPrompt(b.modID(), b.req.Request, ce.Files)))
	case errors.Is(m.err, sm.ErrRevetFailed):
		b.detail = strings.Split(strings.TrimSpace(b.req.LastFailure), "\n")
		return c.checksFailed(ctx, b, b.req.LastFailure)
	case m.err != nil:
		return c.fail(ctx, b, "installing failed: "+m.err.Error(), nil)
	}
	b.phase = PhaseDone
	b.version = m.p.Version.ID
	b.end = ctx.Clock().Now()
	text := fmt.Sprintf("%s is ready; active next launch", b.req.ID)
	if c.env.getenv(launcher.EnvLauncherPID) != "" && c.busyReason() == "" {
		text += ". /mantle restart to restart now"
	}
	return tea.Batch(c.changed(ctx), c.stopEngine(b), c.finalize(ctx, b),
		c.notify(ctx, ext.NoticeSuccess, "ready."+b.req.ID, text))
}

// resolved continues after the builder resolved rebase conflicts.
func (c *controller) resolved(ctx ext.Ctx, b *build) tea.Cmd {
	if b.update != nil {
		return c.updateContinue(ctx, b)
	}
	ws, r := b.ws, *b.req
	return func() tea.Msg {
		err := ws.ContinueRebase(&r)
		return continuedMsg{id: r.ID, req: &r, err: err}
	}
}

type continuedMsg struct {
	id  string
	req *sm.Request
	err error
}

func (c *controller) onContinued(ctx ext.Ctx, m continuedMsg) tea.Cmd {
	b := c.builds[m.id]
	if b == nil || b.phase != PhaseResolving {
		return nil
	}
	b.req = m.req
	var ce *sm.ConflictError
	switch {
	case errors.As(m.err, &ce):
		b.detail = append([]string{"more conflicts:"}, ce.Files...)
		return tea.Batch(c.changed(ctx), c.send(ctx, b, sm.ConflictPrompt(b.modID(), b.req.Request, ce.Files)))
	case m.err != nil:
		if b.req.Round < sm.MaxRounds {
			b.req.Round++
			return c.send(ctx, b, "The rebase cannot continue yet: "+m.err.Error()+"\nFix it in the worktree. Do not run git commands that change history.")
		}
		return c.fail(ctx, b, "the conflicts could not be resolved: "+m.err.Error(), nil)
	}
	b.phase = PhaseVetting
	b.detail = nil
	return tea.Batch(c.changed(ctx), c.vet(ctx, b))
}

// finishing

func (c *controller) fail(ctx ext.Ctx, b *build, msg string, detail []string) tea.Cmd {
	b.phase = PhaseFailed
	b.err = msg
	if detail != nil {
		b.detail = detail
	}
	b.end = ctx.Clock().Now()
	if b.cancel != nil {
		b.cancel()
	}
	var save tea.Cmd
	if b.req.State != sm.StateFailed {
		r := *b.req
		r.State, r.LastFailure = sm.StateFailed, strings.TrimSpace(msg+"\n"+strings.Join(b.detail, "\n"))
		b.req = &r
		ws, saved := b.ws, r
		save = func() tea.Msg { ws.Save(&saved); return nil }
	}
	return tea.Batch(c.changed(ctx), c.stopEngine(b), c.finalize(ctx, b), save,
		c.notify(ctx, ext.NoticeError, "failed."+b.req.ID, "/mantle "+b.req.ID+": "+msg))
}

// finalize commits the finished build's block to the transcript (or prints
// it when there is no transcript store).
func (c *controller) finalize(ctx ext.Ctx, b *build) tea.Cmd {
	it := b.item(ctx.Clock().Now())
	if ctx.Transcript() != nil {
		return ext.Msg(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{it}})
	}
	w, _ := ctx.Size()
	blk := ctx.Renderer(KeyBuild)(ext.RenderCtx{Width: max(w, 20), Theme: ctx.Theme(), Now: ctx.Clock().Now()}, it)
	return ctx.Print(strings.Join(blk.Lines, "\n"))
}
