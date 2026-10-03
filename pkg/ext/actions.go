package ext

// Action IDs, verbatim from Claude Code (keybindings.json uses them), grouped by family.
// Generated from the 2.1.288 action list; add new IDs here as Claude Code adds them.
const (
	// app:*
	ActAppCycleDiffBase         ActionID = "app:cycleDiffBase"
	ActAppDiffFileListDown      ActionID = "app:diffFileListDown"
	ActAppDiffFileListUp        ActionID = "app:diffFileListUp"
	ActAppExit                  ActionID = "app:exit"
	ActAppHelp                  ActionID = "app:help"
	ActAppInterrupt             ActionID = "app:interrupt"
	ActAppOpenArtifact          ActionID = "app:openArtifact"
	ActAppRedraw                ActionID = "app:redraw"
	ActAppToggleBrief           ActionID = "app:toggleBrief"
	ActAppToggleDiffNoiseFilter ActionID = "app:toggleDiffNoiseFilter"
	ActAppToggleDiffPreSession  ActionID = "app:toggleDiffPreSession"
	ActAppToggleReplTab         ActionID = "app:toggleReplTab"
	ActAppToggleTerminal        ActionID = "app:toggleTerminal"
	ActAppToggleTodos           ActionID = "app:toggleTodos"
	ActAppToggleTranscript      ActionID = "app:toggleTranscript"

	// chat:*
	ActChatAttentionDown         ActionID = "chat:attentionDown"
	ActChatAttentionUp           ActionID = "chat:attentionUp"
	ActChatCancel                ActionID = "chat:cancel"
	ActChatClearInput            ActionID = "chat:clearInput"
	ActChatClearScreen           ActionID = "chat:clearScreen"
	ActChatCycleMode             ActionID = "chat:cycleMode"
	ActChatCycleProactivity      ActionID = "chat:cycleProactivity"
	ActChatDecreaseEffort        ActionID = "chat:decreaseEffort"
	ActChatDefaultToNewerModel   ActionID = "chat:defaultToNewerModel"
	ActChatExternalEditor        ActionID = "chat:externalEditor"
	ActChatFastMode              ActionID = "chat:fastMode"
	ActChatImagePaste            ActionID = "chat:imagePaste"
	ActChatIncreaseEffort        ActionID = "chat:increaseEffort"
	ActChatKillAgents            ActionID = "chat:killAgents"
	ActChatModelPicker           ActionID = "chat:modelPicker"
	ActChatNewline               ActionID = "chat:newline"
	ActChatQueueSubmit           ActionID = "chat:queueSubmit"
	ActChatSendNow               ActionID = "chat:sendNow"
	ActChatStash                 ActionID = "chat:stash"
	ActChatSubmit                ActionID = "chat:submit"
	ActChatThinkingToggle        ActionID = "chat:thinkingToggle"
	ActChatUndo                  ActionID = "chat:undo"
	ActChatWorkflowKeywordToggle ActionID = "chat:workflowKeywordToggle"

	// history:*
	ActHistoryNext     ActionID = "history:next"
	ActHistoryPrevious ActionID = "history:previous"
	ActHistorySearch   ActionID = "history:search"

	// historySearch:*
	ActHistorySearchAccept     ActionID = "historySearch:accept"
	ActHistorySearchCancel     ActionID = "historySearch:cancel"
	ActHistorySearchCycleScope ActionID = "historySearch:cycleScope"
	ActHistorySearchExecute    ActionID = "historySearch:execute"
	ActHistorySearchNext       ActionID = "historySearch:next"

	// autocomplete:*
	ActAutocompleteAccept   ActionID = "autocomplete:accept"
	ActAutocompleteDismiss  ActionID = "autocomplete:dismiss"
	ActAutocompleteNext     ActionID = "autocomplete:next"
	ActAutocompletePrevious ActionID = "autocomplete:previous"

	// confirm:*
	ActConfirmCycleMode     ActionID = "confirm:cycleMode"
	ActConfirmNext          ActionID = "confirm:next"
	ActConfirmNextField     ActionID = "confirm:nextField"
	ActConfirmNo            ActionID = "confirm:no"
	ActConfirmPrevious      ActionID = "confirm:previous"
	ActConfirmPreviousField ActionID = "confirm:previousField"
	ActConfirmToggle        ActionID = "confirm:toggle"
	ActConfirmYes           ActionID = "confirm:yes"

	// select:*
	ActSelectAccept   ActionID = "select:accept"
	ActSelectCancel   ActionID = "select:cancel"
	ActSelectFirst    ActionID = "select:first"
	ActSelectLast     ActionID = "select:last"
	ActSelectNext     ActionID = "select:next"
	ActSelectPageDown ActionID = "select:pageDown"
	ActSelectPageUp   ActionID = "select:pageUp"
	ActSelectPrevious ActionID = "select:previous"

	// tabs:*
	ActTabsNext     ActionID = "tabs:next"
	ActTabsPrevious ActionID = "tabs:previous"

	// transcript:*
	ActTranscriptExit          ActionID = "transcript:exit"
	ActTranscriptToggleShowAll ActionID = "transcript:toggleShowAll"

	// scroll:*
	ActScrollBottom       ActionID = "scroll:bottom"
	ActScrollFullPageDown ActionID = "scroll:fullPageDown"
	ActScrollFullPageUp   ActionID = "scroll:fullPageUp"
	ActScrollHalfPageDown ActionID = "scroll:halfPageDown"
	ActScrollHalfPageUp   ActionID = "scroll:halfPageUp"
	ActScrollLineDown     ActionID = "scroll:lineDown"
	ActScrollLineUp       ActionID = "scroll:lineUp"
	ActScrollPageDown     ActionID = "scroll:pageDown"
	ActScrollPageUp       ActionID = "scroll:pageUp"
	ActScrollTop          ActionID = "scroll:top"

	// selection:*
	ActSelectionClear           ActionID = "selection:clear"
	ActSelectionCopy            ActionID = "selection:copy"
	ActSelectionExtendDown      ActionID = "selection:extendDown"
	ActSelectionExtendLeft      ActionID = "selection:extendLeft"
	ActSelectionExtendLineEnd   ActionID = "selection:extendLineEnd"
	ActSelectionExtendLineStart ActionID = "selection:extendLineStart"
	ActSelectionExtendRight     ActionID = "selection:extendRight"
	ActSelectionExtendUp        ActionID = "selection:extendUp"

	// task:*
	ActTaskBackground ActionID = "task:background"

	// theme:*
	ActThemeEditCustom               ActionID = "theme:editCustom"
	ActThemeToggleSyntaxHighlighting ActionID = "theme:toggleSyntaxHighlighting"

	// help:*
	ActHelpDismiss ActionID = "help:dismiss"

	// attachments:*
	ActAttachmentsExit     ActionID = "attachments:exit"
	ActAttachmentsNext     ActionID = "attachments:next"
	ActAttachmentsPrevious ActionID = "attachments:previous"
	ActAttachmentsRemove   ActionID = "attachments:remove"

	// footer:*
	ActFooterClearSelection ActionID = "footer:clearSelection"
	ActFooterClose          ActionID = "footer:close"
	ActFooterDismiss        ActionID = "footer:dismiss"
	ActFooterDown           ActionID = "footer:down"
	ActFooterNext           ActionID = "footer:next"
	ActFooterOpenSelected   ActionID = "footer:openSelected"
	ActFooterPrevious       ActionID = "footer:previous"
	ActFooterUp             ActionID = "footer:up"

	// abovePrompt:*
	ActAbovePromptFocus             ActionID = "abovePrompt:focus"
	ActAbovePromptHighlightNext     ActionID = "abovePrompt:highlightNext"
	ActAbovePromptHighlightPrevious ActionID = "abovePrompt:highlightPrevious"
	ActAbovePromptLeave             ActionID = "abovePrompt:leave"
	ActAbovePromptNext              ActionID = "abovePrompt:next"
	ActAbovePromptPress             ActionID = "abovePrompt:press"
	ActAbovePromptPrevious          ActionID = "abovePrompt:previous"
	ActAbovePromptToggle            ActionID = "abovePrompt:toggle"

	// pane:*
	ActPaneBottom     ActionID = "pane:bottom"
	ActPaneClose      ActionID = "pane:close"
	ActPaneGrow       ActionID = "pane:grow"
	ActPaneNext       ActionID = "pane:next"
	ActPanePageDown   ActionID = "pane:pageDown"
	ActPanePageUp     ActionID = "pane:pageUp"
	ActPanePrevious   ActionID = "pane:previous"
	ActPaneScrollDown ActionID = "pane:scrollDown"
	ActPaneScrollUp   ActionID = "pane:scrollUp"
	ActPaneShrink     ActionID = "pane:shrink"
	ActPaneTop        ActionID = "pane:top"

	// diff:*
	ActDiffBack           ActionID = "diff:back"
	ActDiffDismiss        ActionID = "diff:dismiss"
	ActDiffNextFile       ActionID = "diff:nextFile"
	ActDiffNextSource     ActionID = "diff:nextSource"
	ActDiffPreviousFile   ActionID = "diff:previousFile"
	ActDiffPreviousSource ActionID = "diff:previousSource"
	ActDiffViewDetails    ActionID = "diff:viewDetails"

	// modelPicker:*
	ActModelPickerDecreaseEffort  ActionID = "modelPicker:decreaseEffort"
	ActModelPickerIncreaseEffort  ActionID = "modelPicker:increaseEffort"
	ActModelPickerThisSessionOnly ActionID = "modelPicker:thisSessionOnly"

	// effortSlider:*
	ActEffortSliderDecreaseEffort  ActionID = "effortSlider:decreaseEffort"
	ActEffortSliderIncreaseEffort  ActionID = "effortSlider:increaseEffort"
	ActEffortSliderThisSessionOnly ActionID = "effortSlider:thisSessionOnly"
	ActEffortSliderToggleUltracode ActionID = "effortSlider:toggleUltracode"

	// settings:*
	ActSettingsPeriodDay    ActionID = "settings:periodDay"
	ActSettingsPeriodWeek   ActionID = "settings:periodWeek"
	ActSettingsRetry        ActionID = "settings:retry"
	ActSettingsSearch       ActionID = "settings:search"
	ActSettingsSortByTokens ActionID = "settings:sortByTokens"

	// plugin:*
	ActPluginCycleMarketplace ActionID = "plugin:cycleMarketplace"
	ActPluginFavorite         ActionID = "plugin:favorite"
	ActPluginInstall          ActionID = "plugin:install"
	ActPluginToggle           ActionID = "plugin:toggle"

	// agents:*
	ActAgentsFind          ActionID = "agents:find"
	ActAgentsNextGroup     ActionID = "agents:nextGroup"
	ActAgentsPreviousGroup ActionID = "agents:previousGroup"
	ActAgentsRename        ActionID = "agents:rename"
	ActAgentsSetGroup      ActionID = "agents:setGroup"
	ActAgentsSwitchView    ActionID = "agents:switchView"
	ActAgentsTogglePin     ActionID = "agents:togglePin"

	// messageSelector:*
	ActMessageSelectorBottom ActionID = "messageSelector:bottom"
	ActMessageSelectorDown   ActionID = "messageSelector:down"
	ActMessageSelectorSelect ActionID = "messageSelector:select"
	ActMessageSelectorTop    ActionID = "messageSelector:top"
	ActMessageSelectorUp     ActionID = "messageSelector:up"

	// voice:*
	ActVoicePushToTalk ActionID = "voice:pushToTalk"
)

