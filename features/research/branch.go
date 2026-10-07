package research

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// The tree follows the conversation live: a prompt the user sends becomes a Running
// node under the engine's leaf (from the input's echo, or the engine's replay), and its
// turn's result finishes it and re-reads the JSONL, which is authoritative. A branch
// reply (plan 06) moves the engine leaf to the node the follow-up hangs off, then
// re-submits the held draft.

func (f *feature) setupBranch(r ext.Registrar) {
	ext.Subscribe(r, FeatureID+".branched", f.onBranched)
	ext.Subscribe(r, FeatureID+".attach", func(_ ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
		if m.EngineID == ext.MainEngine {
			f.busy = false // a (re)started engine is idle
		}
		return nil
	})
	ext.Subscribe(r, FeatureID+".engine", func(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
		if m.EngineID != ext.MainEngine {
			return nil
		}
		f.engineState(m.Event)
		switch e := m.Event.(type) {
		case *proto.User:
			if e.ParentToolUseID != "" || e.IsSynthetic || len(e.ToolResults()) > 0 {
				return nil
			}
			return f.promptSent(ctx, e.UUID, e.Message.Content.PlainText())
		case *proto.Result:
			return f.turnEnded(ctx, e)
		}
		return nil
	})
}

// onBranched re-submits the held follow-up once the engine stands at its node, or
// gives the text back when it could not get there.
func (f *feature) onBranched(ctx ext.Ctx, m ext.BranchedMsg) tea.Cmd {
	p := f.branching
	if p == nil || m.Tag != p.tag {
		return nil
	}
	f.branching = nil
	if m.Err != nil {
		return tea.Batch(
			ext.Msg(ext.EditorSetTextMsg{Text: p.draft.Text}),
			ctx.Notify(ext.Notice{Key: "research.send", Text: "Could not branch: " + m.Err.Error(), Level: ext.NoticeError, Source: FeatureID}),
		)
	}
	if f.tree != nil {
		f.tree.SetEngineLeaf(p.node)
		f.invalidate(ctx)
	}
	d := p.draft
	d.Resubmit = true
	return ctx.Submit(d)
}

// promptSent adds a node for a prompt just sent, under the engine's leaf, and shows it
// when the user asked it from the node on screen (or from the tip).
func (f *feature) promptSent(ctx ext.Ctx, id, text string) tea.Cmd {
	if f.tree == nil || id == "" || !research.IsQuestion(text) || f.tree.Node(id) != nil {
		return nil
	}
	parent := f.tree.EngineLeaf
	follow := f.viewing == "" || f.viewing == parent
	f.tree.PromptSent(id, parent, text)
	f.busy = true
	f.invalidate(ctx)
	if f.active && follow {
		return f.navigate(ctx, id)
	}
	return nil
}

// turnEnded finishes the running node and re-reads the JSONL for its leaf entry.
func (f *feature) turnEnded(ctx ext.Ctx, r *proto.Result) tea.Cmd {
	if f.tree == nil {
		return nil
	}
	state := research.Done
	switch {
	case r.Interrupted():
		state = research.Interrupted
	case r.IsError:
		state = research.Failed
	}
	f.tree.TurnEnded("", state)
	f.invalidate(ctx)
	if !f.active {
		return nil
	}
	return f.loadTree()
}

// echoed picks up the input's echo of a prompt ("user:<uuid>", running), which comes
// before the engine's replay.
func (f *feature) echoed(ctx ext.Ctx, m ext.TranscriptHistoryMsg) tea.Cmd {
	if m.EngineID != ext.MainEngine && m.EngineID != "" {
		return nil
	}
	var cmds []tea.Cmd
	for _, it := range m.Items {
		if it == nil || it.ParentID != "" || it.State != ext.Running || !isTopPrompt(it) {
			continue
		}
		u, ok := it.Data.(*proto.User)
		if !ok {
			continue
		}
		cmds = append(cmds, f.promptSent(ctx, strings.TrimPrefix(it.ID, "user:"), u.Message.Content.PlainText()))
	}
	return tea.Batch(cmds...)
}

// selected quotes a finished selection into the prompt when research.quoteOnSelect is
// on (the default).
func (f *feature) selected(ctx ext.Ctx, m ext.SelectionMsg) tea.Cmd {
	f.selection = m.Text
	if !f.active || m.Text == "" {
		return nil
	}
	if on, ok := ctx.Settings().Mantle(SettingQuoteOnSelect).(bool); ok && !on {
		return nil
	}
	if q := research.FormatQuote(m.Text); q != "" {
		return ext.Msg(ext.EditorQuoteMsg{Text: q})
	}
	return nil
}
