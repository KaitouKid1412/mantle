package settings

import (
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

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
