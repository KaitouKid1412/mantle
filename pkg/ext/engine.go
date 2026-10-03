package ext

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// MainEngine is the ID of the main session's engine.
const MainEngine = "main"

// Engine is a running claude process (plan 02 implements it). Every method returns a
// Cmd; results arrive as messages carrying the engine's ID.
type Engine interface {
	Send(Prompt) tea.Cmd
	Interrupt(cancelQueued bool) tea.Cmd
	Control(subtype string, req any) tea.Cmd // -> ControlResultMsg
	Supports(subtype string) bool
	Restart(SpawnOpts) tea.Cmd // resume / fork / resume-at; stop+handoff via SpawnOpts
}

// Prompt is a user message sent to the engine.
type Prompt struct {
	Blocks   []proto.ContentBlock
	Priority string // "now" | "next" | "later" ("" = engine default)
	UUID     string
	Composed bool // client_composed: no @/slash expansion
}

// SpawnOpts configures an engine (re)start.
type SpawnOpts struct {
	Cwd             string
	Model           string
	PermissionMode  string
	Resume          string // session ID to resume
	Continue        bool   // continue the most recent session in Cwd
	ForkSession     bool
	ResumeSessionAt string // message UUID to resume at
	ResumeDropsTurn bool
	SessionID       string // explicit ID for a new session
	Name            string // -n/--name
	AddDirs         []string
	Settings        string   // --settings JSON (startup gates, e.g. disabledMcpjsonServers)
	ExtraArgs       []string // user claude flags forwarded verbatim (plan 11)
	Env             map[string]string
	SafeMode        bool
	Stop            bool // stop without restarting (hand-off to interactive claude)
}

// Messages from engines. All carry EngineID.
type (
	// EngineEventMsg is one stdout message (stream deltas are coalesced).
	EngineEventMsg struct {
		EngineID string
		Event    proto.Event // deltas coalesced
	}
	// ControlResultMsg answers Engine.Control.
	ControlResultMsg struct {
		EngineID, Subtype, RequestID string
		Resp                         json.RawMessage
		Err                          error
	}
	// PermissionMsg is a can_use_tool request (permissions, AskUserQuestion,
	// ExitPlanMode). Call Reply exactly once, unless a ControlCancelMsg with the same
	// RequestID arrives first.
	PermissionMsg struct {
		EngineID  string
		RequestID string
		Req       proto.CanUseTool
		Reply     func(proto.PermissionResult) tea.Cmd
	}
	// ControlRequestMsg is any other CLI-originated control request (hook_callback,
	// elicitation, request_user_dialog, mcp_message). Call Reply exactly once with the
	// response payload, or an error, unless cancelled first.
	ControlRequestMsg struct {
		EngineID, Subtype, RequestID string
		Request                      json.RawMessage
		Reply                        func(resp any, err error) tea.Cmd
	}
	// ControlCancelMsg withdraws a CLI-originated request: close its dialog, don't reply.
	ControlCancelMsg struct {
		EngineID, RequestID string
	}
	// EngineExitedMsg reports that an engine process ended.
	EngineExitedMsg struct {
		EngineID string
		Err      error  // nil on a clean exit
		Stderr   string // tail of the stderr ring buffer
	}
	// EngineAttachMsg registers a started engine with the host, so Ctx.Engine(id)
	// returns it. Whoever spawns an engine sends this; the host also delivers it to
	// subscribers.
	EngineAttachMsg struct {
		EngineID string
		Engine   Engine
	}
	// EngineDetachMsg removes an engine from the host (after it exited or was stopped).
	EngineDetachMsg struct {
		EngineID string
	}
)

// ExitMsg asks the host to quit with an exit code. 0 is a normal exit; 75 asks the
// launcher to restart mantle (see docs/plans/00-overview.md).
type ExitMsg struct {
	Code   int
	Reason string
}

// ExitRestart is the exit code that asks the launcher to restart mantle-ui.
const ExitRestart = 75
