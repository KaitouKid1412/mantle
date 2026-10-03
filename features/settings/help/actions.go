package help

import (
	"strings"
	"unicode"
)

// actionText describes common actions in mantle's words. Others fall back to Humanize.
var actionText = map[string]string{
	"app:interrupt":                  "Interrupt Claude, or clear the prompt",
	"app:exit":                       "Exit",
	"app:toggleTodos":                "Show or hide the task list",
	"app:toggleTranscript":           "Open the detailed transcript",
	"app:toggleBrief":                "Toggle brief mode",
	"app:redraw":                     "Redraw the screen",
	"app:help":                       "Open help",
	"history:search":                 "Search prompt history",
	"history:previous":               "Previous prompt",
	"history:next":                   "Next prompt",
	"chat:submit":                    "Send the prompt",
	"chat:cancel":                    "Cancel or go back",
	"chat:newline":                   "Insert a new line",
	"chat:cycleMode":                 "Cycle permission mode",
	"chat:modelPicker":               "Choose the model",
	"chat:fastMode":                  "Toggle fast mode",
	"chat:thinkingToggle":            "Toggle extended thinking",
	"chat:increaseEffort":            "Raise effort",
	"chat:decreaseEffort":            "Lower effort",
	"chat:defaultToNewerModel":       "Switch the default to the newer model",
	"chat:externalEditor":            "Edit the prompt in $EDITOR",
	"chat:stash":                     "Stash or restore the prompt",
	"chat:imagePaste":                "Paste an image",
	"chat:killAgents":                "Stop all background agents",
	"chat:queueSubmit":               "Queue the prompt",
	"chat:sendNow":                   "Send now, interrupting the turn",
	"chat:undo":                      "Undo the last edit",
	"chat:clearInput":                "Clear the prompt",
	"chat:clearScreen":               "Clear the screen",
	"chat:workflowKeywordToggle":     "Toggle the workflow keyword",
	"task:background":                "Move the running task to the background",
	"transcript:toggleShowAll":       "Show everything in the transcript",
	"transcript:exit":                "Leave the transcript",
	"autocomplete:accept":            "Accept the suggestion",
	"autocomplete:dismiss":           "Dismiss suggestions",
	"confirm:yes":                    "Confirm",
	"confirm:no":                     "Decline",
	"select:accept":                  "Choose",
	"select:cancel":                  "Cancel",
	"modelPicker:thisSessionOnly":    "Use for this session only",
	"effortSlider:toggleUltracode":   "Toggle ultracode",
	"theme:toggleSyntaxHighlighting": "Toggle syntax highlighting",
	"theme:editCustom":               "Edit the custom theme",
	"voice:pushToTalk":               "Hold to dictate",
}

// ActionDescription returns a short description of an action ID.
func ActionDescription(id string) string {
	if s, ok := actionText[id]; ok {
		return s
	}
	return Humanize(id)
}

// Humanize turns "chat:modelPicker" into "Model picker".
func Humanize(id string) string {
	name := id
	if i := strings.LastIndexByte(id, ':'); i >= 0 {
		name = id[i+1:]
	}
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte(' ')
			r = unicode.ToLower(r)
		}
		if i == 0 {
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
