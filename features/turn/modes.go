package turn

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/features/turn/mode"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// modeState is what the feature knows about one engine's permission modes.
type modeState struct {
	target      string // requested with set_permission_mode, not yet confirmed
	userMode    string // last mode the user chose (re-applied after an engine restart)
	bypass      bool   // engine started with bypass allowed
	autoKnown   bool   // models answered
	auto        bool   // current model supports auto mode
	flags       gates.LaunchFlags
	startupDone bool
}

func (st *state) mode(engineID string) *modeState {
	engineID = engineKey(engineID)
	ms, ok := st.modes[engineID]
	if !ok {
		ms = &modeState{}
		st.modes[engineID] = ms
	}
	return ms
}

func (st *state) setupModes(r ext.Registrar) {
	ext.Subscribe(r, "turn.autoNotice", st.printAutoNotice)
	r.AddAction(ext.Action{
		ID: ext.ActChatCycleMode, Context: ext.ContextChat,
		Description: "Cycle the permission mode",
		Run:         func(c ext.Ctx) (bool, tea.Cmd) { return true, st.cycleMode(c, ext.MainEngine) },
	})
}

// currentMode is the mode the engine is in, or the one just requested.
func (st *state) currentMode(c ext.Ctx, engineID string) string {
	if t := st.mode(engineID).target; t != "" {
		return t
	}
	if m := st.session(c, engineID).PermissionMode; m != "" {
		return m
	}
	return string(mode.Default)
}

// availability is which optional modes the cycle may enter for an engine.
func (st *state) availability(c ext.Ctx, engineID string) mode.Availability {
	ms := st.mode(engineID)
	a := mode.Availability{Bypass: ms.bypass, Auto: ms.auto}
	if mode.Normalize(mode.Mode(st.session(c, engineID).PermissionMode)) == mode.BypassPermissions {
		a.Bypass = true
	}
	if strings.EqualFold(settingString(c, "permissions.disableBypassPermissionsMode"), "disable") {
		a.Bypass = false
	}
	if strings.EqualFold(settingString(c, "permissions.disableAutoMode"), "disable") {
		a.Auto = false
	}
	return a
}

// cycleMode switches an engine to the next mode (shift+tab).
func (st *state) cycleMode(c ext.Ctx, engineID string) tea.Cmd {
	if c.Engine(engineID) == nil {
		return nil
	}
	next := mode.Next(mode.Mode(st.currentMode(c, engineID)), st.availability(c, engineID))
	st.mode(engineID).userMode = string(next)
	return st.setMode(c, engineID, next)
}

func (st *state) setMode(c ext.Ctx, engineID string, m mode.Mode) tea.Cmd {
	eng := c.Engine(engineID)
	if eng == nil {
		return nil
	}
	st.mode(engineID).target = string(m)
	return eng.Control(proto.SubSetPermissionMode, proto.SetPermissionModeRequest{Mode: string(m)})
}

// publishMode tells the host (and the footer) about a confirmed mode change.
func (st *state) publishMode(c ext.Ctx, engineID, m string) tea.Cmd {
	if mode.Normalize(mode.Mode(m)) == mode.BypassPermissions {
		st.mode(engineID).bypass = true
	}
	info := st.session(c, engineID)
	info.EngineID = engineID
	info.PermissionMode = m
	return ext.Msg(ext.SessionChangedMsg{EngineID: engineID, Info: info})
}

func (st *state) setModels(c ext.Ctx, engineID string, models []proto.ModelInfo) {
	ms := st.mode(engineID)
	ms.autoKnown = true
	ms.auto = supportsAuto(models, st.session(c, engineID).Model)
}

// supportsAuto reports whether the session's model supports auto mode: the matching
// model's flag, or any listed model's when the session model isn't listed.
func supportsAuto(models []proto.ModelInfo, current string) bool {
	some := false
	for _, m := range models {
		if current != "" && (m.Value == current || m.ResolvedModel == current) {
			return m.SupportsAutoMode
		}
		some = some || m.SupportsAutoMode
	}
	return some
}

// onAttach resets a (re)started engine's mode state. Auto-mode availability comes from
// the initialize response, which the engine bridge delivers after the attach. mantle
// sends no control request of its own here: the engine's first stdin traffic stays the
// bridge's handshake and the user's first prompt.
func (st *state) onAttach(c ext.Ctx, engineID string, e ext.Engine) tea.Cmd {
	ms := st.mode(engineID)
	ms.target = ""
	ms.startupDone = false
	ms.autoKnown = false
	st.running[engineID] = false
	return nil
}

