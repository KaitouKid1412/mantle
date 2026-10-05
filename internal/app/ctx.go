package app

import (
	"log/slog"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// uiCtx implements ext.Ctx over the root model. Valid only on the UI goroutine.
type uiCtx struct{ r *Root }

var _ ext.Ctx = (*uiCtx)(nil)

func (c *uiCtx) Engine(id string) ext.Engine {
	if id == "" {
		id = ext.MainEngine
	}
	if e, ok := c.r.engines[id]; ok {
		return e
	}
	return nil
}

func (c *uiCtx) Session() ext.SessionInfo {
	if s, ok := c.r.sessions[ext.MainEngine]; ok {
		return s
	}
	return ext.SessionInfo{EngineID: ext.MainEngine}
}

func (c *uiCtx) Transcript() ext.Transcript { return c.r.transcript }
func (c *uiCtx) Settings() ext.Settings     { return c.r.opts.Settings }
func (c *uiCtx) Theme() *theme.Theme        { return &c.r.theme }
func (c *uiCtx) Clock() ext.Clock           { return c.r.opts.Clock }
func (c *uiCtx) Size() (int, int)           { return c.r.w, c.r.h }
func (c *uiCtx) Layout() ext.LayoutMode     { return c.r.layoutMode() }
func (c *uiCtx) Invalidate(id string)       { c.r.invalidate(id) }
func (c *uiCtx) Log() *slog.Logger          { return c.r.logger }
func (c *uiCtx) Focused() string            { return c.r.focus }
func (c *uiCtx) Accessibility() ext.Accessibility {
	return c.r.opts.A11y
}

func (c *uiCtx) Print(blocks ...string) tea.Cmd { return c.r.enqueuePrint(blocks) }
func (c *uiCtx) Reprint() tea.Cmd               { return c.r.enqueueClear() }

func (c *uiCtx) Notify(n ext.Notice) tea.Cmd {
	return c.r.addNotice(n)
}

func (c *uiCtx) OpenDialog(id string, args any) tea.Cmd { return c.r.openDialog(id, args) }
func (c *uiCtx) CloseDialog(id string) tea.Cmd          { return c.r.closeDialog(id) }

func (c *uiCtx) Run(a ext.ActionID) tea.Cmd {
	_, cmd := c.r.runAction(a)
	return cmd
}

func (c *uiCtx) Store(featureID string) ext.KV { return c.r.store(featureID) }

func (c *uiCtx) Focus(id string) tea.Cmd { return c.r.setFocus(id) }

func (c *uiCtx) SetContextActive(context string, on bool) {
	if on {
		c.r.ambient[context] = true
	} else {
		delete(c.r.ambient, context)
	}
}

func (c *uiCtx) KeysFor(context string, a ext.ActionID) []string {
	return c.r.resolver.Keymap().KeysFor(context, a)
}

func (c *uiCtx) Command(name string) (ext.Command, bool) {
	name = strings.TrimPrefix(name, "/")
	if cmd, ok := c.r.commands[name]; ok && !c.r.host.Disabled(c.r.host.featureOf(KindCommand, cmd.ID)) {
		return c.r.safeCommand(*cmd), true
	}
	for _, key := range sortedKeys(c.r.runtimeCmds) {
		for _, cmd := range c.r.runtimeCmds[key] {
			if cmd.Name == name || slices.Contains(cmd.Aliases, name) {
				return cmd, true
			}
		}
	}
	return ext.Command{}, false
}

// Commands lists the menu-visible commands. A hidden command (registered Hidden, or
// hidden by a CommandVisibilityMsg overlay) is left out but still reserves its name,
// so a same-named engine command does not show through in its place.
func (c *uiCtx) Commands() []ext.Command {
	seen := map[string]bool{}
	var out []ext.Command
	for _, e := range c.r.host.live(KindCommand) {
		cmd := e.value.(ext.Command)
		if seen[cmd.Name] {
			continue
		}
		seen[cmd.Name] = true
		if cmd.Hidden || c.r.commandHidden(cmd.Name) {
			continue
		}
		out = append(out, c.r.safeCommand(cmd))
	}
	for _, key := range sortedKeys(c.r.runtimeCmds) {
		for _, cmd := range c.r.runtimeCmds[key] {
			if seen[cmd.Name] {
				continue
			}
			seen[cmd.Name] = true
			if cmd.Hidden || c.r.commandHidden(cmd.Name) {
				continue
			}
			out = append(out, cmd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// commandHidden reports whether any visibility overlay hides a command name.
func (r *Root) commandHidden(name string) bool {
	for _, hidden := range r.cmdHidden {
		if hidden[name] {
			return true
		}
	}
	return false
}

func (c *uiCtx) Submit(d ext.Draft) tea.Cmd { return c.r.submit(d) }

func (c *uiCtx) Renderer(k ext.ContentKey) ext.Renderer {
	f, feature := c.r.host.renderer(k)
	if f == nil {
		return plainRenderer
	}
	r := c.r
	return func(rc ext.RenderCtx, it *ext.Item) (b ext.Block) {
		if r.host.Disabled(feature) {
			return plainRenderer(rc, it)
		}
		if !r.safe(feature, ext.RendererID(k), func() { b = f(rc, it) }) {
			return plainRenderer(rc, it)
		}
		return b
	}
}

// plainRenderer is the fallback when no renderer (not even "default") is registered.
func plainRenderer(rc ext.RenderCtx, it *ext.Item) ext.Block {
	line := string(it.Key)
	if it.ID != "" {
		line += " " + it.ID
	}
	return ext.Block{Lines: []string{clip(line, rc.Width)}}
}

// safeCommand wraps a command's Run and Complete so panics disable its feature.
func (r *Root) safeCommand(cmd ext.Command) ext.Command {
	feature := r.host.featureOf(KindCommand, cmd.ID)
	if run := cmd.Run; run != nil {
		cmd.Run = func(ctx ext.Ctx, args string) (out tea.Cmd) {
			r.safe(feature, cmd.ID, func() { out = run(ctx, args) })
			return wrapCmd(feature, cmd.ID, out)
		}
	}
	if comp := cmd.Complete; comp != nil {
		cmd.Complete = func(ctx ext.Ctx, s string) (out []ext.Completion) {
			r.safe(feature, cmd.ID+".Complete", func() { out = comp(ctx, s) })
			return out
		}
	}
	return cmd
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// submit runs the prompt pipeline. Stages run in ascending priority until one consumes
// or rejects the draft.
func (r *Root) submit(d ext.Draft) tea.Cmd {
	var cmds []tea.Cmd
	draft := d
	r.log().Debug("submit", "mode", d.Mode, "len", len(d.Text), "engine", r.engines[ext.MainEngine] != nil)
	for _, e := range r.stages {
		var v ext.Verdict
		var cmd tea.Cmd
		if !r.safe(e.feature, e.id, func() { v, cmd = e.value.(ext.PromptStage)(r.ctx, &draft) }) {
			continue
		}
		cmds = append(cmds, wrapCmd(e.feature, e.id, cmd))
		if v != ext.Continue {
			r.log().Debug("submit: stage decided", "stage", e.id, "verdict", int(v))
			break
		}
	}
	return tea.Batch(cmds...)
}

// setFocus moves focus to a component (OnBlur/OnFocus called).
func (r *Root) setFocus(id string) tea.Cmd {
	if id == r.focus {
		return nil
	}
	c := r.byID[id]
	if c == nil {
		return nil
	}
	if _, ok := c.m.comp.(ext.Focusable); !ok {
		return nil
	}
	var cmds []tea.Cmd
	if old := r.byID[r.focus]; old != nil {
		if fa, ok := old.m.comp.(ext.FocusAware); ok {
			var cmd tea.Cmd
			r.safe(old.feature, r.focus+".OnBlur", func() { cmd = fa.OnBlur(r.ctx) })
			cmds = append(cmds, wrapCmd(old.feature, r.focus, cmd))
		}
		old.valid = false
	}
	r.focus = id
	c.valid = false
	if fa, ok := c.m.comp.(ext.FocusAware); ok {
		var cmd tea.Cmd
		r.safe(c.feature, id+".OnFocus", func() { cmd = fa.OnFocus(r.ctx) })
		cmds = append(cmds, wrapCmd(c.feature, id, cmd))
	}
	return tea.Batch(cmds...)
}
