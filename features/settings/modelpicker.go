package settings

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogModel   = "dialog.model"
	modelPickerID = "settings.model.picker"

	// settingModelSwitchWarning turns the cache warning on model switches on or off.
	settingModelSwitchWarning = "settings.modelSwitchWarning"
)

func (a *area) setupModel(r ext.Registrar) error {
	a.addSetting(r, ext.SettingSpec{
		Key: settingModelSwitchWarning, Type: "bool", Default: true,
		Description: "Ask before switching models mid-conversation, since the new model re-reads the whole conversation without the prompt cache.",
	})
	r.AddDialog(dialogModel, a.newModelPicker)
	r.AddCommand(ext.Command{
		Name: "model", Description: "Choose the model and its effort", ArgHint: "[model]",
		Source: ext.SourceBuiltin, Run: a.runModelCommand, Complete: a.completeModel,
	})
	r.AddAction(ext.Action{
		ID: ext.ActChatModelPicker, Context: ext.ContextChat, Description: "Open the model picker",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return true, c.OpenDialog(dialogModel, nil) },
	})
	a.setupNewerModel(r)
	for _, s := range modelStories() {
		r.AddStory(s)
	}
	return nil
}

// modelRows builds picker rows from the cached engine state and current settings.
func (a *area) modelRows(c ext.Ctx) []model.Row {
	rows := model.Rows(a.engine.models, a.engine.unavailable, model.Current{Model: a.currentModel(c)}, a.effortInputs(c))
	// The engine reports the session model's effort in system/init; when nothing
	// mantle knows sets it, that beats our assumed default.
	if a.engine.sys != nil {
		if e, ok := model.ParseEffort(a.engine.sys.Effort); ok {
			for i := range rows {
				if rows[i].Current && rows[i].Effort.Source == model.SourceDefault && len(rows[i].Efforts) > 0 {
					rows[i].Effort.Effort = model.Clamp(e, rows[i].Efforts)
				}
			}
		}
	}
	return rows
}

func (a *area) effortInputs(c ext.Ctx) model.EffortInputs {
	return model.EffortInputs{
		Session:  a.sessionEffort,
		Env:      os.Getenv("CLAUDE_CODE_EFFORT_LEVEL"),
		Settings: settingsDoc(c, "effortLevel", "modelSettings", "maxEffortLevel", "ultracode"),
	}
}

func (a *area) runModelCommand(c ext.Ctx, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" {
		return c.OpenDialog(dialogModel, nil)
	}
	row, ok := model.FindRow(a.modelRows(c), args)
	if !ok {
		// The engine knows models the list leaves out (full IDs, custom gateways).
		row = model.Row{Value: args, Label: args, Info: model.Info{Value: args}}
	}
	return a.chooseModel(c, model.Choice{Row: row})
}

func (a *area) completeModel(c ext.Ctx, prefix string) []ext.Completion {
	var out []ext.Completion
	for _, r := range a.modelRows(c) {
		if r.Disabled || !strings.HasPrefix(strings.ToLower(r.Value), strings.ToLower(prefix)) {
			continue
		}
		out = append(out, ext.Completion{Value: r.Value, Display: r.Value, Description: r.Label})
	}
	return out
}

// chooseModel applies a choice and reports it.
func (a *area) chooseModel(c ext.Ctx, ch model.Choice) tea.Cmd {
	effects := model.Plan(ch)
	for _, e := range effects {
		if e.Control != nil && e.Control.Subtype == proto.SubApplyFlagSettings {
			a.sessionEffort = ch.Effort
		}
	}
	return tea.Batch(a.apply(c, effects), c.Notify(ext.Notice{
		Key: "settings.model", Level: ext.NoticeSuccess, Source: "settings", Text: modelNotice(ch),
	}))
}

func modelNotice(ch model.Choice) string {
	s := "Model set to " + ch.Row.Label
	if len(ch.Row.Efforts) > 0 && ch.Effort != "" {
		s += " with " + string(model.Clamp(ch.Effort, ch.Row.Efforts)) + " effort"
	}
	if ch.SessionOnly {
		s += " for this session"
	}
	return s
}

// modelPicker is the /model dialog.
type modelPicker struct {
	a       *area
	rows    []model.Row
	cursor  int
	efforts map[string]model.Effort // effort the user picked per row value
	loading bool
	confirm *model.Choice // waiting for the cache-warning answer
	chosen  *model.Choice
}

