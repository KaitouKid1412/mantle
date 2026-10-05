package settings

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogEffort = "dialog.effort"
	effortID     = "settings.effort.slider"
)

func (a *area) setupEffort(r ext.Registrar) error {
	r.AddDialog(dialogEffort, a.newEffortSlider)
	r.AddCommand(ext.Command{
		Name: "effort", Description: "Set how much effort the model spends",
		ArgHint: "[low|medium|high|xhigh|max|auto|ultracode on|off]", Source: ext.SourceBuiltin,
		Run: a.runEffortCommand,
		Complete: func(c ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, v := range append(effortWords(a.currentRow(c)), "auto", "ultracode on", "ultracode off") {
				if strings.HasPrefix(v, prefix) {
					out = append(out, ext.Completion{Value: v})
				}
			}
			return out
		},
	})
	r.AddCommand(ext.Command{
		Name: "fast", Description: "Turn fast mode on or off", ArgHint: "[on|off]",
		Source: ext.SourceBuiltin, Run: a.runFastCommand,
	})
	r.AddAction(ext.Action{ID: ext.ActChatFastMode, Context: ext.ContextChat, Description: "Toggle fast mode",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return true, a.setFast(c, !a.fastOn(c)) }})
	r.AddAction(ext.Action{ID: ext.ActChatThinkingToggle, Context: ext.ContextChat, Description: "Toggle extended thinking",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return true, a.toggleThinking(c) }})
	r.AddAction(ext.Action{ID: ext.ActChatIncreaseEffort, Context: ext.ContextChat, Description: "Raise effort",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return a.nudgeEffort(c, 1) }})
	r.AddAction(ext.Action{ID: ext.ActChatDecreaseEffort, Context: ext.ContextChat, Description: "Lower effort",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return a.nudgeEffort(c, -1) }})
	r.AddStory(ext.Story{ID: "settings.effort/slider", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		a := storyArea()
		d, _ := a.newEffortSlider(c, nil)
		return d.View(c, ar)
	}})
	return nil
}

// currentRow is the picker row of the session model (the default row if unknown).
func (a *area) currentRow(c ext.Ctx) model.Row {
	rows := a.modelRows(c)
	if i := model.CurrentIndex(rows, model.Current{Model: a.currentModel(c)}); i >= 0 {
		return rows[i]
	}
	if len(rows) > 0 {
		return rows[0]
	}
	return model.Row{}
}

func effortWords(r model.Row) []string {
	var out []string
	for _, e := range r.Efforts {
		out = append(out, string(e))
	}
	if len(out) == 0 {
		for _, e := range model.Levels {
			out = append(out, string(e))
		}
	}
	return out
}

// effortEffects applies an effort to the session and, unless sessionOnly, saves it
// where it will win next time: the model's modelSettings entry when the user keeps one
// (it outranks the top-level key), otherwise effortLevel via update_settings.
func (a *area) effortEffects(c ext.Ctx, r model.Row, e model.Effort, sessionOnly bool) []action.Effect {
	out := []action.Effect{action.ControlEffect(proto.SubApplyFlagSettings,
		map[string]any{"settings": map[string]any{"effortLevel": string(e)}})}
	if sessionOnly {
		return out
	}
	if !e.Persistable() {
		return append(out, action.Effect{Note: "Max effort applies to this session only; it is not saved."})
	}
	if key := r.Info.SettingsKey(); key != "" && a.hasModelSetting(c, key) {
		return append(out, action.PatchEffect(patch.SetKey(patch.User, string(e), "modelSettings", key, "effortLevel")))
	}
	return append(out, action.ControlEffect(proto.SubUpdateSettings,
		map[string]any{"source": string(patch.User), "settings": map[string]any{"effortLevel": string(e)}}))
}

func (a *area) hasModelSetting(c ext.Ctx, key string) bool {
	ms, _ := settingsDoc(c, "modelSettings")["modelSettings"].(map[string]any)
	for k, v := range ms {
		if model.Family(k) == key {
			if m, ok := v.(map[string]any); ok {
				if _, ok := m["effortLevel"]; ok {
					return true
				}
			}
		}
	}
	return false
}

