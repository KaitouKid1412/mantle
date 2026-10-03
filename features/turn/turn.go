//go:build turnwip

// Part B work in progress (plan 05): excluded from normal builds until the remaining
// pieces (modes.go, control.go, queue.go, gateflow.go, stories.go) exist.

package turn

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// FeatureID is the turn area's feature.
const FeatureID = "turn"

// Dialog IDs this feature registers. Mods can Replace or Wrap any of them.
const (
	DialogPermission      = "dialog.permission"
	DialogAskUserQuestion = "dialog.askUserQuestion"
	DialogPlanApproval    = "dialog.planApproval"
	DialogElicitation     = "dialog.elicitation"
	DialogTrust           = "dialog.trust"
	DialogMcpApproval     = "dialog.mcpApproval"
	DialogBypassWarning   = "dialog.bypassWarning"
	DialogAPIKey          = "dialog.apiKey"
	DialogAutoMode        = "dialog.autoMode"
	DialogUsageLimit      = "dialog.usageLimit"
)

// Component IDs.
const (
	QueueComponentID = "turn.queue"
)

// doublePressWindow is how long the second ctrl+c / ctrl+d press may take.
const doublePressWindow = 800 * time.Millisecond

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 100,
		Parity: []string{
			"TC-01", "TC-02", "TC-03", "TC-04", "TC-05", "TC-11", "TC-12", "TC-15", "TC-16",
			"TC-17", "TC-18", "TC-19", "TC-20",
			"PD-01", "PD-02", "PD-03", "PD-04", "PD-05", "PD-06", "PD-07", "PD-08", "PD-09",
			"PD-10", "PD-11", "PD-12", "PD-13", "PD-14", "PD-16", "PD-17", "PD-19", "PD-20",
			"PD-21", "PD-22", "PD-24", "PD-25", "PD-27", "PD-28", "PD-29", "PD-30", "PD-31",
			"PD-32", "PD-33", "PD-34", "PD-35", "PD-37", "PD-38", "PD-41", "PD-43", "PD-45",
		},
		Setup: func(r ext.Registrar) error {
			newState().setup(r)
			return nil
		},
	})
}

// state is the feature's UI-goroutine state, shared by its registrations. One instance
// per Setup, so tests get a fresh one.
type state struct {
	reqs     requestQueue
	sessions map[string]ext.SessionInfo // non-main engines (main comes from Ctx.Session)
	running  map[string]bool
	stateEv  map[string]bool // the engine emits session_state_changed
	modes    map[string]*modeState
	agents   map[string]string // task/agent id → display name
	tasks    map[string][]proto.BackgroundTask
	presses  map[ext.ActionID]time.Time
	queued   map[string][]queuedPrompt
	limit    *limitWait
	limitGen int
	seenLim  map[string]bool
	gates    gateQueue
	trusted  map[string]bool // session-only trust (home directory)

	// env locates gate files; tests replace it.
	env func() (gates.Env, error)
}

func newState() *state {
	return &state{
		sessions: map[string]ext.SessionInfo{},
		running:  map[string]bool{},
		stateEv:  map[string]bool{},
		modes:    map[string]*modeState{},
		agents:   map[string]string{},
		tasks:    map[string][]proto.BackgroundTask{},
		presses:  map[ext.ActionID]time.Time{},
		queued:   map[string][]queuedPrompt{},
		seenLim:  map[string]bool{},
		trusted:  map[string]bool{},
		env:      gates.FromOS,
	}
}

func (st *state) setup(r ext.Registrar) {
	st.setupRequests(r)
	st.setupModes(r)
	st.setupControl(r)
	st.setupQueue(r)
	st.setupGates(r)
	st.setupStories(r)

	ext.Subscribe(r, "turn.sessions", func(c ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
		if m.EngineID != "" && m.EngineID != ext.MainEngine {
			st.sessions[m.EngineID] = m.Info
		}
		return st.maybeStartupMode(c, engineKey(m.EngineID))
	})
	ext.Subscribe(r, "turn.events", st.onEngineEvent)
	ext.Subscribe(r, "turn.controlResults", st.onControlResult)
	ext.Subscribe(r, "turn.attach", func(c ext.Ctx, m ext.EngineAttachMsg) tea.Cmd {
		return st.onAttach(c, engineKey(m.EngineID), m.Engine)
	})
	ext.Subscribe(r, "turn.detach", func(c ext.Ctx, m ext.EngineDetachMsg) tea.Cmd {
		return st.engineGone(c, engineKey(m.EngineID))
	})
	ext.Subscribe(r, "turn.exited", func(c ext.Ctx, m ext.EngineExitedMsg) tea.Cmd {
		return st.engineGone(c, engineKey(m.EngineID))
	})
}

// engineKey normalizes "" to the main engine's ID.
func engineKey(id string) string {
	if id == "" {
		return ext.MainEngine
	}
	return id
}

// session returns the SessionInfo for an engine.
func (st *state) session(c ext.Ctx, engineID string) ext.SessionInfo {
	engineID = engineKey(engineID)
	if engineID == ext.MainEngine {
		return c.Session()
	}
	return st.sessions[engineID]
}

func (st *state) engineGone(c ext.Ctx, engineID string) tea.Cmd {
	delete(st.running, engineID)
	delete(st.tasks, engineID)
	delete(st.queued, engineID)
	if engineID == ext.MainEngine {
		c.SetContextActive(ext.ContextTask, false)
		c.Invalidate(QueueComponentID)
	}
	return st.dropRequests(c, engineID)
}