func (a *area) newModelPicker(c ext.Ctx, _ any) (ext.Dialog, error) {
	p := &modelPicker{a: a, efforts: map[string]model.Effort{}}
	p.reload(c)
	p.loading = len(a.engine.models) == 0
	return p, nil
}

func (p *modelPicker) reload(c ext.Ctx) {
	var keep string
	if p.cursor < len(p.rows) {
		keep = p.rows[p.cursor].Value
	}
	p.rows = p.a.modelRows(c)
	p.cursor = 0
	for i, r := range p.rows {
		if (keep == "" && r.Current) || (keep != "" && r.Value == keep) {
			p.cursor = i
		}
	}
}

func (p *modelPicker) ID() string               { return modelPickerID }
func (p *modelPicker) Placement() ext.Placement { return ext.PlaceInline }
func (p *modelPicker) KeyContext() string       { return ext.ContextModelPicker }
func (p *modelPicker) Result() any {
	if p.chosen == nil {
		return nil
	}
	return *p.chosen
}

func (p *modelPicker) KeyContexts() []string {
	if p.confirm != nil {
		return []string{ext.ContextConfirmation}
	}
	return []string{ext.ContextModelPicker, ext.ContextSelect}
}

func (p *modelPicker) Init(c ext.Ctx) tea.Cmd {
	if eng := c.Engine(""); eng != nil && eng.Supports(proto.SubListModels) {
		return eng.Control(proto.SubListModels, proto.ListModelsRequest{})
	}
	p.loading = false
	return nil
}

func (p *modelPicker) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.ControlResultMsg); ok && m.Subtype == proto.SubListModels && isMain(m.EngineID) {
		if m.Err == nil {
			var r proto.ModelsResponse
			if json.Unmarshal(m.Resp, &r) == nil && len(r.Models) > 0 {
				p.a.engine.models = model.FromProto(r.Models)
			}
		}
		p.loading = false
		p.reload(c)
		c.Invalidate(p.ID())
	}
	return nil
}

func (p *modelPicker) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	if p.confirm != nil {
		switch id {
		case ext.ActConfirmYes:
			ch := *p.confirm
			p.confirm = nil
			return true, p.finish(c, ch)
		case ext.ActConfirmNo:
			p.confirm = nil
			c.Invalidate(p.ID())
			return true, nil
		}
		return false, nil
	}
	switch id {
	case ext.ActSelectNext:
		p.move(c, 1)
	case ext.ActSelectPrevious:
		p.move(c, -1)
	case ext.ActSelectPageDown:
		p.move(c, 5)
	case ext.ActSelectPageUp:
		p.move(c, -5)
	case ext.ActSelectFirst:
		p.move(c, -len(p.rows))
	case ext.ActSelectLast:
		p.move(c, len(p.rows))
	case ext.ActModelPickerIncreaseEffort:
		p.stepEffort(c, 1)
	case ext.ActModelPickerDecreaseEffort:
		p.stepEffort(c, -1)
	case ext.ActSelectAccept:
		return true, p.choose(c, false)
	case ext.ActModelPickerThisSessionOnly:
		return true, p.choose(c, true)
	case ext.ActSelectCancel, ext.ActConfirmNo:
		return true, c.CloseDialog(dialogModel)
	default:
		return false, nil
	}
	return true, nil
}

func (p *modelPicker) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.confirm != nil {
		return false, nil
	}
	// Digits jump to a row, as numbered lists do elsewhere.
	if n, err := strconv.Atoi(k.String()); err == nil && n >= 1 && n <= len(p.rows) && !p.rows[n-1].Disabled {
		p.cursor = n - 1
		c.Invalidate(p.ID())
		return true, nil
	}
	return false, nil
}

func (p *modelPicker) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

