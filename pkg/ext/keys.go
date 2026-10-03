package ext

// Keybinding contexts, verbatim from Claude Code (keybindings.json "context" values).
const (
	ContextGlobal            = "Global"
	ContextChat              = "Chat"
	ContextAutocomplete      = "Autocomplete"
	ContextConfirmation      = "Confirmation"
	ContextSettings          = "Settings"
	ContextTabs              = "Tabs"
	ContextTranscript        = "Transcript"
	ContextHistorySearch     = "HistorySearch"
	ContextTask              = "Task"
	ContextThemePicker       = "ThemePicker"
	ContextScroll            = "Scroll"
	ContextHelp              = "Help"
	ContextAttachments       = "Attachments"
	ContextFooter            = "Footer"
	ContextAbovePrompt       = "AbovePrompt"
	ContextAbovePromptInput  = "AbovePromptInput"
	ContextAbovePromptSelect = "AbovePromptSelect"
	ContextPane              = "Pane"
	ContextPaneField         = "PaneField"
	ContextDiffDialog        = "DiffDialog"
	ContextDiffPanel         = "DiffPanel"
	ContextModelPicker       = "ModelPicker"
	ContextEffortSlider      = "EffortSlider"
	ContextSelect            = "Select"
	ContextPlugin            = "Plugin"
	ContextAgents            = "Agents"
	ContextMessageSelector   = "MessageSelector"
)

// Contexts lists every Claude Code keybinding context.
var Contexts = []string{
	ContextGlobal, ContextChat, ContextAutocomplete, ContextConfirmation, ContextSettings,
	ContextTabs, ContextTranscript, ContextHistorySearch, ContextTask, ContextThemePicker,
	ContextScroll, ContextHelp, ContextAttachments, ContextFooter, ContextAbovePrompt,
	ContextAbovePromptInput, ContextAbovePromptSelect, ContextPane, ContextPaneField,
	ContextDiffDialog, ContextDiffPanel, ContextModelPicker, ContextEffortSlider,
	ContextSelect, ContextPlugin, ContextAgents, ContextMessageSelector,
}

// ContextMantle is the context for mantle-only bindings in ~/.mantle/keybindings.json
// that apply everywhere. mantle:* actions may also be bound in any Claude Code context.
const ContextMantle = "Mantle"

// MantleActionPrefix prefixes mantle-only action IDs.
const MantleActionPrefix = "mantle:"

// ChordTimeoutMillis is how long a chord prefix ("ctrl+x") waits for the next key.
const ChordTimeoutMillis = 3000

