package settings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestTUI(t *testing.T) {
	g := newRig(t)
	g.command("tui", "fullscreen")
	if v := g.applied()[patch.User]["tui"]; v != "fullscreen" || !strings.Contains(lastNotice(g), "still being built") {
		t.Errorf("fullscreen: %v %q", v, lastNotice(g))
	}
	g.command("tui", "")
	g.mustContain(100, "Renderer", "Inline", "Fullscreen")
	g.press(ext.ActSelectFirst, ext.ActSelectAccept)
	if _, ok := g.applied()[patch.User]["tui"]; ok {
		t.Error("inline should unset tui")
	}
	g.command("tui", "sideways")
	if !strings.Contains(lastNotice(g), "/tui default") {
		t.Errorf("usage %q", lastNotice(g))
	}
}

func TestAdvisorAndAutocompact(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("advisor", "opus")
	g.command("autocompact", "400000")
	if !reflect.DeepEqual(g.eng.sent, []string{"/advisor opus", "/autocompact 400000"}) {
		t.Fatalf("passthrough %v", g.eng.sent)
	}
	g.c.SettingsV.ClaudeM["advisorModel"] = "claude-fable-5-1"
	g.command("advisor", "")
	g.mustContain(100, "Advisor", "Off", "Fable", "✔")
	g.press(ext.ActSelectFirst, ext.ActSelectAccept)
	if g.eng.sent[len(g.eng.sent)-1] != "/advisor off" || g.dialog != nil {
		t.Errorf("advisor pick %v", g.eng.sent)
	}
	g.c.SettingsV.ClaudeM["autoCompactWindow"] = 600000.0
	g.command("autocompact", "")
	g.mustContain(100, "Auto-compact window", "600k tokens")
	if p := g.dialog.(*choiceDialog); p.set.Choices[p.cursor].Label != "600k tokens" {
		t.Errorf("cursor on %q", p.set.Choices[p.cursor].Label)
	}
	g.press(ext.ActSelectFirst, ext.ActSelectAccept)
	if g.eng.sent[len(g.eng.sent)-1] != "/autocompact auto" {
		t.Errorf("autocompact pick %v", g.eng.sent)
	}
}

func TestSandbox(t *testing.T) {
	g := newRig(t)
	g.command("sandbox", "")
	g.mustContain(100, "Sandbox", "No sandbox", "✔")
	g.press(ext.ActSelectFirst, ext.ActSelectAccept)
	if jsonOf(g.applied()[patch.Local]) != `{"sandbox":{"autoAllowBashIfSandboxed":true,"enabled":true}}` {
		t.Errorf("auto-allow %s", jsonOf(g.applied()[patch.Local]))
	}
	g.c.SettingsV.ClaudeM["sandbox"] = map[string]any{"enabled": true, "autoAllowBashIfSandboxed": false}
	g.command("sandbox", "")
	if p := g.dialog.(*choiceDialog); p.cursor != 1 {
		t.Errorf("current mode row %d", p.cursor)
	}
	g.press(ext.ActSelectLast, ext.ActSelectAccept)
	sb := g.applied()[patch.Local]["sandbox"].(map[string]any)
	if sb["enabled"] != false || sb["autoAllowBashIfSandboxed"] != true {
		t.Errorf("disable kept other keys %v", sb)
	}
}

func TestRestart(t *testing.T) {
	g := newRig(t)
	restart, _ := g.r.Command("restart")
	var exit *ext.ExitMsg
	for _, m := range runCmd(restart.Run(g.c, "")) {
		if e, ok := m.(ext.ExitMsg); ok {
			exit = &e
		}
	}
	if exit == nil || exit.Code != ext.ExitRestart {
		t.Errorf("restart exit %+v", exit)
	}
	g.deliver(restartFailedMsg{err: errInvalid("no network")})
	if !strings.Contains(lastNotice(g), "did not restart") {
		t.Errorf("update failure notice %q", lastNotice(g))
	}
}
