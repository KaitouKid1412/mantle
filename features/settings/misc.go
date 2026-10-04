package settings

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const (
	dialogTUI         = "dialog.tui"
	dialogAdvisor     = "dialog.advisor"
	dialogAutocompact = "dialog.autocompact"
	dialogSandbox     = "dialog.sandbox"
)

func (a *area) setupMisc(r ext.Registrar) error {
	// /tui: inline or fullscreen renderer.
	r.AddDialog(dialogTUI, a.choiceFactory(dialogTUI, a.tuiChoices))
	r.AddCommand(ext.Command{Name: "tui", Description: "Choose the inline or fullscreen renderer",
		ArgHint: "[default|fullscreen]", Source: ext.SourceBuiltin,
		Run: func(c ext.Ctx, args string) tea.Cmd {
			switch v := strings.ToLower(strings.TrimSpace(args)); v {
			case "":
				return c.OpenDialog(dialogTUI, nil)
			case "default", "fullscreen":
				return a.setTUI(c, v)
			}
			return c.Notify(ext.Notice{Key: "settings.tui", Level: ext.NoticeWarning, Source: "settings", Text: "Use /tui default or /tui fullscreen."})
		}})

	// /advisor and /autocompact: the engine runs them; mantle adds a picker.
	r.AddDialog(dialogAdvisor, a.choiceFactory(dialogAdvisor, a.advisorChoices))
	r.AddCommand(ext.Command{Name: "advisor", Description: "Let Claude consult a stronger model on hard problems",
		ArgHint: "[model|off]", Source: ext.SourceBuiltin, Run: a.passthroughOrPick("advisor", dialogAdvisor)})
	r.AddDialog(dialogAutocompact, a.choiceFactory(dialogAutocompact, a.autocompactChoices))
	r.AddCommand(ext.Command{Name: "autocompact", Description: "Set how full the context gets before it is summarised",
		ArgHint: "[auto|tokens]", Source: ext.SourceBuiltin, Run: a.passthroughOrPick("autocompact", dialogAutocompact)})

	// /sandbox: sandbox mode in local settings, as Claude Code stores it.
	r.AddDialog(dialogSandbox, a.choiceFactory(dialogSandbox, a.sandboxChoices))
	r.AddCommand(ext.Command{Name: "sandbox", Description: "Run shell commands in a sandbox",
		Source: ext.SourceBuiltin, Run: func(c ext.Ctx, _ string) tea.Cmd { return c.OpenDialog(dialogSandbox, nil) }})

	// /restart: the launcher restarts mantle (and resumes the session) on exit 75.
	r.AddCommand(ext.Command{Name: "restart", Description: "Restart mantle and Claude Code, keeping this session",
		ArgHint: "[update]", Source: ext.SourceBuiltin, Run: a.runRestart})
	a.subscribeRestart(r)

	r.AddStory(ext.Story{ID: "settings.sandbox/picker", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		d, _ := storyArea().choiceFactory(dialogSandbox, storyArea().sandboxChoices)(c, nil)
		return d.View(c, ar)
	}})
	return nil
}

// passthroughOrPick sends "/name args" to the engine, or opens the picker without args.
func (a *area) passthroughOrPick(name, dialog string) ext.CommandFunc {
	return func(c ext.Ctx, args string) tea.Cmd {
		if strings.TrimSpace(args) == "" {
			return c.OpenDialog(dialog, nil)
		}
		return a.engineCommand(c, "/"+name+" "+strings.TrimSpace(args))
	}
}

func (a *area) engineCommand(c ext.Ctx, text string) tea.Cmd {
	if eng := c.Engine(""); eng != nil {
		return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(text)}})
	}
	return ext.Msg(noEngineMsg{What: text})
}

// ---- choices ----

type choice struct {
	Label, Detail string
	Current       bool
	Pick          func(ext.Ctx) tea.Cmd
}

type choiceSet struct {
	Title, Subtitle string
	Choices         []choice
}

func (a *area) tuiChoices(c ext.Ctx) choiceSet {
	cur := ext.ClaudeString(c.Settings(), "tui", "default")
	return choiceSet{Title: "Renderer", Subtitle: "How mantle draws the conversation.", Choices: []choice{
		{Label: "Inline", Detail: "Output goes into your terminal's own scrollback", Current: cur != "fullscreen",
			Pick: func(c ext.Ctx) tea.Cmd { return a.setTUI(c, "default") }},
		{Label: "Fullscreen", Detail: "A full-window view with its own scrolling and mouse support", Current: cur == "fullscreen",
			Pick: func(c ext.Ctx) tea.Cmd { return a.setTUI(c, "fullscreen") }},
	}}
}

