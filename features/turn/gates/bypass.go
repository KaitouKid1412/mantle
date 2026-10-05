package gates

import (
	"strings"

	"github.com/KaitouKid1412/mantle/features/turn/mode"
)

// LaunchFlags are the command-line inputs the gates care about (plan 11's parser fills
// them).
type LaunchFlags struct {
	PermissionMode   string // --permission-mode
	DangerouslySkip  bool   // --dangerously-skip-permissions
	AllowDangerously bool   // --allow-dangerously-skip-permissions
	Settings         string // --settings (inline JSON or path)
	StrictMcpConfig  bool   // --strict-mcp-config: .mcp.json is ignored
}

// BypassDisabled reports whether settings forbid bypassPermissions mode
// (permissions.disableBypassPermissionsMode: "disable").
func BypassDisabled(layers Layers) bool {
	return strings.EqualFold(layers.String("permissions.disableBypassPermissionsMode"), "disable")
}

// AutoDisabled reports whether settings forbid auto mode (permissions.disableAutoMode).
func AutoDisabled(layers Layers) bool {
	return strings.EqualFold(layers.String("permissions.disableAutoMode"), "disable")
}

// StartsInBypass reports whether the session would start in bypassPermissions mode:
// --dangerously-skip-permissions, --permission-mode bypassPermissions, or (no mode flag)
// permissions.defaultMode bypassPermissions.
func StartsInBypass(f LaunchFlags, layers Layers) bool {
	if f.DangerouslySkip {
		return true
	}
	if m, ok := mode.Parse(f.PermissionMode); ok {
		return m == mode.BypassPermissions
	}
	m, ok := mode.Parse(layers.String("permissions.defaultMode"))
	return ok && m == mode.BypassPermissions
}

// BypassAvailable reports whether the mode cycle may enter bypassPermissions: the engine
// must have been started with --allow-dangerously-skip-permissions or
// --dangerously-skip-permissions (mantle adds the former when starting in bypass), and
// settings must not disable it.
func BypassAvailable(f LaunchFlags, layers Layers) bool {
	if BypassDisabled(layers) {
		return false
	}
	return f.DangerouslySkip || f.AllowDangerously || StartsInBypass(f, layers)
}

// BypassWarningNeeded reports whether the bypass-permissions warning must be shown before
// spawning. It is suppressed by skipDangerousModePermissionPrompt in a scope the
// repository can't ship, or by an earlier acceptance recorded by mantle. store may be
// nil.
func BypassWarningNeeded(f LaunchFlags, layers Layers, store *GateStore) bool {
	if !StartsInBypass(f, layers) || BypassDisabled(layers) {
		return false
	}
	if layers.Bool("skipDangerousModePermissionPrompt", userScopes...) {
		return false
	}
	return store == nil || store.BypassAcceptedAt == ""
}
