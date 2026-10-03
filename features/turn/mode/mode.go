// Package mode holds the permission-mode model: the mode values the engine accepts, the
// shift+tab cycle and the indicator text. It is pure (no I/O, no ext) so the cycle can be
// table-tested against Claude Code's order.
//
// Parity: PD-19, PD-20.
package mode

import "strings"

// Mode is a permission mode as the engine names it on the wire (`set_permission_mode`,
// `init.permissionMode`, `PermissionUpdate.setMode`).
type Mode string

const (
	Default           Mode = "default" // shown as "manual"
	AcceptEdits       Mode = "acceptEdits"
	Plan              Mode = "plan"
	Auto              Mode = "auto"
	BypassPermissions Mode = "bypassPermissions"
	DontAsk           Mode = "dontAsk"
)

// All lists every mode the engine accepts, in cycle order followed by the modes that are
// only reachable explicitly.
var All = []Mode{Default, AcceptEdits, Plan, BypassPermissions, Auto, DontAsk}

// Parse maps a user- or engine-supplied string to a Mode. "manual" is an alias for
// default. Matching ignores case, so "acceptedits" from a flag still resolves.
func Parse(s string) (Mode, bool) {
	t := strings.TrimSpace(s)
	if strings.EqualFold(t, "manual") {
		return Default, true
	}
	for _, m := range All {
		if strings.EqualFold(t, string(m)) {
			return m, true
		}
	}
	return "", false
}

// Valid reports whether m is one of the engine's modes.
func (m Mode) Valid() bool {
	_, ok := Parse(string(m))
	return ok && Normalize(m) == m
}

// Normalize resolves aliases and case; unknown values become Default.
func Normalize(m Mode) Mode {
	if p, ok := Parse(string(m)); ok {
		return p
	}
	return Default
}

// Availability says which optional modes the cycle may enter.
type Availability struct {
	// Bypass is true only when the engine was started with
	// --allow-dangerously-skip-permissions or --dangerously-skip-permissions and settings
	// don't set permissions.disableBypassPermissionsMode.
	Bypass bool
	// Auto is true when the current model supports auto mode and settings don't set
	// permissions.disableAutoMode.
	Auto bool
}

// Next returns the mode shift+tab switches to:
//
//	default → acceptEdits → plan → bypassPermissions | auto | default
//	bypassPermissions → auto | default
//	auto, dontAsk → default
func Next(m Mode, a Availability) Mode {
	switch Normalize(m) {
	case Default:
		return AcceptEdits
	case AcceptEdits:
		return Plan
	case Plan:
		switch {
		case a.Bypass:
			return BypassPermissions
		case a.Auto:
			return Auto
		}
		return Default
	case BypassPermissions:
		if a.Auto {
			return Auto
		}
		return Default
	}
	return Default
}

// Indicator is what the footer and dialogs show for a mode.
type Indicator struct {
	Symbol string // "⏸" or "⏵⏵"
	Text   string // "plan mode on"
	// Token is the pkg/theme token the indicator is coloured with.
	Token string
}

// String joins symbol and text: "⏸ plan mode on".
func (i Indicator) String() string {
	if i.Symbol == "" {
		return i.Text
	}
	return i.Symbol + " " + i.Text
}

const (
	pause = "⏸"
	play  = "⏵⏵"
)

// IndicatorFor returns the indicator for m. Unknown modes get the default indicator.
func IndicatorFor(m Mode) Indicator {
	switch Normalize(m) {
	case AcceptEdits:
		return Indicator{play, "accept edits on", "autoAccept"}
	case Plan:
		return Indicator{pause, "plan mode on", "planMode"}
	case Auto:
		return Indicator{play, "auto mode on", "autoAccept"}
	case BypassPermissions:
		return Indicator{play, "bypass permissions on", "error"}
	case DontAsk:
		return Indicator{play, "don't ask mode on", "warning"}
	}
	return Indicator{pause, "manual mode on", "permission"}
}

// Label is the short human name of a mode, used in option lists ("switch to Auto").
func Label(m Mode) string {
	switch Normalize(m) {
	case AcceptEdits:
		return "Accept edits"
	case Plan:
		return "Plan"
	case Auto:
		return "Auto"
	case BypassPermissions:
		return "Bypass permissions"
	case DontAsk:
		return "Don't ask"
	}
	return "Manual"
}

// CycleHint is appended to the indicator in the footer.
const CycleHint = "(shift+tab to cycle)"

// StartupInput collects what decides the mode a fresh interactive session starts in.
type StartupInput struct {
	Flag            string // --permission-mode, if given
	DangerouslySkip bool   // --dangerously-skip-permissions
	SettingsDefault string // permissions.defaultMode, merged
	Availability    Availability
	// PreferAuto mirrors interactive Claude Code, which starts in auto when it is
	// available and nothing else asks for a mode.
	PreferAuto bool
}

// Startup returns the mode mantle should request with set_permission_mode after
// initialize. Headless starts in default, interactive Claude Code does not, so mantle
// mirrors the interactive rules: the flag wins, then --dangerously-skip-permissions, then
// permissions.defaultMode, then auto if available and preferred, else default. Modes that
// aren't available fall back to default.
func Startup(in StartupInput) Mode {
	pick := func(m Mode) Mode {
		switch m {
		case BypassPermissions:
			if !in.Availability.Bypass {
				return Default
			}
		case Auto:
			if !in.Availability.Auto {
				return Default
			}
		}
		return m
	}
	if m, ok := Parse(in.Flag); ok {
		return pick(m)
	}
	if in.DangerouslySkip {
		return pick(BypassPermissions)
	}
	if m, ok := Parse(in.SettingsDefault); ok {
		return pick(m)
	}
	if in.PreferAuto && in.Availability.Auto {
		return Auto
	}
	return Default
}
