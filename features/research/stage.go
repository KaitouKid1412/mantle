package research

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// The route stage sends a follow-up to the viewed node (MT-R6). It runs after history,
// slash, bash, attachments and priority (only real prompts get here) and before send.
// Viewing the engine's tip, the prompt goes on as usual. Viewing any other node, the
// draft is taken and the engine is moved there first (rewind in place, or a restart at
// the node's last entry); once that is done the draft is submitted again (Resubmit).

// Stage ID and priority.
const (
	StageRoute = "research.route"
	PrioRoute  = 600
)

// pendingSend is a follow-up held while the engine moves to its node.
type pendingSend struct {
	tag   string
	node  string // the node the follow-up hangs off
	draft ext.Draft
}

func (f *feature) setupStage(r ext.Registrar) {
	r.AddPromptStage(StageRoute, PrioRoute, f.route)
}

func (f *feature) route(ctx ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	if !f.active || d.Resubmit || d.Mode == "bash" || !research.IsQuestion(d.Text) {
		return ext.Continue, nil
	}
	if f.branching != nil {
		return f.reject(ctx, d, "Wait a moment: the previous follow-up is still on its way")
	}
	if f.sid == "" {
		f.syncSession(ctx)
	}
	plan := research.Plan(f.tree, f.viewing, f.busy)
	switch plan.Kind {
	case research.Plain:
		f.confirmed = ""
		return ext.Continue, nil
	case research.Blocked:
		return f.reject(ctx, d, "Can't ask a follow-up here: "+plan.Reason)
	}
	node := f.node()
	if plan.NeedsConfirm {
		key := node.ID + "\x00" + d.Text
		if f.confirmed != key {
			f.confirmed = key
			return f.reject(ctx, d, "This question is above a compaction: Claude will reload the full earlier context. Press Enter again to ask it")
		}
	}
	f.confirmed = ""
	p := &pendingSend{tag: uuid.NewString(), node: node.ID, draft: *d}
	p.draft.Attachments = append([]ext.Attachment(nil), d.Attachments...)
	f.branching = p
	req := ext.BranchRequestMsg{EngineID: ext.MainEngine, SessionID: f.sid, At: plan.At, Tag: p.tag}
	if plan.Kind == research.Rewind {
		req.DropPrompt = plan.DropPrompt
	}
	ctx.Log().Debug("research: branching", "kind", plan.Kind.String(), "at", plan.At, "drop", req.DropPrompt)
	return ext.Consumed, tea.Batch(
		ctx.Notify(ext.Notice{Key: "research.send", Text: "Branching from " + quoteLabel(node.Prompt) + "…", Source: FeatureID}),
		ext.Msg(req),
	)
}

// reject refuses a draft and puts its text back in the editor (the pipeline does
// not: the editor was cleared on submit).
func (f *feature) reject(ctx ext.Ctx, d *ext.Draft, why string) (ext.Verdict, tea.Cmd) {
	return ext.Reject, tea.Batch(
		ext.Msg(ext.EditorSetTextMsg{Text: d.Text}),
		ctx.Notify(ext.Notice{Key: "research.send", Text: why, Level: ext.NoticeWarning, Source: FeatureID}),
	)
}

// quoteLabel is a node's question for a notice: one line, at most 40 runes.
func quoteLabel(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > 40 {
		q = string(r[:39]) + "…"
	}
	if q == "" {
		return "this question"
	}
	return `"` + q + `"`
}
