package settings

import (
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestCommandVisibilityMessages(t *testing.T) {
	g := newRig(t)
	visibility := func() []map[string]bool {
		var out []map[string]bool
		for _, m := range g.msgs {
			if v, ok := m.(ext.CommandVisibilityMsg); ok {
				if v.Source != visibilitySource {
					t.Errorf("source %q", v.Source)
				}
				out = append(out, v.Hidden)
			}
		}
		return out
	}
	g.loadModels() // initialize: opus offers fast mode; no command list yet
	if v := visibility(); len(v) != 1 || v[0]["fast"] || v[0]["advisor"] {
		t.Fatalf("after initialize %v", v)
	}
	// commands_changed without the advisor twin hides /advisor (a /login that turns the
	// feature off restarts the engine and lands here too).
	ev := &proto.CommandsChanged{Commands: []proto.SlashCommand{{Name: "compact"}}}
	g.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	if v := visibility(); len(v) != 2 || !v[1]["advisor"] || v[1]["fast"] {
		t.Fatalf("after commands_changed %v", v)
	}
	// Nothing changed: nothing sent.
	g.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	if v := visibility(); len(v) != 2 {
		t.Errorf("resent unchanged overlay: %v", v)
	}
}

func TestCommandAvailability(t *testing.T) {
	a := newArea()
	if h := a.commandAvailability(); h["fast"] || h["advisor"] {
		t.Errorf("hidden before initialize: %v", h)
	}
	// Models without fast mode, engine list without the advisor twin.
	a.engine.models = []model.Info{{Value: "sonnet"}, {Value: "haiku"}}
	a.engine.commands = []proto.SlashCommand{{Name: "compact"}}
	if h := a.commandAvailability(); !h["fast"] || !h["advisor"] {
		t.Errorf("should hide both: %v", h)
	}
	a.engine.models = append(a.engine.models, model.Info{Value: "opus", SupportsFastMode: true})
	a.engine.commands = append(a.engine.commands, proto.SlashCommand{Name: "advisor"})
	if h := a.commandAvailability(); h["fast"] || h["advisor"] {
		t.Errorf("should show both: %v", h)
	}
}
