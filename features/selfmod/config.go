package selfmod

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"

	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// proposed handles a builder that answered with a config change instead of
// code: nothing is built, the worktree is dropped, and the user can apply
// the change with /mantle apply <id>.
func (c *controller) proposed(ctx ext.Ctx, b *build, p *sm.ConfigProposal) tea.Cmd {
	b.proposal = p
	b.phase = PhaseConfig
	b.end = ctx.Clock().Now()
	b.detail = []string{
		p.Summary,
		fmt.Sprintf("%s setting %s = %s", p.Scope, p.Key, compactJSON(p.Value)),
		"apply it with /mantle apply " + b.req.ID,
	}
	ws, r := b.ws, *b.req
	save := func() tea.Msg {
		if data, err := json.MarshalIndent(p, "", "  "); err == nil {
			os.WriteFile(proposalFile(ws, r.ID), append(data, '\n'), 0o644)
		}
		ws.Abandon(&r)
		r.State = sm.StateConfig
		ws.Save(&r)
		return nil
	}
	return tea.Batch(c.changed(ctx), c.stopEngine(b), c.finalize(ctx, b), save,
		c.notify(ctx, ext.NoticeInfo, "config."+b.req.ID,
			fmt.Sprintf("No rebuild needed: %s. /mantle apply %s to apply it", p.Summary, b.req.ID)))
}

func proposalFile(ws *sm.Workspace, id string) string {
	return filepath.Join(ws.Layout.Build(id), "proposal.json")
}

func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

// cmdApply applies a proposal: a mantle setting through the settings API,
// a Claude Code setting by merging it into ~/.claude/settings.json (Claude
// Code reloads that file by itself).
func (c *controller) cmdApply(ctx ext.Ctx, id string) tea.Cmd {
	var p *sm.ConfigProposal
	if b := c.builds[id]; b != nil && b.proposal != nil {
		p = b.proposal
	} else if data, err := os.ReadFile(proposalFile(c.workspace(ctx), id)); err == nil {
		var cp sm.ConfigProposal
		if json.Unmarshal(data, &cp) == nil {
			p = &cp
		}
	}
	if p == nil {
		return c.notify(ctx, ext.NoticeWarning, "apply", id+" has no config proposal")
	}
	v, err := p.DecodedValue()
	if err != nil {
		return c.notify(ctx, ext.NoticeError, "apply", "cannot read the proposed value: "+err.Error())
	}
	switch p.Scope {
	case "mantle":
		return tea.Batch(ctx.Settings().SetMantle(p.Key, v),
			c.notify(ctx, ext.NoticeSuccess, "apply", "Set mantle setting "+p.Key))
	default:
		path, key := c.env.claudeSettingsPath, p.Key
		return func() tea.Msg {
			if err := MergeJSONSetting(path, key, v); err != nil {
				return printMsg{notice: notice(ext.NoticeError, "apply", "Could not update "+path+": "+err.Error())}
			}
			return printMsg{notice: notice(ext.NoticeSuccess, "apply", fmt.Sprintf("Set %s in %s", key, path))}
		}
	}
}

// MergeJSONSetting sets one top-level key in a JSON settings file, keeping
// every other key. The old file is kept as <file>.mantle-bak and the new one
// is written atomically. A file that is not a JSON object is left alone.
func MergeJSONSetting(path, key string, value any) error {
	m := map[string]any{}
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(old, &m); err != nil {
			return fmt.Errorf("it is not a valid JSON object (%v); not changing it", err)
		}
		if m == nil {
			m = map[string]any{}
		}
		if err := os.WriteFile(path+".mantle-bak", old, 0o600); err != nil {
			return err
		}
	}
	m[key] = value
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
