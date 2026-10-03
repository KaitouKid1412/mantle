package theme

import (
	"image/color"
	"maps"
	"slices"
)

// Built-in theme names, as used by the "theme" setting. "auto" is resolved by the host
// to Dark or Light from the terminal background.
const (
	NameAuto            = "auto"
	NameDark            = "dark"
	NameLight           = "light"
	NameDarkDaltonized  = "dark-daltonized"
	NameLightDaltonized = "light-daltonized"
	NameDarkANSI        = "dark-ansi"
	NameLightANSI       = "light-ansi"
	// CustomPrefix prefixes themes loaded from ~/.claude/themes/<name>.json.
	CustomPrefix = "custom:"
)

// palette is a token → colour-string table. These palettes are mantle's own.
type palette map[Token]string

func (p palette) build(name string, dark, ansiOnly bool) Theme {
	t := Theme{Name: name, Dark: dark, ANSI: ansiOnly, Colors: make(map[Token]color.Color, len(p))}
	for tok, s := range p {
		t.Colors[tok] = MustColor(s)
	}
	return t
}

func (p palette) with(over palette) palette {
	out := maps.Clone(p)
	maps.Copy(out, over)
	return out
}

var darkPalette = palette{
	Text: "#e6e3de", InverseText: "#16161a", Inactive: "#8b8b93", Subtle: "#55555c",
	Background: "#16161a",
	Accent:     "#e0956e", AccentShimmer: "#f3c3a6",
	SystemSpinner: "#7aa2e3", SystemSpinnerShimmer: "#b7cdf1",
	Suggestion: "#93b4fb", Remember: "#9cc8e6", IDE: "#6fa6d6", Skill: "#b691ee",
	Success: "#6fc28b", Error: "#ef7a80", Warning: "#e7b75f", WarningShimmer: "#f4d79d",
	Merged:     "#a98cf7",
	Permission: "#9ea6f3", PermissionShimmer: "#cbd0f8", PlanMode: "#4fb0a6",
	AutoAccept: "#b691ee", AutoAcceptShimmer: "#d9c7f6",
	FastMode: "#f0a04f", FastModeShimmer: "#f7c992",
	BashBorder: "#ee72a8", PromptBorder: "#6c6c74", PromptBorderShimmer: "#9c9ca4",
	DiffAdded: "#20402b", DiffRemoved: "#4b2429", DiffAddedDimmed: "#1a2c20", DiffRemovedDimmed: "#33201f",
	DiffAddedWord: "#2e6a40", DiffRemovedWord: "#7b3039",
	UserMessageBg: "#26262c", UserMessageBgHover: "#303037", BashMessageBg: "#2c2330",
	MemoryBg: "#212f39", SelectionBg: "#2e4366",
	RateLimitFill: "#93b4fb", RateLimitEmpty: "#3a3a41",
	AgentRed: "#e2707a", AgentBlue: "#62adec", AgentGreen: "#93c47d", AgentYellow: "#e3c07e",
	AgentPurple: "#c27bdb", AgentOrange: "#d39a68", AgentPink: "#f292b4", AgentCyan: "#5ab5c0",
	SyntaxKeyword: "#c27bdb", SyntaxString: "#93c47d", SyntaxNumber: "#d39a68",
	SyntaxComment: "#7d828c", SyntaxFunction: "#62adec", SyntaxType: "#e3c07e",
	SyntaxVariable: "#e2707a", SyntaxConstant: "#d39a68", SyntaxOperator: "#5ab5c0",
	SyntaxPunctuation: "#aab0bc", SyntaxTag: "#e2707a", SyntaxAttribute: "#d39a68",
	SyntaxBuiltin: "#5ab5c0", SyntaxPlain: "#e6e3de",
}

var lightPalette = palette{
	Text: "#1d1d21", InverseText: "#fbfaf8", Inactive: "#6e6e76", Subtle: "#a9a9b0",
	Background: "#fbfaf8",
	Accent:     "#c4643a", AccentShimmer: "#e4a183",
	SystemSpinner: "#3f6fc0", SystemSpinnerShimmer: "#87a7dc",
	Suggestion: "#3a62c9", Remember: "#2f78a8", IDE: "#2f6fa6", Skill: "#7a4fc9",
	Success: "#2e8a4e", Error: "#c8323c", Warning: "#a8730f", WarningShimmer: "#d4a650",
	Merged:     "#7650d6",
	Permission: "#4d55c7", PermissionShimmer: "#8f95e0", PlanMode: "#16857a",
	AutoAccept: "#7a4fc9", AutoAcceptShimmer: "#b39be2",
	FastMode: "#c06a12", FastModeShimmer: "#e0a663",
	BashBorder: "#c63d7c", PromptBorder: "#9a9aa2", PromptBorderShimmer: "#c4c4ca",
	DiffAdded: "#d9f2df", DiffRemoved: "#f8dcde", DiffAddedDimmed: "#ebf7ee", DiffRemovedDimmed: "#fbeced",
	DiffAddedWord: "#a8e0b6", DiffRemovedWord: "#f0b0b5",
	UserMessageBg: "#efeeeb", UserMessageBgHover: "#e5e4e0", BashMessageBg: "#f6e9f0",
	MemoryBg: "#e4eff6", SelectionBg: "#c9daf5",
	RateLimitFill: "#3a62c9", RateLimitEmpty: "#d6d6db",
	AgentRed: "#c4404a", AgentBlue: "#2f70c0", AgentGreen: "#3f8a2f", AgentYellow: "#9c7a12",
	AgentPurple: "#8a3fb0", AgentOrange: "#b9601e", AgentPink: "#c4477d", AgentCyan: "#1f8790",
	SyntaxKeyword: "#8a3fb0", SyntaxString: "#3f8a2f", SyntaxNumber: "#b9601e",
	SyntaxComment: "#8a8f98", SyntaxFunction: "#2f70c0", SyntaxType: "#9c7a12",
	SyntaxVariable: "#c4404a", SyntaxConstant: "#b9601e", SyntaxOperator: "#1f8790",
	SyntaxPunctuation: "#4a4f58", SyntaxTag: "#c4404a", SyntaxAttribute: "#b9601e",
	SyntaxBuiltin: "#1f8790", SyntaxPlain: "#1d1d21",
}

