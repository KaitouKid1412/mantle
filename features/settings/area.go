package settings

import (
	"context"
	"encoding/json"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/termsetup"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// area is the state the settings features share. It lives on the UI goroutine: only
// Setup callbacks, subscribers, commands, actions and dialogs touch it.
type area struct {
	// env locates settings files; filled lazily from the process and the session.
	env func(ext.Ctx) patch.Env
	// write applies a settings patch. It runs inside a Cmd (off the UI goroutine), so
	// it must not touch area state.
	write func(env patch.Env, p patch.Patch) (string, error)

	engine engineState
	// mantleSpecs are the SettingSpecs this area registers, for /config's mantle
	// section (the host does not list other features' specs yet).
	mantleSpecs []ext.SettingSpec

	// autoModeDefaults lists the auto-mode classifier's built-in rules through plan 09's
	// safe claude runner (never hand-built argv); tests replace it.
	autoModeDefaults func(ctx context.Context, dir string) (AutoModeRules, error)

	// lastVisibility is the command overlay last sent to the host.
	lastVisibility map[string]bool

	// endPreviewPending: a confirmed /theme preview waits for the settings reload.
	endPreviewPending bool

	// openEditor runs $EDITOR on a file (tea.ExecProcess); tests replace it.
	openEditor func(path string, done func(error) tea.Msg) tea.Cmd
	// termLoad and termApply run the /terminal-setup installers; tests replace them.
	termLoad  func(home string, kittyKeyboard bool) (termsetup.Proposal, error)
	termApply func(home string, p termsetup.Proposal) (string, error)
	// kittyKeyboard: the terminal confirmed the kitty keyboard protocol.
	kittyKeyboard bool

	// Session overrides applied with apply_flag_settings (nil/"" = none).
	sessionEffort    model.Effort
	sessionUltracode *bool
	sessionFast      *bool
	sessionThinking  *bool

	// newerOffer is the successor offered for the saved default model (ctrl+y).
	newerOffer      *model.Row
	newerOfferedFor string
}

// engineState caches what the main engine reported, for panels that open before a
// fresh control request answers.
type engineState struct {
	init        *proto.InitializeResponse
	sys         *proto.SystemInit
	models      []model.Info
	unavailable []model.Info
	commands    []proto.SlashCommand
	styles      []string
}

func newArea() *area {
	return &area{env: processEnv, write: writeFile, openEditor: defaultOpenEditor,
		termLoad: defaultTermLoad, termApply: defaultTermApply, autoModeDefaults: claudeAutoModeDefaults}
}

// claudeAutoModeDefaults runs "claude auto-mode defaults" in dir (it prints config and
// never calls the model).
func claudeAutoModeDefaults(ctx context.Context, dir string) (AutoModeRules, error) {
	r := *claudecli.Default
	r.Dir = dir
	rules, err := r.AutoModeDefaults(ctx, "")
	return AutoModeRules(rules), err
}

// processEnv reads HOME, CLAUDE_CONFIG_DIR and the session directory.
func processEnv(c ext.Ctx) patch.Env {
	home, _ := os.UserHomeDir()
	e := patch.Env{Home: home, ConfigDir: os.Getenv("CLAUDE_CONFIG_DIR")}
	if c != nil {
		e.ProjectRoot = c.Session().Cwd
	}
	if e.ProjectRoot == "" {
		e.ProjectRoot, _ = os.Getwd()
	}
	return e
}

// writeFile applies a patch to its scope's file through plan 01's config writer
// (locked, merge-on-write, order-preserving, never ~/.claude.json).
func writeFile(env patch.Env, p patch.Patch) (string, error) {
	path, err := env.Path(p.Scope)
	if err != nil {
		return "", err
	}
	w := config.Writer{Paths: config.PathsFor(env.Home, env.ProjectRoot, env.ConfigDir, "")}
	return path, w.Update(path, p.Apply)
}

// subscribe keeps engineState current. Registered once, by the core settings feature.
func (a *area) subscribe(r ext.Registrar) {
	ext.Subscribe(r, "settings.engine-results", func(c ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
		if !isMain(m.EngineID) {
			return nil
		}
		if m.Err != nil {
			return a.controlFailed(c, m)
		}
		a.observeControl(m)
		if m.Subtype == proto.SubInitialize || m.Subtype == proto.SubListModels {
			return tea.Batch(a.visibilityCmd(), a.offerNewerModel(c))
		}
		return nil
	})
	ext.Subscribe(r, "settings.engine-events", func(c ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
		if !isMain(m.EngineID) {
			return nil
		}
		a.observeEvent(m.Event)
		if _, ok := m.Event.(*proto.CommandsChanged); ok {
			return a.visibilityCmd()
		}
		return nil
	})
	// A new engine process starts without the old session's flag settings.
	ext.Subscribe(r, "settings.engine-attach", func(c ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
		if isMain(m.EngineID) {
			a.resetSession()
		}
		return nil
	})
}

// addSetting registers a mantle setting and remembers it for /config.
func (a *area) addSetting(r ext.Registrar, s ext.SettingSpec) {
	r.AddSetting(s)
	a.mantleSpecs = append(a.mantleSpecs, s)
}

func (a *area) resetSession() {
	a.sessionEffort = ""
	a.sessionUltracode, a.sessionFast, a.sessionThinking = nil, nil, nil
	a.engine.sys = nil
}

func isMain(id string) bool { return id == "" || id == ext.MainEngine }

func (a *area) observeControl(m ext.ControlResultMsg) {
	switch m.Subtype {
	case proto.SubInitialize:
		var r proto.InitializeResponse
		if json.Unmarshal(m.Resp, &r) == nil {
			a.engine.init = &r
			a.engine.models = model.FromProto(r.Models)
			a.engine.unavailable = model.ParseUnavailable(r.UnavailableModels)
			a.engine.commands = r.Commands
			a.engine.styles = r.AvailableOutputStyles
		}
	case proto.SubListModels:
		var r proto.ModelsResponse
		if json.Unmarshal(m.Resp, &r) == nil && len(r.Models) > 0 {
			a.engine.models = model.FromProto(r.Models)
		}
	case proto.SubReloadOutputStyles:
		var r proto.OutputStylesResponse
		if json.Unmarshal(m.Resp, &r) == nil {
			a.engine.styles = r.AvailableOutputStyles
		}
	}
}

func (a *area) observeEvent(ev proto.Event) {
	switch e := ev.(type) {
	case *proto.SystemInit:
		a.engine.sys = e
	case *proto.CommandsChanged:
		a.engine.commands = e.Commands
	}
}

// currentModel is the session model: the newest system/init, else the host's session
// info, else "" (the default).
func (a *area) currentModel(c ext.Ctx) string {
	if a.engine.sys != nil && a.engine.sys.Model != "" {
		return a.engine.sys.Model
	}
	return c.Session().Model
}

// settingsDoc is the merged Claude Code settings for the keys panels read.
func settingsDoc(c ext.Ctx, keys ...string) map[string]any {
	doc := map[string]any{}
	for _, k := range keys {
		if v, ok := c.Settings().Claude(k); ok {
			doc[k] = v
		}
	}
	return doc
}
