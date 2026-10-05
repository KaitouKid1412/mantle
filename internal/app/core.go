package app

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/ui"
)

// ExitConfirmWindow is how long a second ctrl+c (or ctrl+d) has to follow the first.
const ExitConfirmWindow = 2 * time.Second

// CoreFeatures are the host's own registrations, set up before every built-in. They
// can be overridden like any built-in (Replace("app:interrupt", …)).
func CoreFeatures() []ext.Feature {
	return []ext.Feature{{
		ID:     "core.app",
		Order:  -100,
		Parity: []string{"MT-01", "MT-02", "MT-03", "MT-04", "MT-05", "MT-08", "MT-09", "CF-01", "CF-02", "CF-03", "CF-04", "CF-05", "CF-07", "CF-08", "CF-09", "CF-10", "CF-11", "CF-12", "CF-13", "CF-06", "CF-14", "CF-15", "CF-16", "CF-17", "CF-18", "CF-19", "CF-20", "MT-06"},
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotAboveInput, &noticesComp{}, ext.SlotOpts{Weight: 1000, MaxHeight: 5})
			r.AddAction(ext.Action{
				ID: ext.ActAppInterrupt, Context: ext.ContextGlobal,
				Description: "Interrupt; press twice to exit",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, confirmExit(c, "ctrl+c") },
			})
			r.AddAction(ext.Action{
				ID: ext.ActAppExit, Context: ext.ContextGlobal,
				Description: "Exit (press twice)",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, confirmExit(c, "ctrl+d") },
			})
			r.AddAction(ext.Action{
				ID: ext.ActAppRedraw, Context: ext.ContextGlobal,
				Description: "Redraw the screen",
				Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, c.Reprint() },
			})
			r.AddPromptStage("core.send", CoreSendPriority, coreSend)
			r.OnStart("core.validate", validateConfig)
			return nil
		},
	}, {
		ID:     "core.widgets",
		Order:  -99,
		Parity: []string{"MT-07"},
		Setup: func(r ext.Registrar) error {
			for _, s := range ui.Stories() {
				r.AddStory(s)
			}
			return nil
		},
	}}
}

// confirmExit exits on the second press within ExitConfirmWindow, and otherwise shows
// a hint. Features that give ctrl+c a meaning (interrupt a turn, clear the prompt)
// handle the action first through HandleAction or Wrap.
func confirmExit(c ext.Ctx, key string) tea.Cmd {
	uc, ok := c.(*uiCtx)
	if !ok {
		return ext.Msg(ext.ExitMsg{Code: 0, Reason: key})
	}
	r := uc.r
	now := r.opts.Clock.Now()
	if !r.interrupt.IsZero() && now.Sub(r.interrupt) <= ExitConfirmWindow {
		r.interrupt = time.Time{}
		return ext.Msg(ext.ExitMsg{Code: 0, Reason: key})
	}
	r.interrupt = now
	return r.addNotice(ext.Notice{Key: "core.exit", Text: "Press " + key + " again to exit", Level: ext.NoticeInfo, Timeout: ExitConfirmWindow, Source: "core"})
}

// CoreSendPriority is the core send stage's priority: after every feature stage.
const CoreSendPriority = math.MaxInt32

// coreSend is the fallback last prompt stage: if no feature consumed the draft (the
// input feature's own send stage normally does), send its text to the main engine as
// is. It keeps prompts working with -tags no_input and for drafts submitted before
// the input feature exists.
func coreSend(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	text := strings.TrimSpace(d.Text)
	if text == "" || d.Mode == "bash" {
		return ext.Continue, nil
	}
	p := ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(d.Text)}, Priority: d.Priority, UUID: uuid.NewString()}
	e := c.Engine(ext.MainEngine)
	if e == nil {
		// Typed while claude is still starting (startup gates, spawn): queue it and
		// send it when the main engine attaches, as a user would expect.
		if r := rootOf(c); r != nil {
			r.pendingPrompts = append(r.pendingPrompts, p)
			return ext.Consumed, c.Notify(ext.Notice{Key: "core.send", Text: "Waiting for claude to start…", Level: ext.NoticeInfo, Source: "core"})
		}
		return ext.Reject, c.Notify(ext.Notice{Key: "core.send", Text: "claude is not running yet", Level: ext.NoticeWarning, Source: "core"})
	}
	return ext.Consumed, e.Send(p)
}

// flushPendingPrompts sends prompts queued before the main engine attached.
func (r *Root) flushPendingPrompts() tea.Cmd {
	e := r.engines[ext.MainEngine]
	if e == nil || len(r.pendingPrompts) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, p := range r.pendingPrompts {
		cmds = append(cmds, e.Send(p))
	}
	r.pendingPrompts = nil
	r.expireNotice(noticeExpireMsg{key: "core.send", seq: r.noticeSeq("core.send")})
	return tea.Sequence(cmds...)
}

// validateConfig reports configuration problems once at startup (CF-18): invalid
// settings files (Claude Code ignores them silently in -p), deprecated keys, an
// invalid ~/.mantle/settings.json, and keybinding errors in the user's files.
func validateConfig(c ext.Ctx) tea.Cmd {
	var cmds []tea.Cmd
	warn := func(key, text string) {
		cmds = append(cmds, c.Notify(ext.Notice{Key: key, Text: text, Level: ext.NoticeWarning, Timeout: 15 * time.Second, Source: "core"}))
	}
	if ss, ok := c.Settings().(ext.ScopedSettings); ok {
		for _, src := range ss.ClaudeSources() {
			if src.Err != nil {
				where := src.Path
				if where == "" {
					where = src.Scope
				}
				warn("settings:"+src.Scope, "Ignoring invalid settings in "+where+": "+src.Err.Error())
			}
		}
	}
	if _, ok := c.Settings().Claude("keybindingFlavor"); ok {
		warn("settings:keybindingFlavor", "keybindingFlavor is deprecated and has no effect; word editing always follows readline")
	}
	if r := rootOf(c); r != nil {
		if s, ok := r.configStore(); ok && s.Snapshot().MantleErr != nil {
			warn("settings:mantle", "Ignoring ~/.mantle/settings.json: "+s.Snapshot().MantleErr.Error())
		}
		var errs []string
		for _, i := range r.resolver.Keymap().Issues {
			if i.Severity == "error" && i.Source != "defaults" && i.Source != "features" {
				errs = append(errs, i.Keys+": "+i.Message)
			}
		}
		if len(errs) > 0 {
			text := "keybindings: " + errs[0]
			if len(errs) > 1 {
				text += " (+" + itoa(len(errs)-1) + " more; see mantle-ui catalog)"
			}
			warn("keybindings", text)
		}
	}
	return tea.Batch(cmds...)
}

// rootOf returns the root behind a host ctx (nil for other ctx implementations).
func rootOf(c ext.Ctx) *Root {
	if uc, ok := c.(*uiCtx); ok {
		return uc.r
	}
	return nil
}
