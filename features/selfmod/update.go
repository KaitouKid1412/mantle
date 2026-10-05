package selfmod

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// updateState tracks /mantle update: commits are re-applied one at a time,
// and the builder resolves conflicts with each mod's request as intent.
type updateState struct {
	resolving bool
	commit    *sm.Commit
}

func (c *controller) cmdUpdate(ctx ext.Ctx) tea.Cmd {
	for _, b := range c.builds {
		if b.update != nil && activePhase(b.phase) {
			return c.notify(ctx, ext.NoticeWarning, "update", "an update is already running ("+b.req.ID+")")
		}
	}
	return c.begin(ctx, "update", func(ws *sm.Workspace) (*sm.Request, error) {
		r, err := ws.StartUpdate()
		if errors.Is(err, sm.ErrUpToDate) {
			return nil, fmt.Errorf("your mods are already on the newest mantle (%s)", ws.Upstream)
		}
		return r, err
	})
}

type updateStepMsg struct {
	id    string
	req   *sm.Request
	stuck *sm.Commit
	err   error
}

// updateStep re-applies the pending commits in a Cmd.
func (c *controller) updateStep(ctx ext.Ctx, b *build) tea.Cmd {
	ws, r := b.ws, *b.req
	return func() tea.Msg {
		stuck, err := ws.UpdateStep(&r)
		return updateStepMsg{id: r.ID, req: &r, stuck: stuck, err: err}
	}
}

func (c *controller) updateContinue(ctx ext.Ctx, b *build) tea.Cmd {
	return c.updateStep(ctx, b)
}

func (c *controller) onUpdateStep(ctx ext.Ctx, m updateStepMsg) tea.Cmd {
	b := c.builds[m.id]
	if b == nil || b.update == nil || !activePhase(b.phase) {
		return nil
	}
	b.req = m.req
	var ce *sm.ConflictError
	switch {
	case errors.As(m.err, &ce):
		modID, request := "", ""
		if m.stuck != nil {
			modID, request = m.stuck.Trailers.Get(sm.TrailerMod), m.stuck.Trailers.Get(sm.TrailerRequest)
			if request == "" {
				request = m.stuck.Subject
			}
		}
		b.update.resolving, b.update.commit = true, m.stuck
		b.phase = PhaseResolving
		b.detail = append([]string{fmt.Sprintf("re-applying %s: conflicts in", modID)}, ce.Files...)
		return tea.Batch(c.changed(ctx), c.send(ctx, b, sm.ConflictPrompt(modID, request, ce.Files)))
	case m.err != nil:
		if b.update.resolving && b.req.Round < sm.MaxRounds {
			// The resolution is incomplete (conflict markers left): ask again.
			b.req.Round++
			return c.send(ctx, b, "The update cannot continue yet: "+m.err.Error()+"\nFinish resolving the conflicts. Do not run git commands.")
		}
		return c.fail(ctx, b, "the update failed: "+m.err.Error(), nil)
	}
	b.update.resolving = false
	b.phase = PhaseVetting
	b.detail = nil
	return tea.Batch(c.changed(ctx), c.vet(ctx, b))
}

func (c *controller) cmdUpstream(ctx ext.Ctx, id string) tea.Cmd {
	ws := c.workspace(ctx)
	return async(func() (string, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		branch, err := ws.ExportUpstream(cctx, id)
		if err != nil {
			return "", err
		}
		origin, _ := ws.OriginURL()
		return fmt.Sprintf("exported %s to %s as branch %s (main is untouched)", id, origin, branch), nil
	}, "/mantle upstream")
}