func (a *area) setTUI(c ext.Ctx, v string) tea.Cmd {
	p := patch.SetKey(patch.User, v, "tui")
	text := "Inline renderer"
	if v == "default" {
		p = patch.DeleteKey(patch.User, "tui")
	} else {
		text = "Fullscreen renderer saved. mantle's fullscreen view is still being built, so mantle stays inline for now."
	}
	return tea.Batch(a.apply(c, []action.Effect{action.PatchEffect(p)}),
		c.Notify(ext.Notice{Key: "settings.tui", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

func (a *area) advisorChoices(c ext.Ctx) choiceSet {
	cur := ext.ClaudeString(c.Settings(), "advisorModel", "")
	set := choiceSet{Title: "Advisor", Subtitle: "A stronger model Claude can ask for a second opinion on hard problems."}
	set.Choices = append(set.Choices, choice{Label: "Off", Detail: "No advisor", Current: cur == "",
		Pick: func(c ext.Ctx) tea.Cmd { return a.engineCommand(c, "/advisor off") }})
	for _, r := range a.modelRows(c) {
		if r.IsDefault || r.Disabled {
			continue
		}
		v := r.Value
		set.Choices = append(set.Choices, choice{Label: r.Label, Detail: r.Description, Current: cur != "" && r.Info.SameModel(cur),
			Pick: func(c ext.Ctx) tea.Cmd { return a.engineCommand(c, "/advisor "+v) }})
	}
	return set
}

// autocompactSizes are the windows offered besides "auto", in tokens.
var autocompactSizes = []int{200_000, 400_000, 600_000, 800_000, 1_000_000}

func (a *area) autocompactChoices(c ext.Ctx) choiceSet {
	cur := ""
	if v, ok := c.Settings().Claude("autoCompactWindow"); ok {
		cur = fmt.Sprint(v)
	}
	set := choiceSet{Title: "Auto-compact window", Subtitle: "How full the context gets before Claude summarises it."}
	set.Choices = append(set.Choices, choice{Label: "Auto", Detail: "Let Claude Code decide for the model", Current: cur == "" || cur == "auto",
		Pick: func(c ext.Ctx) tea.Cmd { return a.engineCommand(c, "/autocompact auto") }})
	for _, n := range autocompactSizes {
		v := strconv.Itoa(n)
		set.Choices = append(set.Choices, choice{Label: fmt.Sprintf("%dk tokens", n/1000), Current: cur == v,
			Pick: func(c ext.Ctx) tea.Cmd { return a.engineCommand(c, "/autocompact "+v) }})
	}
	return set
}

func (a *area) sandboxChoices(c ext.Ctx) choiceSet {
	sb, _ := settingsDoc(c, "sandbox")["sandbox"].(map[string]any)
	on, _ := sb["enabled"].(bool)
	auto, set := sb["autoAllowBashIfSandboxed"].(bool)
	autoOn := on && (!set || auto)
	pick := func(enabled bool, autoAllow *bool) func(ext.Ctx) tea.Cmd {
		return func(c ext.Ctx) tea.Cmd {
			ops := []patch.Op{{Kind: patch.Set, Path: []string{"sandbox", "enabled"}, Value: enabled}}
			if autoAllow != nil {
				ops = append(ops, patch.Op{Kind: patch.Set, Path: []string{"sandbox", "autoAllowBashIfSandboxed"}, Value: *autoAllow})
			}
			text := "Sandbox off"
			if enabled {
				text = "Sandbox on"
			}
			return tea.Batch(a.apply(c, []action.Effect{action.PatchEffect(patch.Patch{Scope: patch.Local, Ops: ops})}),
				c.Notify(ext.Notice{Key: "settings.sandbox", Level: ext.NoticeSuccess, Source: "settings", Text: text + " for this project"}))
		}
	}
	yes, no := true, false
	return choiceSet{Title: "Sandbox", Subtitle: "Run shell commands with restricted file and network access. Saved for this project, just you.",
		Choices: []choice{
			{Label: "Sandbox, run without asking", Detail: "Sandboxed commands skip the permission prompt", Current: autoOn, Pick: pick(true, &yes)},
			{Label: "Sandbox, still ask", Detail: "Commands are sandboxed and permission rules apply as usual", Current: on && !autoOn, Pick: pick(true, &no)},
			{Label: "No sandbox", Detail: "Commands run normally", Current: !on, Pick: pick(false, nil)},
		}}
}

// ---- /restart ----

// runRestart exits with the restart code; with "update" it runs "claude update" first.
func (a *area) runRestart(c ext.Ctx, args string) tea.Cmd {
	exit := ext.Msg(ext.ExitMsg{Code: ext.ExitRestart, Reason: "restart"})
	if strings.EqualFold(strings.TrimSpace(args), "update") {
		cmd, err := claudecli.Default.UpdateCmd()
		if err != nil {
			return c.Notify(ext.Notice{Key: "settings.restart", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
		}
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return restartFailedMsg{err}
			}
			return ext.ExitMsg{Code: ext.ExitRestart, Reason: "restart after update"}
		})
	}
	return tea.Sequence(c.Notify(ext.Notice{Key: "settings.restart", Source: "settings", Text: "Restarting…"}), exit)
}

// restartFailedMsg reports a failed "claude update"; mantle keeps running.
type restartFailedMsg struct{ err error }

func (a *area) subscribeRestart(r ext.Registrar) {
	ext.Subscribe(r, "settings.restart-failed", func(c ext.Ctx, m restartFailedMsg) tea.Cmd {
		return c.Notify(ext.Notice{Key: "settings.restart", Level: ext.NoticeError, Source: "settings",
			Text: "Update failed, so mantle did not restart: " + m.err.Error()})
	})
}

// ---- the choice dialog ----

type choiceDialog struct {
	id, dialogID string
	build        func(ext.Ctx) choiceSet
	set          choiceSet
	cursor       int
}

func (a *area) choiceFactory(dialogID string, build func(ext.Ctx) choiceSet) ext.DialogFactory {
	return func(c ext.Ctx, _ any) (ext.Dialog, error) {
		d := &choiceDialog{id: "settings." + strings.TrimPrefix(dialogID, "dialog.") + ".picker", dialogID: dialogID, build: build}
		d.set = build(c)
		for i, ch := range d.set.Choices {
			if ch.Current {
				d.cursor = i
			}
		}
		return d, nil
	}
}

func (d *choiceDialog) ID() string                                         { return d.id }
func (d *choiceDialog) Placement() ext.Placement                           { return ext.PlaceInline }
func (d *choiceDialog) KeyContext() string                                 { return ext.ContextSelect }
func (d *choiceDialog) Init(ext.Ctx) tea.Cmd                               { return nil }
func (d *choiceDialog) Update(ext.Ctx, tea.Msg) tea.Cmd                    { return nil }
func (d *choiceDialog) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (d *choiceDialog) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }

func (d *choiceDialog) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActSelectNext:
		d.cursor = min(d.cursor+1, len(d.set.Choices)-1)
	case ext.ActSelectPrevious:
		d.cursor = max(d.cursor-1, 0)
	case ext.ActSelectFirst, ext.ActSelectPageUp:
		d.cursor = 0
	case ext.ActSelectLast, ext.ActSelectPageDown:
		d.cursor = len(d.set.Choices) - 1
	case ext.ActSelectAccept:
		ch := d.set.Choices[d.cursor]
		return true, tea.Sequence(c.CloseDialog(d.dialogID), ch.Pick(c))
	case ext.ActSelectCancel:
		return true, c.CloseDialog(d.dialogID)
	default:
		return false, nil
	}
	c.Invalidate(d.ID())
	return true, nil
}

func (d *choiceDialog) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	rows := make([]listRow, len(d.set.Choices))
	for i, ch := range d.set.Choices {
		rows[i] = listRow{Label: ch.Label, Detail: ch.Detail, Current: ch.Current}
	}
	body := listLines(t, ar.Width-4, rows, d.cursor, listHeight(ar.MaxHeight, termH, 8))
	return ext.Rendered{Text: frame(t, ar.Width, d.set.Title, d.set.Subtitle, body,
		hintLine(keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectAccept), "enter"), "choose",
			keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "cancel"))}
}

var _ ext.ActionHandler = (*choiceDialog)(nil)
