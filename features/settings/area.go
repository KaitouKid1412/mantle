package settings

import (
	"encoding/json"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/settingsfile"
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

	// endPreviewPending: a confirmed /theme preview waits for the settings reload.
	endPreviewPending bool

	// Session overrides applied with apply_flag_settings (nil/"" = none).
	sessionEffort    model.Effort
	sessionUltracode *bool
	sessionFast      *bool
	sessionThinking  *bool
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
	return &area{env: processEnv, write: writeFile}
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

func writeFile(env patch.Env, p patch.Patch) (string, error) {
	w := settingsfile.Writer{Env: env, LockDir: filepath.Join(env.Home, ".mantle", "locks")}
	return w.Apply(p)
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
		return nil
	})
	ext.Subscribe(r, "settings.engine-events", func(c ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
		if isMain(m.EngineID) {
			a.observeEvent(m.Event)
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
