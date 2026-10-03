// Package eco holds what the ecosystem panels share: the engine-state tracker,
// async and exec helpers, engine helpers, and the panel widgets (dialog stack,
// list, text field, confirm). It is internal to features/ecosystem.
package eco

import (
	"slices"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// EngineState is what the panels need to know about one engine, collected from
// its stream: the newest system/init, the command list and background tasks.
type EngineState struct {
	Init     *proto.SystemInit
	Commands []proto.SlashCommand // from commands_changed; nil until one arrives
	Tasks    []proto.BackgroundTask
}

// Tracker caches EngineState per engine. It is updated and read only on the UI
// goroutine (subscriptions, Update, View), so it needs no locking.
type Tracker struct {
	engines map[string]*EngineState
}

// MCPCounts summarises MCP server states from system/init.
type MCPCounts struct {
	Total, Connected, NeedsAuth, Failed int
}

// State is the process-wide tracker, fed by the ecosystem.state feature.
var State = NewTracker()

// NewTracker returns an empty tracker.
func NewTracker() *Tracker { return &Tracker{engines: map[string]*EngineState{}} }

func engineKey(id string) string {
	if id == "" {
		return ext.MainEngine
	}
	return id
}

// Engine returns the state for an engine ("" = main); never nil.
func (t *Tracker) Engine(id string) *EngineState {
	id = engineKey(id)
	s, ok := t.engines[id]
	if !ok {
		s = &EngineState{}
		t.engines[id] = s
	}
	return s
}

// Observe records one engine event. For a system/init it returns the MCP
// counts before and after, and ok=true.
func (t *Tracker) Observe(engineID string, ev proto.Event) (before, after MCPCounts, ok bool) {
	s := t.Engine(engineID)
	switch e := ev.(type) {
	case *proto.SystemInit:
		before = s.MCPCounts()
		s.Init = e
		return before, s.MCPCounts(), true
	case *proto.CommandsChanged:
		s.Commands = e.Commands
	case *proto.BackgroundTasksChanged:
		s.Tasks = e.Tasks
	}
	return MCPCounts{}, MCPCounts{}, false
}

// Forget drops an engine's state (after it detached).
func (t *Tracker) Forget(engineID string) { delete(t.engines, engineKey(engineID)) }

// MCPCounts counts MCP server states from the newest init.
func (s *EngineState) MCPCounts() MCPCounts {
	var c MCPCounts
	if s.Init == nil {
		return c
	}
	for _, m := range s.Init.MCPServers {
		c.Total++
		switch strings.ToLower(m.Status) {
		case "connected":
			c.Connected++
		case "needs-auth":
			c.NeedsAuth++
		case "failed":
			c.Failed++
		}
	}
	return c
}

// HasCommand reports whether the engine accepts a slash command headlessly
// (name without "/"). The newest commands_changed wins over init's list.
func (s *EngineState) HasCommand(name string) bool {
	name = strings.TrimPrefix(name, "/")
	if s.Commands != nil {
		for _, c := range s.Commands {
			if c.Name == name || slices.Contains(c.Aliases, name) {
				return true
			}
		}
		return false
	}
	return s.Init != nil && slices.Contains(s.Init.SlashCommands, name)
}

// Skills returns init.skills.
func (s *EngineState) Skills() []string {
	if s.Init == nil {
		return nil
	}
	return s.Init.Skills
}

// Agents returns init.agents.
func (s *EngineState) Agents() []string {
	if s.Init == nil {
		return nil
	}
	return s.Init.Agents
}

// Plugins returns init.plugins.
func (s *EngineState) Plugins() []proto.PluginInfo {
	if s.Init == nil {
		return nil
	}
	return s.Init.Plugins
}

// ForegroundTasks returns background tasks that a restart would kill (ambient
// ones excluded).
func (s *EngineState) ForegroundTasks() []proto.BackgroundTask {
	var out []proto.BackgroundTask
	for _, t := range s.Tasks {
		if !t.Ambient {
			out = append(out, t)
		}
	}
	return out
}
