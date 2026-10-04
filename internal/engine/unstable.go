package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Undocumented control subtypes. They exist in the engine binary but have no public
// schema, so they may change in any release. They live only here, never in pkg/proto.
// Each is supported only on versions it was verified on (and turns off when the
// engine rejects it); callers must use the documented fallback otherwise.
type unstable struct {
	verifiedOn string
	fallback   string
}

var unstableSubtypes = map[string]unstable{
	SubRewindConversation: {"2.1.288", "restart with --resume-session-at=<uuid>"},
	SubForkConversation:   {"2.1.288", "restart with --resume=<id> --fork-session"},
	SubExportConversation: {"2.1.288", "read the session JSONL (plan 06)"},
	SubGetStatus:          {"2.1.288", "compose /status from init data and get_usage"},
	SubSideQuestion:       {"2.1.288", "ask in a forked engine (/btw)"},
	SubSetCwd:             {"2.1.288", "restart the engine in the new cwd"},
	SubAddDirectory:       {"2.1.288", "send /add-dir as a prompt, or restart with --add-dir"},
	SubMCPAuthenticate:    {"2.1.288", "hand off to `claude mcp`"},
	SubMCPClearAuth:       {"2.1.288", "hand off to `claude mcp`"},
	SubClaudeAuthenticate: {"2.1.288", "hand off to `claude auth login`"},
	SubGetMemoryDialog:    {"2.1.288", "list memory files from get_context_usage"},
	SubGetSkillsDialog:    {"2.1.288", "list skills from initialize/init"},
	SubGetSandboxDialog:   {"2.1.288", "read sandbox settings with get_settings"},
	SubGetWorkspaceDiff:   {"2.1.288", "run git diff"},
	SubListDirectory:      {"2.1.288", "read the directory locally"},
}

// Unstable control subtypes.
const (
	SubRewindConversation = "rewind_conversation"
	SubForkConversation   = "fork_conversation"
	SubExportConversation = "export_conversation"
	SubGetStatus          = "get_status"
	SubSideQuestion       = "side_question"
	SubSetCwd             = "set_cwd"
	SubAddDirectory       = "add_directory"
	SubMCPAuthenticate    = "mcp_authenticate"
	SubMCPClearAuth       = "mcp_clear_auth"
	SubClaudeAuthenticate = "claude_authenticate"
	SubGetMemoryDialog    = "get_memory_dialog"
	SubGetSkillsDialog    = "get_skills_dialog"
	SubGetSandboxDialog   = "get_sandbox_dialog"
	SubGetWorkspaceDiff   = "get_workspace_diff"
	SubListDirectory      = "list_directory"
)

// UnstableFallback returns the documented fallback for an unstable subtype.
func UnstableFallback(subtype string) string { return unstableSubtypes[subtype].fallback }

// RewindConversationRequest rewinds the conversation to a message (unstable; field
// names from the 2.1.288 binary).
type RewindConversationRequest struct {
	TargetMessageUUID       string `json:"target_message_uuid"`
	InterruptIfRunning      bool   `json:"interrupt_if_running,omitempty"`
	LastSeenUserMessageUUID string `json:"last_seen_user_message_uuid,omitempty"`
}

// ControlSubtype implements proto.Request.
func (RewindConversationRequest) ControlSubtype() string { return SubRewindConversation }

var _ proto.Request = RewindConversationRequest{}

// RewindResult is the rewind_conversation reply (seen on 2.1.289).
type RewindResult struct {
	Rewound bool `json:"rewound"`
	// TargetMessageUUID is the user message that was removed, with everything after.
	TargetMessageUUID string `json:"targetMessageUuid"`
	// PrefillText is that message's text, to put back into the prompt editor.
	PrefillText string `json:"prefillText,omitempty"`
	// PrecedingAssistantUUID is the last kept entry.
	PrecedingAssistantUUID string `json:"precedingAssistantUuid,omitempty"`
	// Restarted is true when the fallback (restart with --resume-session-at) ran.
	Restarted bool `json:"-"`
}

// ErrNeedsTranscript means the fallback needs data only the session JSONL has.
var ErrNeedsTranscript = errors.New("engine: rewind fallback needs the preceding assistant uuid (read the session JSONL)")

// Rewind removes the user message target and everything after it. It uses
// rewind_conversation when supported. Otherwise it restarts the engine with
// --resume=<session> --resume-session-at=<precedingAssistant> --resume-drops-turn,
// for which the caller must pass the uuid of the last entry to keep (plan 06 reads it
// from the JSONL); without it the fallback returns ErrNeedsTranscript.
func (e *Engine) Rewind(ctx context.Context, target, precedingAssistant string) (RewindResult, error) {
	var r RewindResult
	if e.Supports(SubRewindConversation) {
		raw, err := e.Request(ctx, RewindConversationRequest{TargetMessageUUID: target, InterruptIfRunning: true})
		if err == nil {
			err = json.Unmarshal(raw, &r)
			return r, err
		}
		if !proto.IsUnsupported(err) {
			return r, err
		}
	}
	if precedingAssistant == "" {
		return r, ErrNeedsTranscript
	}
	o := e.Options()
	sid := e.Snapshot().Info.SessionID
	if sid == "" {
		return r, errors.New("engine: rewind fallback: no session id yet")
	}
	o.Resume, o.Continue, o.ForkSession, o.SessionID = sid, false, false, ""
	o.ResumeSessionAt, o.ResumeDropsTurn = precedingAssistant, true
	if err := e.RestartNow(ctx, o); err != nil {
		return r, err
	}
	return RewindResult{Rewound: true, TargetMessageUUID: target, PrecedingAssistantUUID: precedingAssistant, Restarted: true}, nil
}

