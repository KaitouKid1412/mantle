package ext

// Additions in contracts-v1.11 (request 13-01, research mode).

// UIModeResearch is the research UI mode (plan 13).
const UIModeResearch = "research"

// ContextResearch is the keybinding context for research mode. It is mantle-only and
// ambient: plan 13 activates it (Ctx ambient contexts) only while research mode is on.
// Its bindings live in ~/.mantle/keybindings.json, like ContextMantle's.
const ContextResearch = "Research"

// EnvResearch, when set, starts mantle in research mode (plan 13).
const EnvResearch = "MANTLE_RESEARCH"

// UIModeRequestMsg asks for a UI mode switch. Mode "" leaves the current UI mode.
// Any feature may send it; plan 13 consumes it and answers with UIModeChangedMsg.
type UIModeRequestMsg struct {
	Mode string
}

// UIModeChangedMsg reports that the UI mode changed from Prev to Mode ("" is the
// default mode). Produced by plan 13; consumed by plans 05 and 07.
type UIModeChangedMsg struct {
	Mode, Prev string
}

// TranscriptScopeMsg scopes the transcript view to Source on behalf of Owner (a
// feature ID). A nil Source removes the scope. Produced by plan 13; consumed by
// plan 12 (fullscreen viewport).
type TranscriptScopeMsg struct {
	Owner  string
	Source Transcript
}

// SelectionMsg reports the text selected in view Source. Text "" means the selection
// was cleared. Produced by plan 12; consumed by plan 13.
type SelectionMsg struct {
	Source, Text string
}

// EditorQuoteMsg sets the prompt's quote block: the leading run of "> " lines is
// replaced by Text, or Text is inserted at the top when there is none. Produced by
// plan 13; consumed by plan 04.
type EditorQuoteMsg struct {
	Text string
}

// BranchRequestMsg asks to continue session SessionID of engine EngineID from message
// At (a JSONL uuid), so the next prompt becomes a sibling branch. DropPrompt, when set,
// is the prompt uuid to rewind away (fast path, without a restart). Tag is echoed in
// BranchedMsg. Produced by plan 13; consumed by plan 06.
type BranchRequestMsg struct {
	EngineID, SessionID, At, DropPrompt, Tag string
}

// BranchedMsg answers a BranchRequestMsg. Restarted reports that the engine was
// restarted (resume-at) rather than rewound in place. Err is set on failure.
// Produced by plan 06; consumed by plan 13.
type BranchedMsg struct {
	EngineID, SessionID, At, Tag string
	Restarted                    bool
	Err                          error
}