// ClaudeActions lists every Claude Code action ID mantle knows, for validation of
// keybindings.json and for /keybindings.
var ClaudeActions = []ActionID{
	ActAppCycleDiffBase, ActAppDiffFileListDown, ActAppDiffFileListUp, ActAppExit, ActAppHelp, ActAppInterrupt, ActAppOpenArtifact, ActAppRedraw, ActAppToggleBrief, ActAppToggleDiffNoiseFilter, ActAppToggleDiffPreSession, ActAppToggleReplTab, ActAppToggleTerminal, ActAppToggleTodos, ActAppToggleTranscript,
	ActChatAttentionDown, ActChatAttentionUp, ActChatCancel, ActChatClearInput, ActChatClearScreen, ActChatCycleMode, ActChatCycleProactivity, ActChatDecreaseEffort, ActChatDefaultToNewerModel, ActChatExternalEditor, ActChatFastMode, ActChatImagePaste, ActChatIncreaseEffort, ActChatKillAgents, ActChatModelPicker, ActChatNewline, ActChatQueueSubmit, ActChatSendNow, ActChatStash, ActChatSubmit, ActChatThinkingToggle, ActChatUndo, ActChatWorkflowKeywordToggle,
	ActHistoryNext, ActHistoryPrevious, ActHistorySearch,
	ActHistorySearchAccept, ActHistorySearchCancel, ActHistorySearchCycleScope, ActHistorySearchExecute, ActHistorySearchNext,
	ActAutocompleteAccept, ActAutocompleteDismiss, ActAutocompleteNext, ActAutocompletePrevious,
	ActConfirmCycleMode, ActConfirmNext, ActConfirmNextField, ActConfirmNo, ActConfirmPrevious, ActConfirmPreviousField, ActConfirmToggle, ActConfirmYes,
	ActSelectAccept, ActSelectCancel, ActSelectFirst, ActSelectLast, ActSelectNext, ActSelectPageDown, ActSelectPageUp, ActSelectPrevious,
	ActTabsNext, ActTabsPrevious,
	ActTranscriptExit, ActTranscriptToggleShowAll,
	ActScrollBottom, ActScrollFullPageDown, ActScrollFullPageUp, ActScrollHalfPageDown, ActScrollHalfPageUp, ActScrollLineDown, ActScrollLineUp, ActScrollPageDown, ActScrollPageUp, ActScrollTop,
	ActSelectionClear, ActSelectionCopy, ActSelectionExtendDown, ActSelectionExtendLeft, ActSelectionExtendLineEnd, ActSelectionExtendLineStart, ActSelectionExtendRight, ActSelectionExtendUp,
	ActTaskBackground,
	ActThemeEditCustom, ActThemeToggleSyntaxHighlighting,
	ActHelpDismiss,
	ActAttachmentsExit, ActAttachmentsNext, ActAttachmentsPrevious, ActAttachmentsRemove,
	ActFooterClearSelection, ActFooterClose, ActFooterDismiss, ActFooterDown, ActFooterNext, ActFooterOpenSelected, ActFooterPrevious, ActFooterUp,
	ActAbovePromptFocus, ActAbovePromptHighlightNext, ActAbovePromptHighlightPrevious, ActAbovePromptLeave, ActAbovePromptNext, ActAbovePromptPress, ActAbovePromptPrevious, ActAbovePromptToggle,
	ActPaneBottom, ActPaneClose, ActPaneGrow, ActPaneNext, ActPanePageDown, ActPanePageUp, ActPanePrevious, ActPaneScrollDown, ActPaneScrollUp, ActPaneShrink, ActPaneTop,
	ActDiffBack, ActDiffDismiss, ActDiffNextFile, ActDiffNextSource, ActDiffPreviousFile, ActDiffPreviousSource, ActDiffViewDetails,
	ActModelPickerDecreaseEffort, ActModelPickerIncreaseEffort, ActModelPickerThisSessionOnly,
	ActEffortSliderDecreaseEffort, ActEffortSliderIncreaseEffort, ActEffortSliderThisSessionOnly, ActEffortSliderToggleUltracode,
	ActSettingsPeriodDay, ActSettingsPeriodWeek, ActSettingsRetry, ActSettingsSearch, ActSettingsSortByTokens,
	ActPluginCycleMarketplace, ActPluginFavorite, ActPluginInstall, ActPluginToggle,
	ActAgentsFind, ActAgentsNextGroup, ActAgentsPreviousGroup, ActAgentsRename, ActAgentsSetGroup, ActAgentsSwitchView, ActAgentsTogglePin,
	ActMessageSelectorBottom, ActMessageSelectorDown, ActMessageSelectorSelect, ActMessageSelectorTop, ActMessageSelectorUp,
	ActVoicePushToTalk,
}
