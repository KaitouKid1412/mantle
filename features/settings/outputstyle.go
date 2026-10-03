package settings

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const (
	dialogOutputStyle = "dialog.outputStyle"
	outputStyleID     = "settings.outputStyle.picker"
)

// styleText describes the built-in output styles in mantle's words.
var styleText = map[string]string{
	"default":     "Concise answers focused on getting the work done",
	"explanatory": "Explains choices and trade-offs as it works",
	"learning":    "Teaches as it goes and leaves small tasks for you",
	"concise":     "As short as possible",
	"proactive":   "Takes initiative on follow-up work",
}

func (a *area) setupOutputStyle(r ext.Registrar) error {
	r.AddDialog(dialogOutputStyle, a.newStylePicker)
	r.AddCommand(ext.Command{
		Name: "output-style", Description: "Choose how responses are written", ArgHint: "[style]",
		Source: ext.SourceBuiltin, Run: a.runOutputStyleCommand,
		Complete: func(c ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, s := range a.styles(c) {
				if strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix)) {
					out = append(out, ext.Completion{Value: s, Description: styleText[strings.ToLower(s)]})
				}
			}
			return out
		},
	})
	r.AddStory(ext.Story{ID: "settings.outputStyle/picker", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		a := storyArea()
		a.engine.styles = []string{"default", "Explanatory", "Learning", "Concise", "Proactive", "pirate"}
		d, _ := a.newStylePicker(c, nil)
		return d.View(c, ar)
	}})
	return nil
}

func (a *area) styles(c ext.Ctx) []string {
	if len(a.engine.styles) > 0 {
		return a.engine.styles
	}
	return []string{"default", "Explanatory", "Learning", "Concise", "Proactive"}
}

func (a *area) currentStyle(c ext.Ctx) string {
	switch {
	case a.engine.sys != nil && a.engine.sys.OutputStyle != "":
		return a.engine.sys.OutputStyle
	case a.engine.init != nil && a.engine.init.OutputStyle != "":
		return a.engine.init.OutputStyle
	case c.Session().OutputStyle != "":
		return c.Session().OutputStyle
	}
	return "default"
}

// runOutputStyleCommand opens the picker; with a name the engine's own command runs.
func (a *area) runOutputStyleCommand(c ext.Ctx, args string) tea.Cmd {
	if args = strings.TrimSpace(args); args == "" {
		return c.OpenDialog(dialogOutputStyle, nil)
	}
	if eng := c.Engine(""); eng != nil {
		return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("/output-style " + args)}})
	}
	return ext.Msg(noEngineMsg{What: "/output-style"})
}

// styleEffects saves the style to local settings (where the engine keeps it) and asks
// the engine to reload its styles.
func styleEffects(name string) []action.Effect {
	return []action.Effect{
		action.ControlEffect(proto.SubUpdateSettings, map[string]any{
			"source": string(patch.Local), "settings": map[string]any{"outputStyle": name}}),
		action.ControlEffect(proto.SubReloadOutputStyles, map[string]any{}),
	}
}

type stylePicker struct {
	a      *area
	styles []string
	cursor int
	cur    string
}

func (a *area) newStylePicker(c ext.Ctx, _ any) (ext.Dialog, error) {
	p := &stylePicker{a: a}
	p.reload(c)
	return p, nil
}

func (p *stylePicker) reload(c ext.Ctx) {
	p.styles = p.a.styles(c)
	p.cur = p.a.currentStyle(c)
	for i, s := range p.styles {
		if strings.EqualFold(s, p.cur) {
			p.cursor = i
		}
	}
}

func (p *stylePicker) ID() string                                         { return outputStyleID }
func (p *stylePicker) Placement() ext.Placement                           { return ext.PlaceInline }
func (p *stylePicker) KeyContext() string                                 { return ext.ContextSelect }
func (p *stylePicker) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (p *stylePicker) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }

func (p *stylePicker) Init(c ext.Ctx) tea.Cmd {
	if eng := c.Engine(""); eng != nil && eng.Supports(proto.SubReloadOutputStyles) {
		return eng.Control(proto.SubReloadOutputStyles, map[string]any{})
	}
	return nil
}

func (p *stylePicker) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.ControlResultMsg); ok && m.Subtype == proto.SubReloadOutputStyles && m.Err == nil && isMain(m.EngineID) {
		var r proto.OutputStylesResponse
		if json.Unmarshal(m.Resp, &r) == nil && len(r.AvailableOutputStyles) > 0 {
			p.a.engine.styles = r.AvailableOutputStyles
			keep := p.styles[p.cursor]
			p.reload(c)
			for i, s := range p.styles {
				if s == keep {
					p.cursor = i
				}
			}
			c.Invalidate(p.ID())
		}
	}
	return nil
}

func (p *stylePicker) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActSelectNext:
		p.cursor = min(p.cursor+1, len(p.styles)-1)
	case ext.ActSelectPrevious:
		p.cursor = max(p.cursor-1, 0)
	case ext.ActSelectFirst, ext.ActSelectPageUp:
		p.cursor = 0
	case ext.ActSelectLast, ext.ActSelectPageDown:
		p.cursor = len(p.styles) - 1
	case ext.ActSelectAccept:
		name := p.styles[p.cursor]
		if strings.EqualFold(name, p.cur) {
			return true, c.CloseDialog(dialogOutputStyle)
		}
		return true, tea.Sequence(p.a.apply(c, styleEffects(name)), c.CloseDialog(dialogOutputStyle),
			c.Notify(ext.Notice{Key: "settings.outputStyle", Level: ext.NoticeSuccess, Source: "settings",
				Text: "Output style set to " + name + " for this project"}))
	case ext.ActSelectCancel:
		return true, c.CloseDialog(dialogOutputStyle)
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

func (p *stylePicker) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	rows := make([]listRow, len(p.styles))
	for i, s := range p.styles {
		d := styleText[strings.ToLower(s)]
		if d == "" {
			d = "Custom style"
		}
		rows[i] = listRow{Label: s, Detail: d, Current: strings.EqualFold(s, p.cur)}
	}
	body := listLines(t, ar.Width-4, rows, p.cursor, listHeight(ar.MaxHeight, termH, 8))
	return ext.Rendered{Text: frame(t, ar.Width, "Output style",
		"How Claude writes its answers. Saved for this project; custom styles live in output-styles folders.", body,
		hintLine(keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectAccept), "enter"), "choose",
			keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "cancel"))}
}

var _ ext.ActionHandler = (*stylePicker)(nil)
