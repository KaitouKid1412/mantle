package engine

import (
	"reflect"
	"sync"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Snapshot is everything the tracker knows about an engine's session.
type Snapshot struct {
	Info  ext.SessionInfo
	State string // idle | running | requires_action ("" before the first event)

	// Newest system/init (tools, MCP servers, skills, plugins, agents, ...).
	Init *proto.SystemInit
	// Initialize response (commands, models, account, output styles).
	Initialize *proto.InitializeResponse
	// Commands is the current slash command list (initialize, then commands_changed).
	Commands []proto.SlashCommand
	// BackgroundTasks is the latest background_tasks_changed list.
	BackgroundTasks []proto.BackgroundTask
	FastModeState   string
}

// tracker follows session state from engine events (B7). Methods are safe for
// concurrent use; observe runs on the reader goroutine.
type tracker struct {
	mu sync.RWMutex
	s  Snapshot
	// live is set once an event reported mode/style; the initialize reply (which can
	// be processed later) must not overwrite newer values.
	live bool
}

func newTracker(engineID string, o ext.SpawnOpts) *tracker {
	t := &tracker{}
	t.s.Info = ext.SessionInfo{EngineID: engineID, Cwd: o.Cwd, Model: o.Model, PermissionMode: o.PermissionMode}
	if o.Resume != "" && !o.ForkSession {
		t.s.Info.SessionID = o.Resume
	}
	if o.SessionID != "" {
		t.s.Info.SessionID = o.SessionID
	}
	return t
}

// Snapshot returns a copy of the tracked state.
func (t *tracker) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := t.s
	s.Commands = append([]proto.SlashCommand(nil), t.s.Commands...)
	s.BackgroundTasks = append([]proto.BackgroundTask(nil), t.s.BackgroundTasks...)
	return s
}

// Info returns the current SessionInfo.
func (t *tracker) Info() ext.SessionInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s.Info
}

// Observe updates state from ev and reports whether Info changed.
func (t *tracker) Observe(ev proto.Event) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	before := t.s.Info
	i := &t.s.Info
	switch e := ev.(type) {
	case *proto.SystemInit:
		t.s.Init = e
		t.live = true
		setIf(&i.SessionID, e.SessionID)
		setIf(&i.Cwd, e.CWD)
		setIf(&i.Model, e.Model)
		setIf(&i.PermissionMode, e.PermissionMode)
		setIf(&i.ClaudeVersion, e.ClaudeCodeVersion)
		setIf(&i.OutputStyle, e.OutputStyle)
		setIf(&t.s.FastModeState, e.FastModeState)
	case *proto.Status:
		if e.PermissionMode != "" {
			t.live = true
		}
		setIf(&i.PermissionMode, e.PermissionMode)
	case *proto.ConversationReset:
		setIf(&i.SessionID, e.NewConversationID)
		i.Title = ""
	case *proto.SessionTitleChanged:
		i.Title = e.Title
	case *proto.SessionStateChanged:
		t.s.State = e.State
	case *proto.CommandsChanged:
		t.s.Commands = e.Commands
	case *proto.BackgroundTasksChanged:
		t.s.BackgroundTasks = e.Tasks
	case *proto.Result:
		setIf(&t.s.FastModeState, e.FastModeState)
	default:
		if env := ev.Env(); env.SessionID != "" && i.SessionID == "" {
			i.SessionID = env.SessionID
		}
	}
	return !reflect.DeepEqual(*i, before)
}

// ObserveInitialize records the initialize response; reports whether Info changed.
func (t *tracker) ObserveInitialize(r *proto.InitializeResponse) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	before := t.s.Info
	t.s.Initialize = r
	t.s.Commands = r.Commands
	if !t.live {
		setIf(&t.s.Info.PermissionMode, r.CurrentPermissionMode)
		setIf(&t.s.Info.OutputStyle, r.OutputStyle)
	}
	setIf(&t.s.FastModeState, r.FastModeState)
	setIf(&t.s.State, r.SessionState)
	return !reflect.DeepEqual(t.s.Info, before)
}

// restore sets Info from a hand-off; later events update it as usual.
func (t *tracker) restore(info ext.SessionInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.s.Info = info
	t.live = true
}

// SetPermissionMode and SetModel record successful control changes.
func (t *tracker) SetPermissionMode(m string) bool {
	return t.set(func(i *ext.SessionInfo) { i.PermissionMode = m })
}
func (t *tracker) SetModel(m string) bool { return t.set(func(i *ext.SessionInfo) { i.Model = m }) }
func (t *tracker) SetTitle(s string) bool { return t.set(func(i *ext.SessionInfo) { i.Title = s }) }

func (t *tracker) set(f func(*ext.SessionInfo)) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	before := t.s.Info
	f(&t.s.Info)
	return !reflect.DeepEqual(t.s.Info, before)
}

func setIf(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