// SideQuestionRequest asks a question about the conversation without adding it
// (the /btw side question).
type SideQuestionRequest struct {
	Question string             `json:"question"`
	History  []SideQuestionTurn `json:"history"`
}

// SideQuestionTurn is an earlier side question and its answer.
type SideQuestionTurn struct {
	Question string `json:"question"`
	Response string `json:"response"`
}

// ControlSubtype implements proto.Request.
func (SideQuestionRequest) ControlSubtype() string { return SubSideQuestion }

// SideAnswer is the side_question reply.
type SideAnswer struct {
	Response  string `json:"response"`
	Synthetic bool   `json:"synthetic,omitempty"`
	// Forked is true when the fallback (a forked engine) answered.
	Forked bool `json:"-"`
}

// SideQuestion answers a question about the conversation without adding it to the
// session. It uses side_question when supported; otherwise it forks the session
// into a throwaway engine (no session file), asks there and stops it.
func (e *Engine) SideQuestion(ctx context.Context, question string, history []SideQuestionTurn) (SideAnswer, error) {
	var a SideAnswer
	if history == nil {
		history = []SideQuestionTurn{}
	}
	if e.Supports(SubSideQuestion) {
		raw, err := e.Request(ctx, SideQuestionRequest{Question: question, History: history})
		if err == nil {
			err = json.Unmarshal(raw, &a)
			return a, err
		}
		if !proto.IsUnsupported(err) {
			return a, err
		}
	}
	return e.askForked(ctx, question)
}

// askForked asks question in a fork of e's session, in a private Manager whose
// messages never reach the UI.
func (e *Engine) askForked(ctx context.Context, question string) (SideAnswer, error) {
	sid := e.Snapshot().Info.SessionID
	o := e.Options()
	if sid != "" {
		o.Resume, o.ForkSession = sid, true
	}
	o.Continue, o.SessionID, o.ResumeSessionAt, o.ResumeDropsTurn = false, "", "", false
	o.ExtraArgs = append(append([]string(nil), o.ExtraArgs...), "--no-session-persistence")
	id := uuid.NewString()
	done := make(chan *proto.Result, 1)
	m := NewManager(func(msg tea.Msg) {
		if ev, ok := msg.(ext.EngineEventMsg); ok {
			if r, ok := ev.Event.(*proto.Result); ok && (r.UserMessageUUID == id || slices.Contains(r.UserMessageUUIDs, id)) {
				select {
				case done <- r:
				default:
				}
			}
		}
	})
	m.Binary, m.Spawner, m.RunDir = e.mgr.Binary, e.mgr.Spawner, e.mgr.RunDir
	defer m.Close(context.Background())
	fork, err := m.Start("side-question", o)
	if err != nil {
		return SideAnswer{}, err
	}
	if err := fork.SendPrompt(ext.Prompt{UUID: id, Blocks: []proto.ContentBlock{proto.Text(question)}}); err != nil {
		return SideAnswer{}, err
	}
	select {
	case r := <-done:
		if r.IsError {
			return SideAnswer{}, fmt.Errorf("engine: side question failed: %s", r.Result)
		}
		return SideAnswer{Response: r.Result, Forked: true}, nil
	case <-ctx.Done():
		return SideAnswer{}, ctx.Err()
	}
}

// WorkspaceDiff is the get_workspace_diff reply (seen on 2.1.289), also produced by
// the git fallback.
type WorkspaceDiff struct {
	Stats struct {
		FilesCount   int `json:"filesCount"`
		LinesAdded   int `json:"linesAdded"`
		LinesRemoved int `json:"linesRemoved"`
	} `json:"stats"`
	PerFileStats []FileDiffStat  `json:"perFileStats"`
	Hunks        []FileDiffHunks `json:"hunks"`
	SkippedLarge json.RawMessage `json:"skippedLarge,omitempty"`
	Restricted   json.RawMessage `json:"restricted,omitempty"`
	Source       *DiffSource     `json:"source,omitempty"`
	Raw          json.RawMessage `json:"-"`
	FromGit      bool            `json:"-"` // the git fallback produced it
}

// FileDiffStat is one file's line counts.
type FileDiffStat struct {
	Path        string `json:"path"`
	Added       int    `json:"added"`
	Removed     int    `json:"removed"`
	IsBinary    bool   `json:"isBinary"`
	IsUntracked bool   `json:"isUntracked"`
}

// FileDiffHunks is one file's hunks.
type FileDiffHunks struct {
	Path  string     `json:"path"`
	Hunks []DiffHunk `json:"hunks"`
}

// DiffHunk is one unified-diff hunk; Lines keep their +/-/space prefix.
type DiffHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// DiffSource says what the diff was taken against.
type DiffSource struct {
	Kind string `json:"kind"` // working-tree, ...
}

// WorkspaceDiff returns the workspace git diff (/diff). It uses get_workspace_diff
// when supported, else runs git diff (against HEAD) in the engine's cwd.
func (e *Engine) WorkspaceDiff(ctx context.Context) (WorkspaceDiff, error) {
	if e.Supports(SubGetWorkspaceDiff) {
		raw, err := e.Request(ctx, proto.RawRequest{Subtype: SubGetWorkspaceDiff})
		if err == nil {
			var wrap struct {
				Diff json.RawMessage `json:"diff"`
			}
			var d WorkspaceDiff
			if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Diff) > 0 {
				err = json.Unmarshal(wrap.Diff, &d)
				d.Raw = wrap.Diff
				return d, err
			}
		} else if !proto.IsUnsupported(err) {
			return WorkspaceDiff{}, err
		}
	}
	dir := e.Snapshot().Info.Cwd
	if dir == "" {
		dir = e.Options().Cwd
	}
	return GitWorkspaceDiff(ctx, dir)
}
