package sessions

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Resume from summary: resuming a big conversation that sat idle re-reads all of it
// on the next prompt. Like Claude Code, mantle offers to compact it first. The
// headless engine has no "resume from summary" request, so compacting is the engine's
// /compact right after the resume.

const (
	DialogResumeSummary = "dialog.resume-summary"

	summaryIdle   = time.Hour
	summaryTokens = 100_000

	// summaryDeclinedKey in the feature's store: the user chose "don't ask again".
	summaryDeclinedKey = "resumeSummaryDeclined"
)

// summaryArgs opens the offer: before switching to meta (switch), or after the host
// resumed a session at startup.
type summaryArgs struct {
	meta    sessions.SessionMeta
	sw      switchOpts
	startup bool
	idle    time.Duration
	tokens  int64
}

func (f *feature) registerSummary(r ext.Registrar) {
	r.AddDialog(DialogResumeSummary, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		a, _ := args.(summaryArgs)
		return f.summaryDialog(a), nil
	})
	ext.Subscribe(r, "sessions.compact-on-attach", func(ctx ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
		if m.EngineID != ext.MainEngine || !f.compactOnAttach {
			return nil
		}
		f.compactOnAttach = false
		return sendCommand(ctx, ext.MainEngine, "/compact")
	})
}

// summaryWorthy reports whether resuming a conversation of this size and age should
// offer to compact first.
func (f *feature) summaryWorthy(ctx ext.Ctx, lastActive time.Time, tokens int64) bool {
	if tokens <= summaryTokens || lastActive.IsZero() || f.now().Sub(lastActive) <= summaryIdle {
		return false
	}
	var declined bool
	if ok, _ := ctx.Store(FeatureID).Get(summaryDeclinedKey, &declined); ok && declined {
		return false
	}
	return true
}

func (f *feature) summaryDialog(a summaryArgs) ext.Dialog {
	subtitle := "This conversation was last active " + ago(a.idle) + " and holds about " +
		compactTokens(a.tokens) + " tokens of context. Compacting first summarizes it, so the next prompt starts small."
	compact := choice{label: "Compact it first (recommended)"}
	full := choice{label: "Keep the full conversation"}
	never := choice{label: "Keep the full conversation, and don't ask again"}
	if a.startup {
		compact.run = func(ctx ext.Ctx) tea.Cmd { return f.compactWhenReady(ctx) }
		full.run = func(ext.Ctx) tea.Cmd { return nil }
		never.run = func(ctx ext.Ctx) tea.Cmd { return f.declineSummary(ctx, nil) }
	} else {
		sw := a.sw
		sw.skipSummary = true
		compactSw := sw
		compactSw.compactAfter = true
		compact.run = func(ctx ext.Ctx) tea.Cmd { return f.switchTo(ctx, a.meta, compactSw) }
		full.run = func(ctx ext.Ctx) tea.Cmd { return f.switchTo(ctx, a.meta, sw) }
		never.run = func(ctx ext.Ctx) tea.Cmd { return f.declineSummary(ctx, f.switchTo(ctx, a.meta, sw)) }
	}
	return &choiceDialog{
		id: DialogResumeSummary, title: "Resume a long conversation", subtitle: subtitle,
		choices: []choice{compact, full, never},
	}
}

func (f *feature) declineSummary(ctx ext.Ctx, then tea.Cmd) tea.Cmd {
	_ = ctx.Store(FeatureID).Set(summaryDeclinedKey, true)
	return then
}

// compactWhenReady compacts the main conversation now, or once its engine attaches.
func (f *feature) compactWhenReady(ctx ext.Ctx) tea.Cmd {
	if ctx.Engine(ext.MainEngine) != nil {
		return sendCommand(ctx, ext.MainEngine, "/compact")
	}
	f.compactOnAttach = true
	return nil
}

// ago is a duration as "2 hours ago", "3 days ago".
func ago(d time.Duration) string {
	switch {
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 48*time.Hour:
		return itoa(int(d/time.Hour)) + " hours ago"
	}
	return itoa(int(d/(24*time.Hour))) + " days ago"
}
