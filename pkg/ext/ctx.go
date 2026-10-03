package ext

import (
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Ctx is valid only on the UI goroutine (Update/View/Run). Cmds must not capture it.
//
// Only mantle implements Ctx (the host, and pkg/ext/exttest for tests); it may gain
// methods (additive). Never implement it yourself: embed or use exttest.Ctx in tests.
type Ctx interface {
	Engine(id string) Engine // "" = main session; nil when that engine is not running
	Session() SessionInfo
	Transcript() Transcript
	Settings() Settings
	Theme() *theme.Theme
	Clock() Clock
	Size() (w, h int)
	Layout() LayoutMode
	Invalidate(componentID string)
	Print(blocks ...string) tea.Cmd // commit to scrollback (inline), chunked by host
	// Reprint clears the screen and scrollback (ESC[2J ESC[3J ESC[H), then delivers
	// ScreenClearedMsg; the commit policy (plan 03) resets its watermark and prints
	// everything finished again.
	Reprint() tea.Cmd
	Notify(Notice) tea.Cmd
	OpenDialog(id string, args any) tea.Cmd
	CloseDialog(id string) tea.Cmd
	Run(ActionID) tea.Cmd
	Store(featureID string) KV // ~/.mantle/state/<featureID>.json

	// Focus moves keyboard focus to a component (by ID). Dialogs always sit above it.
	Focus(componentID string) tea.Cmd
	// Focused returns the ID of the focused component ("" if none).
	Focused() string
	// SetContextActive turns an ambient keybinding context on or off ("Task" while a
	// turn runs, "Footer" while the footer is selected). Ambient contexts are consulted
	// after the focused component's contexts and before "Global".
	SetContextActive(context string, on bool)
	// KeysFor returns the chords currently bound to an action in a context, in display
	// form ("ctrl+o", "ctrl+x ctrl+k"), user bindings applied. Context "" searches all.
	KeysFor(context string, a ActionID) []string
	// Command looks up a resolved slash command by name or alias.
	Command(name string) (Command, bool)
	// Commands returns every resolved, visible slash command, sorted by name.
	Commands() []Command
	// Submit runs a draft through the prompt pipeline (slash routing, ! mode, …).
	Submit(Draft) tea.Cmd
	// Log is mantle's debug log (~/.mantle/logs). Never write to stdout or stderr.
	Log() *slog.Logger

	// Since contracts-v1.1.

	// Renderer returns the resolved renderer for a content key: exact key, then the
	// longest wildcard, then "default", with mods' Replace/Wrap applied. Never nil.
	Renderer(key ContentKey) Renderer
	// Accessibility returns the resolved accessibility preferences.
	Accessibility() Accessibility
}

// Accessibility preferences, resolved from flags, environment and settings
// (--ax-screen-reader, CLAUDE_AX_SCREEN_READER, axScreenReader, prefersReducedMotion).
type Accessibility struct {
	ScreenReader  bool // flat, linear output; no spinners or redraw tricks
	ReducedMotion bool // no shimmer or animation
}

// ScreenClearedMsg is delivered after Ctx.Reprint has cleared the screen and
// scrollback. The clear is written to the terminal before this message is delivered.
type ScreenClearedMsg struct{}

// FirstFrameMsg is delivered once, after the first frame has been drawn.
type FirstFrameMsg struct{}

// TranscriptAttachMsg registers the transcript store with the host, so
// Ctx.Transcript() returns it. features/transcript sends it from OnStart.
type TranscriptAttachMsg struct{ Transcript Transcript }

// EditorStateMsg reports the prompt editor's state (sent by input.editor on change);
// chrome uses it for the prompt frame colour and the vim mode indicator.
type EditorStateMsg struct {
	Mode  string // "prompt" | "bash"
	Vim   string // "" (vim off) | "INSERT" | "NORMAL" | "VISUAL" | "VISUAL LINE" | "REPLACE"
	Empty bool
}

// EnvSafe is the environment variable `mantle --safe` sets: the host skips every
// feature with Order >= ModOrder (user mods).
const EnvSafe = "MANTLE_SAFE"

// ExitUsage is the exit code for command-line usage errors (EX_USAGE); the launcher
// shows mantle-ui's log instead of counting a crash.
const ExitUsage = 64

// LayoutMode is the host's rendering mode.
type LayoutMode int

const (
	Inline     LayoutMode = iota // classic: finished items go to native scrollback
	Fullscreen                   // alt screen with a virtual transcript (M3)
	AltView                      // an alt-screen sub-view (ctrl+o viewer, /diff, big pickers)
)

func (m LayoutMode) String() string {
	switch m {
	case Inline:
		return "inline"
	case Fullscreen:
		return "fullscreen"
	case AltView:
		return "altview"
	}
	return "unknown"
}

// SessionInfo describes the session an engine is running.
type SessionInfo struct {
	EngineID       string
	SessionID      string
	Cwd            string
	Title          string
	Model          string
	PermissionMode string // "default" | "acceptEdits" | "plan" | "auto" | "bypassPermissions" | "dontAsk"
	ClaudeVersion  string
	OutputStyle    string
}

// SessionChangedMsg updates the host's SessionInfo for an engine. Engine bridges and
// session features send it; the host stores it and delivers it to subscribers.
type SessionChangedMsg struct {
	EngineID string
	Info     SessionInfo
}

// NoticeLevel orders notices by severity.
type NoticeLevel int

const (
	NoticeInfo NoticeLevel = iota
	NoticeSuccess
	NoticeWarning
	NoticeError
)

// Notice is a transient message shown above the input.
type Notice struct {
	Key     string // dedupe key: a newer notice with the same key replaces the older one
	Text    string
	Level   NoticeLevel
	Timeout time.Duration // 0 = host default; < 0 = until dismissed or replaced
	Source  string        // feature or engine ID that raised it
}

// KV is a small persistent store for one feature (~/.mantle/state/<featureID>.json).
// Values are JSON-encoded. Only mantle implements KV.
type KV interface {
	// Get decodes the value at key into out; ok is false when the key is absent.
	Get(key string, out any) (ok bool, err error)
	Set(key string, v any) error
	Delete(key string) error
	Keys() []string
}

// Clock is the host's time source. Tests freeze or step it; use it instead of
// time.Now so stories and goldens are deterministic. Only mantle implements Clock.
type Clock interface {
	Now() time.Time
	// Tick returns a Cmd that fires fn once after d.
	Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd
}

// SettingSpec declares a mantle setting (~/.mantle/settings.json). Specs generate the
// mantle section of /config.
type SettingSpec struct {
	Key, Type, Description string // Type: "bool" | "string" | "int" | "enum" | "json"
	Default                any
	Options                []string // for Type "enum"
}

// Settings reads Claude Code's merged settings and mantle's own. Only mantle
// implements it.
type Settings interface {
	Claude(key string) (any, bool) // merged Claude Code settings, read-only
	Mantle(key string) any         // mantle setting, or its SettingSpec default
	SetMantle(key string, v any) tea.Cmd
}

// SettingsMsg is sent after settings files change on disk or through SetMantle.
// Changed lists top-level keys ("theme", "mantle:selfmod.confirm"); mantle keys carry
// the "mantle:" prefix.
type SettingsMsg struct{ Changed []string }

// ClaudeString returns a string Claude Code setting, or def.
func ClaudeString(s Settings, key, def string) string {
	if v, ok := s.Claude(key); ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return def
}

// ClaudeBool returns a boolean Claude Code setting, or def.
func ClaudeBool(s Settings, key string, def bool) bool {
	if v, ok := s.Claude(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// ThemeChangedMsg is sent after the active theme changes (setting, auto detection,
// custom theme file edit).
type ThemeChangedMsg struct{ Name string }
