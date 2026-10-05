package settings

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestNewerModelOffer(t *testing.T) {
	g := newRig(t)
	// Without an outdated pin, ctrl+y falls through.
	if g.action(ext.ActChatDefaultToNewerModel) {
		t.Fatal("ctrl+y handled with nothing offered")
	}
	g.c.SettingsV.ClaudeM["model"] = "claude-opus-4-6"
	g.loadModels()
	if !strings.Contains(lastNotice(g), "claude-opus-4-6 is your default model. Press ctrl+y to make Opus your default.") {
		t.Fatalf("offer %q", lastNotice(g))
	}
	// The same session does not offer twice.
	n := len(g.c.Notices)
	g.loadModels()
	if len(g.c.Notices) != n {
		t.Error("offered twice")
	}
	if !g.action(ext.ActChatDefaultToNewerModel) {
		t.Fatal("ctrl+y not handled")
	}
	if p := jsonOf(g.eng.controls[len(g.eng.controls)-1].Payload); p != `{"model":"opus"}` {
		t.Errorf("set_model %s", p)
	}
	if v := g.applied()[patch.User]["model"]; v != "opus" {
		t.Errorf("saved %v", v)
	}
	if g.action(ext.ActChatDefaultToNewerModel) {
		t.Error("second ctrl+y handled")
	}
}

func TestNewerModelOfferLimits(t *testing.T) {
	// Unbound key: no offer.
	g := newRig(t)
	g.c.Bindings = nil
	g.c.SettingsV.ClaudeM["model"] = "claude-opus-4-6"
	g.loadModels()
	for _, n := range g.c.Notices {
		if n.Key == newerModelNotice {
			t.Error("offered without a key")
		}
	}
	// At most three offers across sessions.
	g2 := newRig(t)
	g2.c.SettingsV.ClaudeM["model"] = "claude-opus-4-6"
	offers := 0
	for i := 0; i < 5; i++ {
		g2.a.newerOfferedFor = "" // a new session
		before := len(g2.c.Notices)
		g2.loadModels()
		if len(g2.c.Notices) > before && g2.c.Notices[len(g2.c.Notices)-1].Key == newerModelNotice {
			offers++
		}
	}
	if offers != newerModelMaxAsks {
		t.Errorf("offered %d times", offers)
	}
}
