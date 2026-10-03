package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Manager owns every engine and delivers their messages to the UI (B6).
//
// Wiring in the host:
//
//	m := engine.NewManager(program.Send)
//	e, err := m.Start(ext.MainEngine, opts) // after the startup gates passed
//	defer m.Close(context.Background())
//
// Each engine has one delivery goroutine, so its messages (EngineAttachMsg,
// EngineEventMsg, PermissionMsg, SessionChangedMsg, EngineExitedMsg, ...) reach the
// program in stdout order. Results of Engine.Control and Send arrive as the Cmd's
// own message.
type Manager struct {
	send func(tea.Msg)

	// Binary is the claude executable; "" means FindBinary.
	Binary string
	// Spawner starts processes; nil means ExecSpawner.
	Spawner Spawner
	// RunDir holds one <pid>.json per live engine (pid, pgid) for the launcher;
	// "" means ~/.mantle/run, "-" disables.
	RunDir string
	// DialogKinds is sent as initialize.supportedDialogKinds: only kinds a feature
	// really implements (plans 05/09).
	DialogKinds []string
	// CoalesceInterval for stream deltas; 0 means DefaultCoalesceInterval.
	CoalesceInterval time.Duration
	// Tap, if set, sees every stdin/stdout line of every engine (debug, recorder).
	Tap func(dir Direction, line []byte)
	// Logf receives debug diagnostics (unknown messages, shape drift, late replies).
	Logf func(format string, args ...any)

	mu      sync.Mutex
	engines map[string]*Engine
}

// NewManager returns a manager delivering messages with send (tea.Program.Send).
func NewManager(send func(tea.Msg)) *Manager {
	return &Manager{send: send, engines: map[string]*Engine{}}
}

func (m *Manager) spawner() Spawner {
	if m.Spawner == nil {
		return ExecSpawner{}
	}
	return m.Spawner
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

// Engine returns the engine with id ("" = main), or nil.
func (m *Manager) Engine(id string) *Engine {
	if id == "" {
		id = ext.MainEngine
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.engines[id]
}

// Engines returns every engine id.
func (m *Manager) Engines() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.engines))
	for id := range m.engines {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Start spawns engine id ("" = main) with o. It returns once the process runs; the
// initialize handshake continues in the background (its result arrives as a
// ControlResultMsg with Subtype "initialize"). Prompts sent right away are queued
// behind initialize.
func (m *Manager) Start(id string, o ext.SpawnOpts) (*Engine, error) {
	if id == "" {
		id = ext.MainEngine
	}
	m.mu.Lock()
	e := m.engines[id]
	if e == nil {
		e = &Engine{id: id, mgr: m, pump: newPump(m.send)}
		m.engines[id] = e
	}
	m.mu.Unlock()
	if err := e.start(o); err != nil {
		return e, err
	}
	return e, nil
}

// StartCmd is Start as a Cmd; a failure arrives as EngineExitedMsg.
func (m *Manager) StartCmd(id string, o ext.SpawnOpts) tea.Cmd {
	return func() tea.Msg {
		if _, err := m.Start(id, o); err != nil {
			if id == "" {
				id = ext.MainEngine
			}
			return ext.EngineExitedMsg{EngineID: id, Err: err}
		}
		return nil
	}
}

// Remove stops engine id and forgets it (EngineDetachMsg follows its exit).
func (m *Manager) Remove(ctx context.Context, id string) {
	if id == "" {
		id = ext.MainEngine
	}
	m.mu.Lock()
	e := m.engines[id]
	delete(m.engines, id)
	m.mu.Unlock()
	if e == nil {
		return
	}
	e.Stop(ctx)
	e.pump.Enqueue([]tea.Msg{ext.EngineDetachMsg{EngineID: id}})
	e.pump.Close()
}

// Close stops every engine (in parallel) and waits for their final messages.
func (m *Manager) Close(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.engines))
	for id := range m.engines {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			m.Remove(ctx, id)
		}(id)
	}
	wg.Wait()
}

// runRecord is ~/.mantle/run/<pid>.json, read by the launcher to kill orphaned
// engine process groups after a crash.
type runRecord struct {
	PID      int       `json:"pid"`
	PGID     int       `json:"pgid"`
	UIPID    int       `json:"ui_pid"`
	EngineID string    `json:"engine_id"`
	Binary   string    `json:"binary"`
	Started  time.Time `json:"started"`
}

func (m *Manager) runDir() string {
	if m.RunDir != "" {
		return m.RunDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "-"
	}
	return filepath.Join(home, ".mantle", "run")
}

func (m *Manager) writeRunFile(engineID, bin string, pid int) string {
	dir := m.runDir()
	if dir == "-" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		m.logf("engine: run dir: %v", err)
		return ""
	}
	b, _ := json.MarshalIndent(runRecord{PID: pid, PGID: pid, UIPID: os.Getpid(), EngineID: engineID, Binary: bin, Started: time.Now()}, "", "  ")
	path := filepath.Join(dir, fmt.Sprintf("%d.json", pid))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		m.logf("engine: run file: %v", err)
		return ""
	}
	return path
}

func (m *Manager) removeRunFile(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}