// maybeStartupMode applies the interactive starting mode once the main engine reports
// its mode and models. Headless claude starts in default; interactive Claude Code
// starts in auto when available, honouring --permission-mode and
// permissions.defaultMode. After a restart the user's last choice is restored.
func (st *state) maybeStartupMode(c ext.Ctx, engineID string) tea.Cmd {
	if engineID != ext.MainEngine {
		return nil
	}
	ms := st.mode(engineID)
	cur := st.session(c, engineID).PermissionMode
	if ms.startupDone || !ms.autoKnown || cur == "" || c.Engine(engineID) == nil {
		return nil
	}
	ms.startupDone = true
	avail := st.availability(c, engineID)
	var desired mode.Mode
	if ms.userMode != "" {
		desired = mode.Normalize(mode.Mode(ms.userMode))
	} else {
		desired = mode.Startup(mode.StartupInput{
			Flag:            ms.flags.PermissionMode,
			DangerouslySkip: ms.flags.DangerouslySkip,
			SettingsDefault: settingString(c, "permissions.defaultMode"),
			Availability:    avail,
			PreferAuto:      true,
		})
	}
	var notice tea.Cmd
	explicit := ms.userMode != "" || ms.flags.PermissionMode != "" || ms.flags.DangerouslySkip ||
		settingString(c, "permissions.defaultMode") != ""
	if desired == mode.Auto && !explicit {
		notice = st.autoNotice(c)
	}
	if mode.Normalize(mode.Mode(cur)) == desired {
		return notice
	}
	return tea.Batch(notice, st.setMode(c, engineID, desired))
}

// autoNoticeText tells the user, once, that auto mode is the default. mantle's own
// wording.
var autoNoticeText = []string{
	"Auto mode is now the default permission mode.",
	"Claude runs routine tool calls without asking and screens risky ones before they run;",
	"press shift+tab to pick another mode, or set permissions.defaultMode.",
}

// autoNoticeMsg prints the auto-mode notice. It is a message of its own so the notice
// lands after the startup banner, which is printed for the same initialize result.
type autoNoticeMsg struct{}

// autoNotice schedules the auto-mode default notice the first time it applies.
func (st *state) autoNotice(c ext.Ctx) tea.Cmd {
	env, err := st.env()
	if err != nil || gates.LoadGateStore(env).AutoNoticeAt != "" {
		return nil
	}
	return ext.Msg(autoNoticeMsg{})
}

func (st *state) printAutoNotice(c ext.Ctx, _ autoNoticeMsg) tea.Cmd {
	env, err := st.env()
	if err != nil || gates.LoadGateStore(env).AutoNoticeAt != "" {
		return nil
	}
	record := func() tea.Msg {
		_ = gates.RecordAutoNotice(env)
		return nil
	}
	lines := append(append([]string{""}, autoNoticeText...), "")
	return tea.Batch(c.Print(strings.Join(lines, "\n")), record)
}

func (st *state) onModeResult(c ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	eng := engineKey(m.EngineID)
	ms := st.mode(eng)
	requested := ms.target
	ms.target = ""
	if m.Err != nil {
		if requested == string(mode.Auto) {
			ms.auto = false
		}
		label := "that"
		if requested != "" {
			label = strings.ToLower(mode.Label(mode.Mode(requested)))
		}
		return c.Notify(ext.Notice{Key: "turn.mode", Text: "Couldn't switch to " + label + " mode: " + m.Err.Error(), Level: ext.NoticeWarning, Source: FeatureID})
	}
	var resp proto.SetPermissionModeRequest
	_ = json.Unmarshal(m.Resp, &resp)
	if resp.Mode == "" {
		resp.Mode = requested
	}
	if resp.Mode == "" {
		return nil
	}
	return st.publishMode(c, eng, resp.Mode)
}

// settingString reads a merged Claude Code setting by dotted path
// ("permissions.defaultMode"), as a whole key or by walking objects.
func settingString(c ext.Ctx, key string) string {
	s, _ := settingValue(c, key).(string)
	return s
}

func settingValue(c ext.Ctx, key string) any {
	s := c.Settings()
	if s == nil {
		return nil
	}
	if v, ok := s.Claude(key); ok {
		return v
	}
	parts := strings.Split(key, ".")
	v, ok := s.Claude(parts[0])
	if !ok {
		return nil
	}
	return dig(v, parts[1:])
}

func dig(v any, path []string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		if v, ok = m[p]; !ok {
			return nil
		}
	}
	return v
}

// userScopedBool reads a boolean only from scopes a repository can't ship (user, flag,
// policy), when the host exposes scopes; otherwise from the merged settings.
func userScopedBool(c ext.Ctx, key string) bool {
	if ss, ok := c.Settings().(ext.ScopedSettings); ok {
		for _, scope := range []string{ext.ScopePolicy, ext.ScopeFlag, ext.ScopeUser} {
			if b, ok := dig(ss.ClaudeScope(scope), strings.Split(key, ".")).(bool); ok {
				return b
			}
		}
		return false
	}
	b, _ := settingValue(c, key).(bool)
	return b
}
