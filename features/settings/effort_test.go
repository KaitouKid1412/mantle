package settings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func lastNotice(g *rig) string {
	if len(g.c.Notices) == 0 {
		return ""
	}
	return g.c.Notices[len(g.c.Notices)-1].Text
}

func TestEffortSlider(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"
	g.command("effort", "")
	g.mustContain(100, "For Opus", "● high", "Ultracode  off")
	g.press(ext.ActEffortSliderDecreaseEffort, ext.ActEffortSliderToggleUltracode)
	g.mustContain(100, "● medium", "Ultracode  on")
	g.press(ext.ActSelectAccept)
	if g.dialog != nil {
		t.Fatal("slider still open")
	}
	want := []string{proto.SubApplyFlagSettings, proto.SubUpdateSettings, proto.SubApplyFlagSettings}
	if !reflect.DeepEqual(g.eng.subtypes(), want) {
		t.Fatalf("controls %v", g.eng.subtypes())
	}
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"settings":{"effortLevel":"medium"},"source":"userSettings"}` {
		t.Errorf("update_settings %s", p)
	}
	if p := jsonOf(g.eng.controls[2].Payload); p != `{"settings":{"ultracode":true}}` {
		t.Errorf("ultracode flag %s", p)
	}
	if jsonOf(g.applied()[patch.User]) != `{"ultracode":true}` {
		t.Errorf("user settings %s", jsonOf(g.applied()[patch.User]))
	}
}

func TestEffortPersistsToModelSettingsWhenPresent(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"
	g.c.SettingsV.ClaudeM["modelSettings"] = map[string]any{"claude-opus-5-5": map[string]any{"effortLevel": "xhigh"}}
	g.command("effort", "low")
	if !reflect.DeepEqual(g.eng.subtypes(), []string{proto.SubApplyFlagSettings}) {
		t.Errorf("controls %v", g.eng.subtypes())
	}
	if jsonOf(g.applied()[patch.User]) != `{"modelSettings":{"claude-opus-5-5":{"effortLevel":"low"}}}` {
		t.Errorf("user settings %s", jsonOf(g.applied()[patch.User]))
	}
	if lastNotice(g) != "Effort set to low" {
		t.Errorf("notice %q", lastNotice(g))
	}
}

func TestEffortCommandVariants(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "sonnet"
	g.command("effort", "max") // clamped to sonnet's levels
	if p := jsonOf(g.eng.controls[0].Payload); p != `{"settings":{"effortLevel":"high"}}` {
		t.Errorf("clamp %s", p)
	}
	g.command("effort", "auto")
	if !reflect.DeepEqual(g.eng.sent, []string{"/effort auto"}) {
		t.Errorf("passthrough %v", g.eng.sent)
	}
	g.command("effort", "ultracode off")
	if jsonOf(g.eng.controls[len(g.eng.controls)-1].Payload) != `{"settings":{"ultracode":false}}` {
		t.Errorf("ultracode off %v", g.eng.controls)
	}

	// max on a model that has it is applied but not saved.
	g2 := newRig(t)
	g2.loadModels()
	g2.c.SessionValue.Model = "opus"
	g2.command("effort", "max")
	if len(g2.writes) != 0 || !strings.Contains(strings.Join(g2.noticeTexts(), "|"), "this session") {
		t.Errorf("max: writes %v notices %q", g2.writes, g2.noticeTexts())
	}
	for _, c := range g2.eng.controls {
		if c.Subtype == proto.SubUpdateSettings {
			t.Error("max sent to update_settings")
		}
	}
}

func TestEffortKeysAreSessionOnly(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"
	g.action(ext.ActChatIncreaseEffort)
	g.action(ext.ActChatIncreaseEffort)
	if len(g.eng.controls) != 2 || len(g.writes) != 0 {
		t.Fatalf("controls %v writes %v", g.eng.subtypes(), g.writes)
	}
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"settings":{"effortLevel":"max"}}` {
		t.Errorf("second step %s", p)
	}
	// At the top already: handled, nothing sent.
	g.action(ext.ActChatIncreaseEffort)
	if len(g.eng.controls) != 2 {
		t.Error("stepped past max")
	}
	// A model without levels lets the key fall through.
	g.c.SessionValue.Model = "haiku"
	if g.action(ext.ActChatDecreaseEffort) {
		t.Error("haiku handled an effort key")
	}
	// A restarted engine forgets session choices.
	g.deliver(ext.EngineAttachMsg{EngineID: ext.MainEngine})
	if g.a.sessionEffort != "" {
		t.Error("session effort survived a restart")
	}
}

func TestFastMode(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"
	g.command("fast", "")
	if p := jsonOf(g.eng.controls[0].Payload); p != `{"settings":{"fastMode":true}}` {
		t.Errorf("fast on %s", p)
	}
	if jsonOf(g.applied()[patch.User]) != `{"fastMode":true}` || lastNotice(g) != "Fast mode on" {
		t.Errorf("persist %s / %q", jsonOf(g.applied()[patch.User]), lastNotice(g))
	}
	g.action(ext.ActChatFastMode) // toggles off
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"settings":{"fastMode":false}}` {
		t.Errorf("fast off %s", p)
	}
	if _, ok := g.applied()[patch.User]["fastMode"]; ok {
		t.Error("fast off should unset fastMode")
	}

	// Per-session opt-in: applied but never saved.
	g2 := newRig(t)
	g2.loadModels()
	g2.c.SessionValue.Model = "opus"
	g2.c.SettingsV.ClaudeM["fastModePerSessionOptIn"] = true
	g2.command("fast", "on")
	if len(g2.writes) != 0 || len(g2.eng.controls) != 1 {
		t.Errorf("per-session opt-in wrote %v", g2.writes)
	}

	// Unsupported model or a disabled reason refuses to turn it on.
	g3 := newRig(t)
	g3.loadModels()
	g3.c.SessionValue.Model = "sonnet"
	g3.command("fast", "on")
	if len(g3.eng.controls) != 0 || !strings.Contains(lastNotice(g3), "unavailable") {
		t.Errorf("sonnet fast: %v %q", g3.eng.subtypes(), lastNotice(g3))
	}
	g3.a.engine.sys = &proto.SystemInit{Model: "opus", FastModeDisabledReason: "not on this plan"}
	g3.command("fast", "on")
	if !strings.Contains(lastNotice(g3), "not on this plan") {
		t.Errorf("reason %q", lastNotice(g3))
	}
	g3.command("fast", "sideways")
	if !strings.Contains(lastNotice(g3), "/fast on") {
		t.Errorf("usage %q", lastNotice(g3))
	}
}

func TestThinkingToggle(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus" // adaptive thinking: locked
	g.action(ext.ActChatThinkingToggle)
	if len(g.eng.controls) != 0 || !strings.Contains(lastNotice(g), "always on") {
		t.Errorf("locked: %v %q", g.eng.subtypes(), lastNotice(g))
	}
	g.c.SessionValue.Model = "haiku"
	g.action(ext.ActChatThinkingToggle)
	g.action(ext.ActChatThinkingToggle)
	if len(g.eng.controls) != 2 ||
		jsonOf(g.eng.controls[0].Payload) != `{"settings":{"alwaysThinkingEnabled":false}}` ||
		jsonOf(g.eng.controls[1].Payload) != `{"settings":{"alwaysThinkingEnabled":true}}` {
		t.Errorf("toggles %v", g.eng.controls)
	}
	if len(g.writes) != 0 {
		t.Error("thinking toggle wrote settings")
	}
}
