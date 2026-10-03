package theme

// Token is a semantic colour name. Components ask the theme for a token, never for a
// literal colour, so built-in themes, custom themes and mods can restyle everything.
//
// Token strings follow the keys Claude Code accepts in custom theme files
// (~/.claude/themes/*.json), so a user's overrides apply to mantle unchanged. Unknown
// keys in a custom theme are kept too (see Theme.Colors), so new tokens never need a
// code change to be overridable.
type Token string

// Text and chrome.
const (
	Text        Token = "text"        // default foreground
	InverseText Token = "inverseText" // text drawn on an accent background
	Inactive    Token = "inactive"    // dim: hints, timestamps, secondary labels
	Subtle      Token = "subtle"      // dimmer than Inactive: rules, borders, placeholders
	Background  Token = "background"  // explicit background where one is needed

	// Accent is the product accent (spinner glyph, brand marks, highlights).
	Accent        Token = "claude"
	AccentShimmer Token = "claudeShimmer" // shimmer pair for the spinner verb

	// SystemSpinner/SystemSpinnerShimmer colour the spinner while the engine is busy
	// with non-model work (compacting, retrying).
	SystemSpinner        Token = "claudeBlue_FOR_SYSTEM_SPINNER"
	SystemSpinnerShimmer Token = "claudeBlueShimmer_FOR_SYSTEM_SPINNER"

	Suggestion Token = "suggestion" // selected item in menus and pickers, ghost text
	Remember   Token = "remember"   // memory entries
	IDE        Token = "ide"        // IDE integration marks
	Skill      Token = "skill"      // skill invocations
)

// Status.
const (
	Success        Token = "success"
	Error          Token = "error"
	Warning        Token = "warning"
	WarningShimmer Token = "warningShimmer"
	Merged         Token = "merged" // PR badge: merged
)

// Modes and prompts.
const (
	Permission          Token = "permission" // permission dialog border and title
	PermissionShimmer   Token = "permissionShimmer"
	PlanMode            Token = "planMode"
	AutoAccept          Token = "autoAccept"
	AutoAcceptShimmer   Token = "autoAcceptShimmer"
	FastMode            Token = "fastMode"
	FastModeShimmer     Token = "fastModeShimmer"
	BashBorder          Token = "bashBorder"   // prompt border in ! (bash) mode
	PromptBorder        Token = "promptBorder" // prompt border in normal mode
	PromptBorderShimmer Token = "promptBorderShimmer"
)

// Diffs.
const (
	DiffAdded         Token = "diffAdded"
	DiffRemoved       Token = "diffRemoved"
	DiffAddedDimmed   Token = "diffAddedDimmed"
	DiffRemovedDimmed Token = "diffRemovedDimmed"
	DiffAddedWord     Token = "diffAddedWord"
	DiffRemovedWord   Token = "diffRemovedWord"
)

// Backgrounds.
const (
	UserMessageBg      Token = "userMessageBackground"
	UserMessageBgHover Token = "userMessageBackgroundHover"
	BashMessageBg      Token = "bashMessageBackgroundColor"
	MemoryBg           Token = "memoryBackgroundColor"
	SelectionBg        Token = "selectionBg"
)

// Meters.
const (
	RateLimitFill  Token = "rate_limit_fill"
	RateLimitEmpty Token = "rate_limit_empty"
)

// Subagent colours (agent definitions pick one by name).
const (
	AgentRed    Token = "red_FOR_SUBAGENTS_ONLY"
	AgentBlue   Token = "blue_FOR_SUBAGENTS_ONLY"
	AgentGreen  Token = "green_FOR_SUBAGENTS_ONLY"
	AgentYellow Token = "yellow_FOR_SUBAGENTS_ONLY"
	AgentPurple Token = "purple_FOR_SUBAGENTS_ONLY"
	AgentOrange Token = "orange_FOR_SUBAGENTS_ONLY"
	AgentPink   Token = "pink_FOR_SUBAGENTS_ONLY"
	AgentCyan   Token = "cyan_FOR_SUBAGENTS_ONLY"
)

// Syntax highlighting classes. Renderers map chroma token types onto these; they are
// mantle's own names (prefix "syntax.") and can be overridden like any other token.
const (
	SyntaxKeyword     Token = "syntax.keyword"
	SyntaxString      Token = "syntax.string"
	SyntaxNumber      Token = "syntax.number"
	SyntaxComment     Token = "syntax.comment"
	SyntaxFunction    Token = "syntax.function"
	SyntaxType        Token = "syntax.type"
	SyntaxVariable    Token = "syntax.variable"
	SyntaxConstant    Token = "syntax.constant"
	SyntaxOperator    Token = "syntax.operator"
	SyntaxPunctuation Token = "syntax.punctuation"
	SyntaxTag         Token = "syntax.tag"
	SyntaxAttribute   Token = "syntax.attribute"
	SyntaxBuiltin     Token = "syntax.builtin"
	SyntaxPlain       Token = "syntax.plain"
)

// Tokens lists every token mantle defines, in a stable order. Built-in themes set all
// of them; Theme.Validate reports any that are missing.
var Tokens = []Token{
	Text, InverseText, Inactive, Subtle, Background,
	Accent, AccentShimmer, SystemSpinner, SystemSpinnerShimmer,
	Suggestion, Remember, IDE, Skill,
	Success, Error, Warning, WarningShimmer, Merged,
	Permission, PermissionShimmer, PlanMode, AutoAccept, AutoAcceptShimmer,
	FastMode, FastModeShimmer, BashBorder, PromptBorder, PromptBorderShimmer,
	DiffAdded, DiffRemoved, DiffAddedDimmed, DiffRemovedDimmed, DiffAddedWord, DiffRemovedWord,
	UserMessageBg, UserMessageBgHover, BashMessageBg, MemoryBg, SelectionBg,
	RateLimitFill, RateLimitEmpty,
	AgentRed, AgentBlue, AgentGreen, AgentYellow, AgentPurple, AgentOrange, AgentPink, AgentCyan,
	SyntaxKeyword, SyntaxString, SyntaxNumber, SyntaxComment, SyntaxFunction, SyntaxType,
	SyntaxVariable, SyntaxConstant, SyntaxOperator, SyntaxPunctuation, SyntaxTag,
	SyntaxAttribute, SyntaxBuiltin, SyntaxPlain,
}

// Shimmer returns the shimmer partner of a token (the lighter colour a spinner or
// border sweeps across it), or the token itself when it has none.
func Shimmer(t Token) Token {
	switch t {
	case Accent:
		return AccentShimmer
	case SystemSpinner:
		return SystemSpinnerShimmer
	case Permission:
		return PermissionShimmer
	case AutoAccept:
		return AutoAcceptShimmer
	case FastMode:
		return FastModeShimmer
	case PromptBorder:
		return PromptBorderShimmer
	case Warning:
		return WarningShimmer
	}
	return t
}