// ReservedKeys cannot be rebound: the terminal or the host owns them. Binding one in
// keybindings.json is reported as an error and ignored.
var ReservedKeys = []string{
	"ctrl+c", "ctrl+d", // interrupt / exit, handled by the host
	"ctrl+m", "ctrl+i", "ctrl+h", "ctrl+[", // indistinguishable from enter, tab, backspace, escape
	`ctrl+\`, // SIGQUIT
	"capslock",
	"cmd+c", "cmd+v", "cmd+x", "cmd+q", "cmd+w", "cmd+tab", "cmd+space",
}

// WarnKeys may be bound but produce a warning (ctrl+z suspends the process group).
var WarnKeys = []string{"ctrl+z"}

// DefaultBindings is the default keymap, matching Claude Code 2.1.288 on macOS and
// Linux. Features register behaviour (Actions), not keys; the host loads this table
// first, then features' AddBinding calls, then ~/.claude/keybindings.json and
// ~/.mantle/keybindings.json.
//
// Platform differences in Claude Code (alt+v image paste on Windows, meta+m mode cycle
// on old Windows terminals) are not listed; mantle targets macOS and Linux.
var DefaultBindings = []Binding{
	// Global
	{ContextGlobal, "ctrl+c", ActAppInterrupt},
	{ContextGlobal, "ctrl+d", ActAppExit},
	{ContextGlobal, "ctrl+t", ActAppToggleTodos},
	{ContextGlobal, "ctrl+o", ActAppToggleTranscript},
	{ContextGlobal, "ctrl+shift+b", ActAppToggleBrief},
	{ContextGlobal, "ctrl+r", ActHistorySearch},
	{ContextGlobal, "ctrl+up", ActAppDiffFileListUp},
	{ContextGlobal, "ctrl+down", ActAppDiffFileListDown},
	{ContextGlobal, "meta+up", ActAppDiffFileListUp},
	{ContextGlobal, "meta+down", ActAppDiffFileListDown},
	{ContextGlobal, "ctrl+]", ActAppOpenArtifact},

	// DiffPanel
	{ContextDiffPanel, "ctrl+x b", ActAppCycleDiffBase},

	// Chat
	{ContextChat, "escape", ActChatCancel},
	{ContextChat, "ctrl+l", ActChatClearInput},
	{ContextChat, "cmd+k", ActChatClearScreen},
	{ContextChat, "ctrl+x ctrl+k", ActChatKillAgents},
	{ContextChat, "shift+tab", ActChatCycleMode},
	{ContextChat, "meta+p", ActChatModelPicker},
	{ContextChat, "meta+o", ActChatFastMode},
	{ContextChat, "ctrl+y", ActChatDefaultToNewerModel},
	{ContextChat, "meta+t", ActChatThinkingToggle},
	{ContextChat, "meta+w", ActChatWorkflowKeywordToggle},
	{ContextChat, "enter", ActChatSubmit},
	{ContextChat, "ctrl+x enter", ActChatQueueSubmit},
	{ContextChat, "ctrl+x ctrl+s", ActChatSendNow},
	{ContextChat, "ctrl+enter", ActChatSendNow},
	{ContextChat, "ctrl+j", ActChatNewline},
	{ContextChat, "up", ActHistoryPrevious},
	{ContextChat, "down", ActHistoryNext},
	{ContextChat, "ctrl+_", ActChatUndo},
	{ContextChat, "ctrl+-", ActChatUndo},
	{ContextChat, "ctrl+shift+-", ActChatUndo},
	{ContextChat, "ctrl+shift+_", ActChatUndo},
	{ContextChat, "ctrl+x ctrl+e", ActChatExternalEditor},
	{ContextChat, "ctrl+x ctrl+a", ActAbovePromptToggle},
	{ContextChat, "ctrl+x tab", ActAbovePromptFocus},
	{ContextChat, "ctrl+g", ActChatExternalEditor},
	{ContextChat, "ctrl+s", ActChatStash},
	{ContextChat, "ctrl+v", ActChatImagePaste},
	{ContextChat, "space", ActVoicePushToTalk},

	// Autocomplete
	{ContextAutocomplete, "tab", ActAutocompleteAccept},
	{ContextAutocomplete, "escape", ActAutocompleteDismiss},
	{ContextAutocomplete, "up", ActAutocompletePrevious},
	{ContextAutocomplete, "down", ActAutocompleteNext},

	// Settings
	{ContextSettings, "escape", ActConfirmNo},
	{ContextSettings, "up", ActSelectPrevious},
	{ContextSettings, "down", ActSelectNext},
	{ContextSettings, "k", ActSelectPrevious},
	{ContextSettings, "j", ActSelectNext},
	{ContextSettings, "ctrl+p", ActSelectPrevious},
	{ContextSettings, "ctrl+n", ActSelectNext},
	{ContextSettings, "home", ActSelectFirst},
	{ContextSettings, "end", ActSelectLast},
	{ContextSettings, "pageup", ActSelectPageUp},
	{ContextSettings, "pagedown", ActSelectPageDown},
	{ContextSettings, "space", ActSelectAccept},
	{ContextSettings, "enter", ActSelectAccept},
	{ContextSettings, "/", ActSettingsSearch},
	{ContextSettings, "r", ActSettingsRetry},
	{ContextSettings, "d", ActSettingsPeriodDay},
	{ContextSettings, "w", ActSettingsPeriodWeek},
	{ContextSettings, "t", ActSettingsSortByTokens},
	{ContextSettings, "ctrl+u", ActScrollHalfPageUp},
	{ContextSettings, "ctrl+d", ActScrollHalfPageDown},

	// Confirmation
	{ContextConfirmation, "enter", ActConfirmYes},
	{ContextConfirmation, "escape", ActConfirmNo},
	{ContextConfirmation, "up", ActConfirmPrevious},
	{ContextConfirmation, "down", ActConfirmNext},
	{ContextConfirmation, "tab", ActConfirmNextField},
	{ContextConfirmation, "space", ActConfirmToggle},
	{ContextConfirmation, "shift+tab", ActConfirmCycleMode},

	// Tabs
	{ContextTabs, "tab", ActTabsNext},
	{ContextTabs, "shift+tab", ActTabsPrevious},
	{ContextTabs, "right", ActTabsNext},
	{ContextTabs, "left", ActTabsPrevious},

	// Transcript (less-style)
	{ContextTranscript, "ctrl+e", ActTranscriptToggleShowAll},
	{ContextTranscript, "ctrl+c", ActTranscriptExit},
	{ContextTranscript, "escape", ActTranscriptExit},
	{ContextTranscript, "q", ActTranscriptExit},
	{ContextTranscript, "ctrl+u", ActScrollHalfPageUp},
	{ContextTranscript, "ctrl+d", ActScrollHalfPageDown},
	{ContextTranscript, "ctrl+b", ActScrollFullPageUp},
	{ContextTranscript, "ctrl+f", ActScrollFullPageDown},
	{ContextTranscript, "ctrl+n", ActScrollLineDown},
	{ContextTranscript, "ctrl+p", ActScrollLineUp},
	{ContextTranscript, "g", ActScrollTop},
	{ContextTranscript, "shift+g", ActScrollBottom},
	{ContextTranscript, "j", ActScrollLineDown},
	{ContextTranscript, "k", ActScrollLineUp},
	{ContextTranscript, "space", ActScrollFullPageDown},
	{ContextTranscript, "b", ActScrollFullPageUp},
	{ContextTranscript, "up", ActScrollLineUp},
	{ContextTranscript, "down", ActScrollLineDown},
	{ContextTranscript, "home", ActScrollTop},
	{ContextTranscript, "end", ActScrollBottom},

	// HistorySearch
	{ContextHistorySearch, "ctrl+r", ActHistorySearchNext},
	{ContextHistorySearch, "escape", ActHistorySearchAccept},
	{ContextHistorySearch, "tab", ActHistorySearchAccept},
	{ContextHistorySearch, "ctrl+c", ActHistorySearchCancel},
	{ContextHistorySearch, "enter", ActHistorySearchExecute},
	{ContextHistorySearch, "ctrl+s", ActHistorySearchCycleScope},

	// Task
	{ContextTask, "ctrl+x ctrl+b", ActTaskBackground},
	{ContextTask, "ctrl+b", ActTaskBackground},

	// ThemePicker
	{ContextThemePicker, "ctrl+t", ActThemeToggleSyntaxHighlighting},
	{ContextThemePicker, "ctrl+e", ActThemeEditCustom},

	// Scroll (fullscreen)
	{ContextScroll, "pageup", ActScrollPageUp},
	{ContextScroll, "pagedown", ActScrollPageDown},
	{ContextScroll, "wheelup", ActScrollLineUp},
	{ContextScroll, "wheeldown", ActScrollLineDown},
	{ContextScroll, "ctrl+home", ActScrollTop},
	{ContextScroll, "ctrl+end", ActScrollBottom},
	{ContextScroll, "ctrl+shift+c", ActSelectionCopy},
	{ContextScroll, "cmd+c", ActSelectionCopy},
	{ContextScroll, "shift+left", ActSelectionExtendLeft},
	{ContextScroll, "shift+right", ActSelectionExtendRight},
	{ContextScroll, "shift+up", ActSelectionExtendUp},
	{ContextScroll, "shift+down", ActSelectionExtendDown},
	{ContextScroll, "shift+home", ActSelectionExtendLineStart},
	{ContextScroll, "shift+end", ActSelectionExtendLineEnd},

	// Help
	{ContextHelp, "escape", ActHelpDismiss},

	// Attachments
	{ContextAttachments, "right", ActAttachmentsNext},
	{ContextAttachments, "left", ActAttachmentsPrevious},
	{ContextAttachments, "backspace", ActAttachmentsRemove},
	{ContextAttachments, "delete", ActAttachmentsRemove},
	{ContextAttachments, "down", ActAttachmentsExit},
	{ContextAttachments, "escape", ActAttachmentsExit},

	// Footer
	{ContextFooter, "up", ActFooterUp},
	{ContextFooter, "ctrl+p", ActFooterUp},
	{ContextFooter, "down", ActFooterDown},
	{ContextFooter, "ctrl+n", ActFooterDown},
	{ContextFooter, "right", ActFooterNext},
	{ContextFooter, "left", ActFooterPrevious},
	{ContextFooter, "enter", ActFooterOpenSelected},
	{ContextFooter, "escape", ActFooterClearSelection},
	{ContextFooter, "x", ActFooterClose},

	// AbovePrompt (plugin panes above the prompt)
	{ContextAbovePrompt, "tab", ActAbovePromptNext},
	{ContextAbovePrompt, "right", ActAbovePromptNext},
	{ContextAbovePrompt, "shift+tab", ActAbovePromptPrevious},
	{ContextAbovePrompt, "left", ActAbovePromptPrevious},
	{ContextAbovePrompt, "enter", ActAbovePromptPress},
	{ContextAbovePrompt, "space", ActAbovePromptPress},
	{ContextAbovePrompt, "escape", ActAbovePromptLeave},
	{ContextAbovePrompt, "up", ActPaneScrollUp},
	{ContextAbovePrompt, "down", ActPaneScrollDown},
	{ContextAbovePrompt, "pageup", ActPanePageUp},
	{ContextAbovePrompt, "pagedown", ActPanePageDown},
	{ContextAbovePrompt, "home", ActPaneTop},
	{ContextAbovePrompt, "end", ActPaneBottom},

	// AbovePromptInput
	{ContextAbovePromptInput, "tab", ActAbovePromptNext},
	{ContextAbovePromptInput, "down", ActAbovePromptNext},
	{ContextAbovePromptInput, "shift+tab", ActAbovePromptPrevious},
	{ContextAbovePromptInput, "up", ActAbovePromptPrevious},
	{ContextAbovePromptInput, "enter", ActAbovePromptPress},
	{ContextAbovePromptInput, "escape", ActAbovePromptLeave},

	// AbovePromptSelect
	{ContextAbovePromptSelect, "tab", ActAbovePromptNext},
	{ContextAbovePromptSelect, "shift+tab", ActAbovePromptPrevious},
	{ContextAbovePromptSelect, "down", ActAbovePromptHighlightNext},
	{ContextAbovePromptSelect, "up", ActAbovePromptHighlightPrevious},
	{ContextAbovePromptSelect, "enter", ActAbovePromptPress},
	{ContextAbovePromptSelect, "escape", ActAbovePromptLeave},

	// Pane
	{ContextPane, "tab", ActAbovePromptNext},
	{ContextPane, "shift+tab", ActAbovePromptPrevious},
	{ContextPane, "enter", ActAbovePromptPress},
	{ContextPane, "escape", ActAbovePromptLeave},
	{ContextPane, "up", ActPaneScrollUp},
	{ContextPane, "down", ActPaneScrollDown},
	{ContextPane, "pageup", ActPanePageUp},
	{ContextPane, "pagedown", ActPanePageDown},
	{ContextPane, "home", ActPaneTop},
	{ContextPane, "end", ActPaneBottom},
	{ContextPane, "ctrl+x left", ActPaneGrow},
	{ContextPane, "ctrl+x up", ActPaneGrow},
	{ContextPane, "ctrl+x right", ActPaneShrink},
	{ContextPane, "ctrl+x down", ActPaneShrink},
	{ContextPane, "ctrl+x x", ActPaneClose},

	// PaneField
	{ContextPaneField, "ctrl+x x", ActPaneClose},

	// DiffDialog
	{ContextDiffDialog, "escape", ActDiffDismiss},
	{ContextDiffDialog, "left", ActDiffPreviousSource},
	{ContextDiffDialog, "right", ActDiffNextSource},
	{ContextDiffDialog, "up", ActDiffPreviousFile},
	{ContextDiffDialog, "down", ActDiffNextFile},
	{ContextDiffDialog, "j", ActDiffNextFile},
	{ContextDiffDialog, "k", ActDiffPreviousFile},
	{ContextDiffDialog, "pageup", ActScrollPageUp},
	{ContextDiffDialog, "pagedown", ActScrollPageDown},
	{ContextDiffDialog, "space", ActScrollFullPageDown},
	{ContextDiffDialog, "shift+space", ActScrollFullPageUp},
	{ContextDiffDialog, "b", ActScrollFullPageUp},
	{ContextDiffDialog, "g", ActScrollTop},
	{ContextDiffDialog, "shift+g", ActScrollBottom},
	{ContextDiffDialog, "home", ActScrollTop},
	{ContextDiffDialog, "end", ActScrollBottom},

	// ModelPicker
	{ContextModelPicker, "left", ActModelPickerDecreaseEffort},
	{ContextModelPicker, "right", ActModelPickerIncreaseEffort},
	{ContextModelPicker, "s", ActModelPickerThisSessionOnly},

	// EffortSlider
	{ContextEffortSlider, "left", ActEffortSliderDecreaseEffort},
	{ContextEffortSlider, "right", ActEffortSliderIncreaseEffort},
	{ContextEffortSlider, "tab", ActEffortSliderToggleUltracode},
	{ContextEffortSlider, "s", ActEffortSliderThisSessionOnly},

	// Select
	{ContextSelect, "up", ActSelectPrevious},
	{ContextSelect, "down", ActSelectNext},
	{ContextSelect, "j", ActSelectNext},
	{ContextSelect, "k", ActSelectPrevious},
	{ContextSelect, "ctrl+n", ActSelectNext},
	{ContextSelect, "ctrl+p", ActSelectPrevious},
	{ContextSelect, "pageup", ActSelectPageUp},
	{ContextSelect, "pagedown", ActSelectPageDown},
	{ContextSelect, "home", ActSelectFirst},
	{ContextSelect, "end", ActSelectLast},
	{ContextSelect, "enter", ActSelectAccept},
	{ContextSelect, "escape", ActSelectCancel},

	// Plugin
	{ContextPlugin, "space", ActPluginToggle},
	{ContextPlugin, "i", ActPluginInstall},
	{ContextPlugin, "f", ActPluginFavorite},
	{ContextPlugin, "ctrl+s", ActPluginCycleMarketplace},

	// Agents (agent view)
	{ContextAgents, "ctrl+s", ActAgentsSwitchView},
	{ContextAgents, "ctrl+t", ActAgentsTogglePin},
	{ContextAgents, "ctrl+f", ActAgentsFind},
	{ContextAgents, "ctrl+r", ActAgentsRename},
	{ContextAgents, "ctrl+up", ActAgentsPreviousGroup},
	{ContextAgents, "meta+up", ActAgentsPreviousGroup},
	{ContextAgents, "ctrl+down", ActAgentsNextGroup},
	{ContextAgents, "meta+down", ActAgentsNextGroup},
}

// ActionAliases maps legacy action IDs to the action that now implements them. A
// binding to the old ID behaves like a binding to the new one.
var ActionAliases = map[ActionID]ActionID{
	ActMessageSelectorUp:     ActSelectPrevious,
	ActMessageSelectorDown:   ActSelectNext,
	ActMessageSelectorTop:    ActSelectFirst,
	ActMessageSelectorBottom: ActSelectLast,
	ActMessageSelectorSelect: ActSelectAccept,
	ActDiffViewDetails:       ActSelectAccept,
}
