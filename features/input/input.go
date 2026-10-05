// Package input registers the prompt input: the editor (input.editor), its
// menus and panels (input.menu), the submit pipeline stages and the input
// actions (chat:submit, history:*, autocomplete:*, attachments:*, ...).
//
// Primary owner: plan 04 (docs/plans/04-input.md).
package input

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// IDs registered by this feature.
const (
	FeatureID   = "input.editor"
	ComponentID = "input.editor" // the prompt (SlotInput)
	MenuID      = "input.menu"   // menus, help and search status (SlotBelowInput)

	StageHistory     = "input.history"
	StageSlash       = "input.slash"
	StageBash        = "input.bash"
	StageAttachments = "input.attachments"
	StagePriority    = "input.priority"
	StageSend        = "input.send"
)

// Stage priorities (ascending).
const (
	PrioHistory     = 100
	PrioSlash       = 200
	PrioBash        = 300
	PrioAttachments = 400
	PrioPriority    = 500
	PrioSend        = 1000
)

// Mantle settings.
const (
	SettingVirtualCursor = "input.virtualCursor"
	SettingWriteHistory  = "input.writeHistory"
)

// ActRewind is plan 06's rewind action (double esc on an empty prompt).
const ActRewind ext.ActionID = "mantle:rewind"

// ActFooterSelect is chrome's action for down on the newest history entry.
const ActFooterSelect ext.ActionID = "mantle:footerSelect"

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 400,
		Parity: []string{
			"ED-01", "ED-02", "ED-03", "ED-04", "ED-05", "ED-06", "ED-07", "ED-08", "ED-09",
			"ED-10", "ED-11", "ED-12", "ED-13", "ED-14", "ED-15", "ED-16", "ED-17", "ED-18",
			"ED-19", "ED-20", "ED-21", "ED-22", "ED-23", "ED-24", "ED-25", "ED-26", "ED-27",
			"ED-28", "ED-29", "ED-31", "ED-32", "ED-33", "ED-34", "ED-36", "ED-37", "ED-38",
			"ED-39", "ED-40",
			"AC-01", "AC-02", "AC-03", "AC-04", "AC-05", "AC-06", "AC-07", "AC-08", "AC-09",
			"AC-11", "AC-12", "AC-13", "AC-16", "AC-17", "AC-21", "AC-22",
			"HI-01", "HI-02", "HI-03", "HI-04", "HI-05", "HI-06", "HI-07", "HI-08", "HI-09",
			"HI-10",
		},
		Setup: setup,
	})
}

func setup(r ext.Registrar) error {
	s := newState()
	register(r, s)
	return nil
}

// register wires a state into a registrar (separate from setup for tests).
func register(r ext.Registrar, s *state) {
	r.AddComponent(ext.SlotInput, &promptComp{s: s}, ext.SlotOpts{})
	r.AddComponent(ext.SlotBelowInput, &menuComp{s: s}, ext.SlotOpts{Weight: -100})

	for _, a := range ownedActions {
		id := a.id
		r.AddAction(ext.Action{
			ID: id, Context: a.context, Description: a.desc,
			Run: func(c ext.Ctx) (bool, tea.Cmd) {
				if c.Focused() != ComponentID {
					return false, nil
				}
				return s.action(c, id)
			},
		})
	}

	r.AddDialog(DialogHistorySearch, func(ext.Ctx, any) (ext.Dialog, error) { return &searchDialog{s: s}, nil })

	r.AddPromptStage(StageHistory, PrioHistory, s.stageHistory)
	r.AddPromptStage(StageSlash, PrioSlash, s.stageSlash)
	r.AddPromptStage(StageBash, PrioBash, s.stageBash)
	r.AddPromptStage(StageAttachments, PrioAttachments, s.stageAttachments)
	r.AddPromptStage(StagePriority, PrioPriority, s.stagePriority)
	r.AddPromptStage(StageSend, PrioSend, s.stageSend)

	r.AddSetting(ext.SettingSpec{
		Key: SettingVirtualCursor, Type: "bool", Default: false,
		Description: "Draw the prompt cursor as a styled cell instead of using the terminal cursor",
	})
	r.AddSetting(ext.SettingSpec{
		Key: SettingWriteHistory, Type: "bool", Default: true,
		Description: "Append submitted prompts to Claude Code's history.jsonl",
	})

	// /vim is plan 08's (it writes editorMode); the editor follows the
	// setting through SettingsMsg.

	r.OnStart(FeatureID+".start", s.start)
	for _, st := range stories(s) {
		r.AddStory(st)
	}
}

// ownedAction describes an action this feature implements.
type ownedAction struct {
	id      ext.ActionID
	context string
	desc    string
}

var ownedActions = []ownedAction{
	{ext.ActChatSubmit, ext.ContextChat, "Submit the prompt"},
	{ext.ActChatNewline, ext.ContextChat, "Insert a newline"},
	{ext.ActChatUndo, ext.ContextChat, "Undo the last edit"},
	{ext.ActChatClearInput, ext.ContextChat, "Clear the prompt"},
	{ext.ActChatQueueSubmit, ext.ContextChat, "Queue the prompt without interrupting"},
	{ext.ActChatSendNow, ext.ContextChat, "Send the prompt now, ending the current turn"},
	{ext.ActChatStash, ext.ContextChat, "Stash the prompt, or restore the stash"},
	{ext.ActChatExternalEditor, ext.ContextChat, "Edit the prompt in $EDITOR"},
	{ext.ActChatImagePaste, ext.ContextChat, "Paste an image from the clipboard"},
	{ext.ActChatWorkflowKeywordToggle, ext.ContextChat, "Toggle the ultracode keyword trigger"},
	{ext.ActHistoryPrevious, ext.ContextChat, "Previous prompt"},
	{ext.ActHistoryNext, ext.ContextChat, "Next prompt"},
	{ext.ActHistorySearch, ext.ContextGlobal, "Search prompt history"},
	{ext.ActHistorySearchNext, ext.ContextHistorySearch, "Next match"},
	{ext.ActHistorySearchAccept, ext.ContextHistorySearch, "Accept the match into the prompt"},
	{ext.ActHistorySearchCancel, ext.ContextHistorySearch, "Cancel the search"},
	{ext.ActHistorySearchExecute, ext.ContextHistorySearch, "Submit the match"},
	{ext.ActHistorySearchCycleScope, ext.ContextHistorySearch, "Cycle session, project and all history"},
	{ext.ActAutocompleteAccept, ext.ContextAutocomplete, "Accept the suggestion"},
	{ext.ActAutocompleteDismiss, ext.ContextAutocomplete, "Dismiss suggestions"},
	{ext.ActAutocompleteNext, ext.ContextAutocomplete, "Next suggestion"},
	{ext.ActAutocompletePrevious, ext.ContextAutocomplete, "Previous suggestion"},
	{ext.ActAttachmentsNext, ext.ContextAttachments, "Next attachment"},
	{ext.ActAttachmentsPrevious, ext.ContextAttachments, "Previous attachment"},
	{ext.ActAttachmentsRemove, ext.ContextAttachments, "Remove the attachment"},
	{ext.ActAttachmentsExit, ext.ContextAttachments, "Leave attachments"},
}