// Daltonized variants avoid red/green contrasts: additions are blue, removals orange.
var darkDaltonizedOver = palette{
	Success: "#5aa7e8", Error: "#f0a04f",
	DiffAdded: "#1f3550", DiffRemoved: "#4d3419", DiffAddedDimmed: "#1a2736", DiffRemovedDimmed: "#33261a",
	DiffAddedWord: "#2d5b8f", DiffRemovedWord: "#8a5a22",
	SyntaxString: "#5aa7e8", SyntaxVariable: "#f0a04f", SyntaxTag: "#f0a04f",
	AgentRed: "#f0a04f", AgentGreen: "#5aa7e8",
}

var lightDaltonizedOver = palette{
	Success: "#2565b0", Error: "#c06a12",
	DiffAdded: "#d6e6f8", DiffRemoved: "#f8e3cc", DiffAddedDimmed: "#e8f0fa", DiffRemovedDimmed: "#fbefe2",
	DiffAddedWord: "#a5c6ee", DiffRemovedWord: "#f0c38f",
	SyntaxString: "#2565b0", SyntaxVariable: "#c06a12", SyntaxTag: "#c06a12",
	AgentRed: "#c06a12", AgentGreen: "#2565b0",
}

// ANSI variants use only the 16 terminal colours (indices), so they follow the user's
// terminal scheme. Backgrounds that need a tint use the "bright black"/"white" slots.
var darkANSIPalette = palette{
	Text: "15", InverseText: "0", Inactive: "8", Subtle: "8", Background: "0",
	Accent: "3", AccentShimmer: "11", SystemSpinner: "4", SystemSpinnerShimmer: "12",
	Suggestion: "12", Remember: "6", IDE: "4", Skill: "5",
	Success: "2", Error: "1", Warning: "3", WarningShimmer: "11", Merged: "5",
	Permission: "12", PermissionShimmer: "14", PlanMode: "6",
	AutoAccept: "5", AutoAcceptShimmer: "13", FastMode: "3", FastModeShimmer: "11",
	BashBorder: "13", PromptBorder: "8", PromptBorderShimmer: "7",
	DiffAdded: "2", DiffRemoved: "1", DiffAddedDimmed: "2", DiffRemovedDimmed: "1",
	DiffAddedWord: "10", DiffRemovedWord: "9",
	UserMessageBg: "8", UserMessageBgHover: "8", BashMessageBg: "8", MemoryBg: "8", SelectionBg: "4",
	RateLimitFill: "12", RateLimitEmpty: "8",
	AgentRed: "1", AgentBlue: "4", AgentGreen: "2", AgentYellow: "3",
	AgentPurple: "5", AgentOrange: "11", AgentPink: "13", AgentCyan: "6",
	SyntaxKeyword: "5", SyntaxString: "2", SyntaxNumber: "3", SyntaxComment: "8",
	SyntaxFunction: "4", SyntaxType: "3", SyntaxVariable: "1", SyntaxConstant: "3",
	SyntaxOperator: "6", SyntaxPunctuation: "7", SyntaxTag: "1", SyntaxAttribute: "3",
	SyntaxBuiltin: "6", SyntaxPlain: "15",
}

var lightANSIOver = palette{
	Text: "0", InverseText: "15", Background: "15", SyntaxPlain: "0", SyntaxPunctuation: "8",
	UserMessageBg: "7", UserMessageBgHover: "7", BashMessageBg: "7", MemoryBg: "7",
	PromptBorderShimmer: "0", RateLimitEmpty: "7",
}

// Builtins returns the six built-in themes in picker order. Each call returns fresh
// copies, so callers may modify them.
func Builtins() []Theme {
	return []Theme{
		darkPalette.build(NameDark, true, false),
		lightPalette.build(NameLight, false, false),
		darkPalette.with(darkDaltonizedOver).build(NameDarkDaltonized, true, false),
		lightPalette.with(lightDaltonizedOver).build(NameLightDaltonized, false, false),
		darkANSIPalette.build(NameDarkANSI, true, true),
		darkANSIPalette.with(lightANSIOver).build(NameLightANSI, false, true),
	}
}

// Builtin returns the built-in theme with the given name.
func Builtin(name string) (Theme, bool) {
	all := Builtins()
	i := slices.IndexFunc(all, func(t Theme) bool { return t.Name == name })
	if i < 0 {
		return Theme{}, false
	}
	return all[i], true
}

// Default returns the dark theme, used before settings are read and in tests.
func Default() Theme {
	t, _ := Builtin(NameDark)
	return t
}