func (p *modelPicker) move(c ext.Ctx, delta int) {
	if len(p.rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	cur := p.cursor
	for n := delta * step; n > 0; n-- {
		next := cur + step
		for next >= 0 && next < len(p.rows) && p.rows[next].Disabled {
			next += step
		}
		if next < 0 || next >= len(p.rows) {
			break
		}
		cur = next
	}
	p.cursor = cur
	c.Invalidate(p.ID())
}

func (p *modelPicker) effortFor(r model.Row) model.Effort {
	if e, ok := p.efforts[r.Value]; ok {
		return e
	}
	return r.Effort.Effort
}

func (p *modelPicker) stepEffort(c ext.Ctx, delta int) {
	if len(p.rows) == 0 {
		return
	}
	r := p.rows[p.cursor]
	if len(r.Efforts) == 0 {
		return
	}
	p.efforts[r.Value] = model.Step(p.effortFor(r), r.Efforts, delta)
	c.Invalidate(p.ID())
}

func (p *modelPicker) choose(c ext.Ctx, sessionOnly bool) tea.Cmd {
	if len(p.rows) == 0 {
		return nil
	}
	r := p.rows[p.cursor]
	if r.Disabled {
		return nil
	}
	ch := model.Choice{Row: r, SessionOnly: sessionOnly}
	if e, ok := p.efforts[r.Value]; ok && e != r.Effort.Effort {
		ch.Effort = e
	}
	if p.needsWarning(c, r) {
		p.confirm = &ch
		c.Invalidate(p.ID())
		return nil
	}
	return p.finish(c, ch)
}

func (p *modelPicker) finish(c ext.Ctx, ch model.Choice) tea.Cmd {
	p.chosen = &ch
	return tea.Sequence(p.a.chooseModel(c, ch), c.CloseDialog(dialogModel))
}

// needsWarning: switching to a different model once the conversation has content
// throws away the warm prompt cache.
func (p *modelPicker) needsWarning(c ext.Ctx, r model.Row) bool {
	if r.Current {
		return false
	}
	if v, ok := c.Settings().Mantle(settingModelSwitchWarning).(bool); ok && !v {
		return false
	}
	tr := c.Transcript()
	return tr != nil && len(tr.Items()) > 0
}

func (p *modelPicker) View(c ext.Ctx, area ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	if p.confirm != nil {
		body := wrap("This conversation is cached for the current model. Switching re-reads all of it "+
			"at full price on the next message, and the cache warms up again from there.", area.Width-4)
		return ext.Rendered{Text: frame(t, area.Width, "Switch to "+p.confirm.Row.Label+"?", "", body,
			hintLine(keyName(c.KeysFor(ext.ContextConfirmation, ext.ActConfirmYes), "enter"), "switch",
				keyName(c.KeysFor(ext.ContextConfirmation, ext.ActConfirmNo), "esc"), "keep the current model"))}
	}
	var body []string
	switch {
	case p.loading && len(p.rows) <= 1:
		body = []string{t.Paint(theme.Inactive, "Loading models…")}
	default:
		rows := make([]listRow, len(p.rows))
		for i, r := range p.rows {
			rows[i] = listRow{Label: r.Label, Detail: r.Description, Current: r.Current, Disabled: r.Disabled}
		}
		body = listLines(t, area.Width-4, rows, p.cursor, listHeight(area.MaxHeight, termH, 9))
		if len(p.rows) > 0 {
			body = append(body, "", p.effortLine(t, p.rows[p.cursor]))
		}
	}
	return ext.Rendered{Text: frame(t, area.Width, "Model",
		"Pick the model for this and future sessions. Use the arrows to set its effort.", body, p.hints(c))}
}

func (p *modelPicker) effortLine(t *theme.Theme, r model.Row) string {
	if len(r.Efforts) == 0 {
		return t.Paint(theme.Inactive, "Effort: not adjustable for this model")
	}
	cur := p.effortFor(r)
	var parts []string
	for _, e := range r.Efforts {
		if e == cur {
			parts = append(parts, t.Paint(theme.Suggestion, "● "+string(e)))
		} else {
			parts = append(parts, t.Paint(theme.Inactive, "○ "+string(e)))
		}
	}
	line := "Effort  " + strings.Join(parts, "  ")
	if r.Thinking == model.ThinkingLocked {
		line += t.Paint(theme.Inactive, "  · thinking always on")
	}
	return line
}

func (p *modelPicker) hints(c ext.Ctx) string {
	return hintLine(
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectAccept), "enter"), "choose",
		keyName(c.KeysFor(ext.ContextModelPicker, ext.ActModelPickerThisSessionOnly), "s"), "use for this session only",
		"←/→", "change effort",
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "cancel",
	)
}

var _ ext.ActionHandler = (*modelPicker)(nil)
var _ ext.ContextStack = (*modelPicker)(nil)
