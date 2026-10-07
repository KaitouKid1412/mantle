package chrome

import "github.com/KaitouKid1412/mantle/pkg/ext"

// Permission modes as the engine reports them (init.permissionMode, status).
const (
	ModeDefault     = "default"
	ModeAcceptEdits = "acceptEdits"
	ModePlan        = "plan"
	ModeAuto        = "auto"
	ModeDontAsk     = "dontAsk"
	ModeBypass      = "bypassPermissions"
)

// ModeIndicator is how the footer shows a permission mode.
type ModeIndicator struct {
	Glyph string
	Label string
	// Token names the theme colour for the indicator ("" = muted text).
	Token string
	// Quiet modes are shown only as a dim hint, not as a coloured badge.
	Quiet bool
}

// IndicatorFor returns the footer indicator for a permission mode. Unknown modes show
// their raw name so a new engine mode is never invisible.
func IndicatorFor(mode string) ModeIndicator {
	switch mode {
	case ModeDefault, "":
		return ModeIndicator{Glyph: "⏸", Label: "manual approval", Quiet: true}
	case ModeAcceptEdits:
		return ModeIndicator{Glyph: "⏵⏵", Label: "edits auto-approved", Token: "autoAccept"}
	case ModePlan:
		return ModeIndicator{Glyph: "⏸", Label: "planning only", Token: "planMode"}
	case ModeAuto:
		return ModeIndicator{Glyph: "⏵⏵", Label: "auto mode", Token: "warning"}
	case ModeDontAsk:
		return ModeIndicator{Glyph: "⏵⏵", Label: "unlisted tools denied", Token: "warning"}
	case ModeBypass:
		return ModeIndicator{Glyph: "⏵⏵", Label: "all permission checks off", Token: "error"}
	}
	return ModeIndicator{Glyph: "•", Label: mode + " mode", Token: "warning"}
}

// Text is the indicator without styling: "⏵⏵ edits auto-approved".
func (m ModeIndicator) Text() string { return m.Glyph + " " + m.Label }

// CycleHint follows the indicator and names the mode-cycle key.
const CycleHint = "shift+tab to change"

// ResearchIndicator is how the footer shows research mode (plan 13, MT-R1). It takes
// the place of the permission-mode indicator: research runs only in the default mode.
var ResearchIndicator = ModeIndicator{Glyph: "⌕", Label: "research", Token: "suggestion"}

// indicator is the footer indicator for the session: research mode's while it is on,
// else the permission mode's.
func (s *sessionState) indicator() ModeIndicator {
	if s.UIMode == ext.UIModeResearch {
		return ResearchIndicator
	}
	return IndicatorFor(s.Mode)
}
