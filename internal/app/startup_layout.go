package app

import (
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// LayoutInputs is what decides the startup layout.
type LayoutInputs struct {
	// TUI is the explicit `tui` setting from any scope (user, project, local,
	// --settings); "" when unset. "fullscreen" or "default" (inline).
	TUI string
	// EngineDefaultTUI is the installed claude's default renderer for an unset `tui`
	// (plan 11's engine-defaults table): "fullscreen" or "default".
	EngineDefaultTUI string
	// NoAltScreen is CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN; ScreenReader is screen-
	// reader mode. Either forces inline.
	NoAltScreen, ScreenReader bool
	// NoFlicker is CLAUDE_CODE_NO_FLICKER, an explicit request for fullscreen.
	NoFlicker bool
}

// StartupLayout picks the layout mantle starts in, matching claude:
//  1. no alternate screen (CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN, screen reader): inline;
//  2. an explicit `tui` setting wins;
//  3. CLAUDE_CODE_NO_FLICKER asks for fullscreen;
//  4. otherwise the installed claude's default (EngineDefaultTUI).
//
// /tui switches at runtime (ext.LayoutRequestMsg) under the same alt-screen rule.
func StartupLayout(in LayoutInputs) ext.LayoutMode {
	if in.NoAltScreen || in.ScreenReader {
		return ext.Inline
	}
	switch strings.ToLower(strings.TrimSpace(in.TUI)) {
	case "fullscreen":
		return ext.Fullscreen
	case "default", "inline":
		return ext.Inline
	}
	if in.NoFlicker {
		return ext.Fullscreen
	}
	if strings.EqualFold(in.EngineDefaultTUI, "fullscreen") {
		return ext.Fullscreen
	}
	return ext.Inline
}