func (a *area) setEffort(c ext.Ctx, e model.Effort, sessionOnly bool) tea.Cmd {
	r := a.currentRow(c)
	if len(r.Efforts) == 0 {
		return c.Notify(ext.Notice{Key: "settings.effort", Level: ext.NoticeWarning, Source: "settings",
			Text: r.Label + " has no effort levels."})
	}
	e = model.Clamp(e, r.Efforts)
	a.sessionEffort = e
	text := "Effort set to " + string(e)
	if sessionOnly || !e.Persistable() {
		text += " for this session"
	}
	return tea.Batch(a.apply(c, a.effortEffects(c, r, e, sessionOnly)),
		c.Notify(ext.Notice{Key: "settings.effort", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

func (a *area) runEffortCommand(c ext.Ctx, args string) tea.Cmd {
	args = strings.ToLower(strings.TrimSpace(args))
	switch {
	case args == "":
		return c.OpenDialog(dialogEffort, nil)
	case args == "ultracode on" || args == "ultracode off" || args == "ultracode":
		return a.setUltracode(c, args != "ultracode off", false)
	}
	if e, ok := model.ParseEffort(args); ok {
		return a.setEffort(c, e, false)
	}
	// "auto", "status" and anything newer: the engine's own /effort handles it.
	if eng := c.Engine(""); eng != nil {
		a.sessionEffort = ""
		return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("/effort " + args)}})
	}
	return ext.Msg(noEngineMsg{What: "/effort"})
}

func (a *area) ultracodeOn(c ext.Ctx) bool {
	return model.Ultracode(a.sessionUltracode, settingsDoc(c, "ultracode", "effortLevel"))
}

func (a *area) setUltracode(c ext.Ctx, on, sessionOnly bool) tea.Cmd {
	a.sessionUltracode = &on
	effects := []action.Effect{action.ControlEffect(proto.SubApplyFlagSettings,
		map[string]any{"settings": map[string]any{"ultracode": on}})}
	if !sessionOnly {
		p := patch.DeleteKey(patch.User, "ultracode")
		if on {
			p = patch.SetKey(patch.User, true, "ultracode")
		}
		effects = append(effects, action.PatchEffect(p))
	}
	text := "Ultracode off"
	if on {
		text = "Ultracode on"
	}
	return tea.Batch(a.apply(c, effects),
		c.Notify(ext.Notice{Key: "settings.ultracode", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

// nudgeEffort is chat:increaseEffort / chat:decreaseEffort: one step, this session.
func (a *area) nudgeEffort(c ext.Ctx, delta int) (bool, tea.Cmd) {
	r := a.currentRow(c)
	if len(r.Efforts) == 0 {
		return false, nil
	}
	cur := r.Effort.Effort
	next := model.Step(cur, r.Efforts, delta)
	if next == cur {
		return true, nil
	}
	return true, a.setEffort(c, next, true)
}

// ---- fast mode ----

func (a *area) fastOn(c ext.Ctx) bool {
	if a.sessionFast != nil {
		return *a.sessionFast
	}
	state := ""
	if a.engine.sys != nil {
		state = a.engine.sys.FastModeState
	} else if a.engine.init != nil {
		state = a.engine.init.FastModeState
	}
	return state == "on"
}

func (a *area) fastUnavailable(c ext.Ctx) string {
	if a.engine.sys != nil && a.engine.sys.FastModeDisabledReason != "" {
		return a.engine.sys.FastModeDisabledReason
	}
	if a.engine.init != nil && a.engine.init.FastModeDisabledReason != "" {
		return a.engine.init.FastModeDisabledReason
	}
	if r := a.currentRow(c); r.Value != "" && !r.Fast {
		return r.Label + " has no fast mode"
	}
	return ""
}

func (a *area) runFastCommand(c ext.Ctx, args string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(args)) {
	case "":
		return a.setFast(c, !a.fastOn(c))
	case "on":
		return a.setFast(c, true)
	case "off":
		return a.setFast(c, false)
	}
	return c.Notify(ext.Notice{Key: "settings.fast", Level: ext.NoticeWarning, Source: "settings", Text: "Use /fast, /fast on or /fast off."})
}

// setFast applies fast mode through flag settings (the engine requires that opt-in in
// headless mode) and saves it unless the user opted into per-session fast mode.
func (a *area) setFast(c ext.Ctx, on bool) tea.Cmd {
	if why := a.fastUnavailable(c); on && why != "" {
		return c.Notify(ext.Notice{Key: "settings.fast", Level: ext.NoticeWarning, Source: "settings",
			Text: "Fast mode is unavailable: " + why})
	}
	a.sessionFast = &on
	effects := []action.Effect{action.ControlEffect(proto.SubApplyFlagSettings,
		map[string]any{"settings": map[string]any{"fastMode": on}})}
	perSession, _ := settingsDoc(c, "fastModePerSessionOptIn")["fastModePerSessionOptIn"].(bool)
	if !perSession {
		p := patch.DeleteKey(patch.User, "fastMode")
		if on {
			p = patch.SetKey(patch.User, true, "fastMode")
		}
		effects = append(effects, action.PatchEffect(p))
	}
	text := "Fast mode off"
	if on {
		text = "Fast mode on"
	}
	return tea.Batch(a.apply(c, effects),
		c.Notify(ext.Notice{Key: "settings.fast", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

// ---- thinking ----

func (a *area) thinkingOn(c ext.Ctx) bool {
	if a.sessionThinking != nil {
		return *a.sessionThinking
	}
	v, ok := settingsDoc(c, "alwaysThinkingEnabled")["alwaysThinkingEnabled"].(bool)
	return !ok || v
}

// toggleThinking is chat:thinkingToggle: a session change through flag settings.
// Models with adaptive thinking cannot turn it off, so the toggle is refused there.
func (a *area) toggleThinking(c ext.Ctx) tea.Cmd {
	r := a.currentRow(c)
	if r.Thinking == model.ThinkingLocked {
		return c.Notify(ext.Notice{Key: "settings.thinking", Source: "settings",
			Text: "Thinking is always on for " + r.Label + "."})
	}
	on := !a.thinkingOn(c)
	a.sessionThinking = &on
	text := "Thinking off for this session"
	if on {
		text = "Thinking on for this session"
	}
	return tea.Batch(a.apply(c, []action.Effect{action.ControlEffect(proto.SubApplyFlagSettings,
		map[string]any{"settings": map[string]any{"alwaysThinkingEnabled": on}})}),
		c.Notify(ext.Notice{Key: "settings.thinking", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

// ---- effort slider dialog ----

type effortSlider struct {
	a         *area
	row       model.Row
	effort    model.Effort
	ultracode bool
	origUltra bool
}

func (a *area) newEffortSlider(c ext.Ctx, _ any) (ext.Dialog, error) {
	r := a.currentRow(c)
	u := a.ultracodeOn(c)
	return &effortSlider{a: a, row: r, effort: r.Effort.Effort, ultracode: u, origUltra: u}, nil
}

func (s *effortSlider) ID() string               { return effortID }
func (s *effortSlider) Placement() ext.Placement { return ext.PlaceInline }
func (s *effortSlider) KeyContext() string       { return ext.ContextEffortSlider }
func (s *effortSlider) KeyContexts() []string {
	return []string{ext.ContextEffortSlider, ext.ContextSelect}
}
func (s *effortSlider) Init(ext.Ctx) tea.Cmd                               { return nil }
func (s *effortSlider) Update(ext.Ctx, tea.Msg) tea.Cmd                    { return nil }
func (s *effortSlider) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (s *effortSlider) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }

func (s *effortSlider) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActEffortSliderIncreaseEffort:
		s.effort = model.Step(s.effort, s.row.Efforts, 1)
	case ext.ActEffortSliderDecreaseEffort:
		s.effort = model.Step(s.effort, s.row.Efforts, -1)
	case ext.ActEffortSliderToggleUltracode:
		s.ultracode = !s.ultracode
	case ext.ActSelectAccept:
		return true, s.commit(c, false)
	case ext.ActEffortSliderThisSessionOnly:
		return true, s.commit(c, true)
	case ext.ActSelectCancel:
		kept := "Effort unchanged"
		if s.row.Effort.Effort != "" {
			kept = "Kept effort as " + string(s.row.Effort.Effort)
		}
		return true, tea.Sequence(c.CloseDialog(dialogEffort), resultLine(c, kept))
	default:
		return false, nil
	}
	c.Invalidate(s.ID())
	return true, nil
}

func (s *effortSlider) commit(c ext.Ctx, sessionOnly bool) tea.Cmd {
	var cmds []tea.Cmd
	if len(s.row.Efforts) > 0 && s.effort != "" {
		cmds = append(cmds, s.a.setEffort(c, s.effort, sessionOnly))
	}
	if s.ultracode != s.origUltra {
		cmds = append(cmds, s.a.setUltracode(c, s.ultracode, sessionOnly))
	}
	cmds = append(cmds, c.CloseDialog(dialogEffort))
	return tea.Sequence(cmds...)
}

func (s *effortSlider) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	var body []string
	if len(s.row.Efforts) == 0 {
		body = append(body, t.Paint(theme.Inactive, s.row.Label+" has no effort levels."))
	} else {
		var parts []string
		for _, e := range s.row.Efforts {
			if e == s.effort {
				parts = append(parts, t.Paint(theme.Suggestion, "● "+string(e)))
			} else {
				parts = append(parts, t.Paint(theme.Inactive, "○ "+string(e)))
			}
		}
		body = append(body, strings.Join(parts, "  "))
	}
	u := t.Paint(theme.Inactive, "off")
	if s.ultracode {
		u = t.Paint(theme.Suggestion, "on")
	}
	body = append(body, "", "Ultracode  "+u+t.Paint(theme.Inactive, "  · lets Claude plan and run multi-agent workflows"))
	sub := "For " + s.row.Label + ". Higher effort thinks longer and costs more."
	return ext.Rendered{Text: frame(t, ar.Width, "Effort", sub, body, hintLine(
		"←/→", "change", keyName(c.KeysFor(ext.ContextEffortSlider, ext.ActEffortSliderToggleUltracode), "tab"), "toggle ultracode",
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectAccept), "enter"), "save",
		keyName(c.KeysFor(ext.ContextEffortSlider, ext.ActEffortSliderThisSessionOnly), "s"), "use for this session only",
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "cancel"))}
}

var _ ext.ActionHandler = (*effortSlider)(nil)
