package settings

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestModelPickerApplyAndPersist(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"

	g.command("model", "")
	if g.dlgID != dialogModel {
		t.Fatalf("dialog %q", g.dlgID)
	}
	if !reflect.DeepEqual(g.eng.subtypes(), []string{proto.SubListModels}) {
		t.Errorf("controls on open %v", g.eng.subtypes())
	}
	g.mustContain(100, "Model", "❯ 3. Opus", "✔", "●●○○○ medium effort (default)", "←/→ to adjust")

	g.press(ext.ActSelectNext)                                                  // Opus (1M context)
	g.press(ext.ActModelPickerIncreaseEffort, ext.ActModelPickerIncreaseEffort) // medium → xhigh
	g.mustContain(100, "❯ 4. Opus (1M context)", "●●●●○ xhigh effort")
	g.press(ext.ActSelectAccept)

	if g.dialog != nil {
		t.Fatal("dialog still open")
	}
	want := []string{proto.SubListModels, proto.SubSetModel, proto.SubApplyFlagSettings}
	if !reflect.DeepEqual(g.eng.subtypes(), want) {
		t.Fatalf("controls %v", g.eng.subtypes())
	}
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"model":"opus[1m]"}` {
		t.Errorf("set_model %s", p)
	}
	if p := jsonOf(g.eng.controls[2].Payload); p != `{"settings":{"effortLevel":"xhigh"}}` {
		t.Errorf("flag %s", p)
	}
	user := g.applied()[patch.User]
	if jsonOf(user) != `{"model":"opus[1m]","modelSettings":{"claude-opus-5-5":{"effortLevel":"xhigh"}}}` {
		t.Errorf("user settings %s", jsonOf(user))
	}
	if p := g.printed(); len(p) != 1 || p[0] != "  ⎿  Model set to Opus (1M context) with xhigh effort" {
		t.Errorf("printed %q", p)
	}
	if res, ok := g.closed[0].Result.(interface{}); !ok || res == nil {
		t.Error("no dialog result")
	}
}

func TestModelPickerSessionOnly(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("model", "")
	// Cursor starts on the default row (no session model); go to Sonnet.
	g.key(tea.KeyPressMsg{Code: '5', Text: "5"})
	g.mustContain(100, "❯ 5. Sonnet")
	g.press(ext.ActModelPickerThisSessionOnly)
	if len(g.writes) != 0 {
		t.Errorf("session-only wrote %v", g.writes)
	}
	if p := g.printed(); len(p) != 1 || !strings.HasSuffix(p[0], "for this session") {
		t.Errorf("printed %q", p)
	}
}

func TestModelPickerCancelAndDisabled(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("model", "")
	g.press(ext.ActSelectLast) // skips past the disabled row? last selectable is Haiku
	g.mustContain(100, "❯ 6. Haiku", "not adjustable")
	g.press(ext.ActSelectCancel)
	if g.dialog != nil || len(g.writes) != 0 || len(g.eng.controls) != 1 {
		t.Errorf("cancel had effects: %v %v", g.writes, g.eng.subtypes())
	}
	if p := g.printed(); len(p) != 1 || p[0] != "  ⎿  Kept model as Default (recommended)" {
		t.Errorf("printed %q", p)
	}
}

func TestModelSwitchWarning(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.c.SessionValue.Model = "opus"
	g.c.TranscriptV = fakeTranscript{items: []*ext.Item{{ID: "1", State: ext.Done}}}
	g.command("model", "")
	g.press(ext.ActSelectNext, ext.ActSelectNext) // Sonnet
	g.press(ext.ActSelectAccept)
	if g.dialog == nil {
		t.Fatal("closed without warning")
	}
	g.mustContain(100, "Switch to Sonnet?")
	if got := g.dialog.(ext.ContextStack).KeyContexts(); !reflect.DeepEqual(got, []string{ext.ContextConfirmation}) {
		t.Errorf("contexts %v", got)
	}
	g.press(ext.ActConfirmNo)
	g.mustContain(100, "❯ 5. Sonnet")
	g.press(ext.ActSelectAccept, ext.ActConfirmYes)
	if g.dialog != nil || jsonOf(g.applied()[patch.User]) != `{"model":"sonnet"}` {
		t.Errorf("after confirm: %v %s", g.dialog, jsonOf(g.applied()[patch.User]))
	}

	// Re-choosing the current model never warns; the warning can be turned off.
	g2 := newRig(t)
	g2.loadModels()
	g2.c.SessionValue.Model = "opus"
	g2.c.TranscriptV = g.c.TranscriptV
	g2.c.SettingsV.MantleM[settingModelSwitchWarning] = false
	g2.command("model", "")
	g2.press(ext.ActSelectNext, ext.ActSelectAccept)
	if g2.dialog != nil {
		t.Error("warning shown although disabled")
	}
}

func TestModelCommandWithArgument(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("model", "Sonnet")
	if g.dialog != nil {
		t.Fatal("dialog opened for /model <name>")
	}
	if p := jsonOf(g.eng.controls[0].Payload); p != `{"model":"sonnet"}` {
		t.Errorf("set_model %s", p)
	}
	g.command("model", "claude-custom-model-x")
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"model":"claude-custom-model-x"}` {
		t.Errorf("unknown model passthrough %s", p)
	}
	g.command("model", "default")
	if p := jsonOf(g.eng.controls[2].Payload); p != `{"model":"default"}` {
		t.Errorf("default %s", p)
	}
	if _, ok := g.applied()[patch.User]["model"]; ok {
		t.Error("default should unset model")
	}
	comps := g.a.completeModel(g.c, "op")
	if len(comps) != 2 || comps[0].Value != "opus" {
		t.Errorf("completions %+v", comps)
	}
}

func TestModelPickerAction(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	if !g.action(ext.ActChatModelPicker) || g.dlgID != dialogModel {
		t.Fatal("meta+p did not open the picker")
	}
}

func TestModelPickerWithoutEngine(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	delete(g.c.Engines, ext.MainEngine)
	g.command("model", "")
	g.press(ext.ActSelectNext, ext.ActSelectAccept)
	// The choice is still saved, and the user is told it applies later.
	if len(g.writes) != 1 {
		t.Fatalf("writes %v", g.writes)
	}
	found := false
	for _, n := range g.c.Notices {
		if n.Key == "settings.no-engine" {
			found = true
		}
	}
	if !found {
		t.Errorf("notices %q", g.noticeTexts())
	}
}

func TestModelPickerLoading(t *testing.T) {
	g := newRig(t)
	g.eng.missing[proto.SubListModels] = true
	g.command("model", "")
	g.mustContain(80, "Default (recommended)")
	// initialize arriving after the picker opened fills it at once.
	g.loadModels()
	g.mustContain(80, "Opus (1M context)")
	if strings.Contains(g.screen(80), "Loading") {
		t.Error("still loading after initialize")
	}
	// A later list_models answer keeps it filled.
	g.deliver(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubListModels,
		Resp: []byte(jsonOf(g.eng.responses[proto.SubListModels]))})
	g.mustContain(80, "Opus (1M context)")
}

func TestControlFailureNotice(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.eng.errs[proto.SubSetModel] = errInvalid("unknown model")
	g.command("model", "opus")
	var texts []string
	for _, n := range g.c.Notices {
		if n.Level == ext.NoticeError {
			texts = append(texts, n.Text)
		}
	}
	if len(texts) != 1 || !strings.Contains(texts[0], "set_model") {
		t.Errorf("error notices %q", texts)
	}
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }
