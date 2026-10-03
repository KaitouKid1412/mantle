package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// cursorTo moves the config panel's cursor to the row whose label contains s.
func cursorTo(t *testing.T, g *rig, s string) {
	t.Helper()
	p := g.dialog.(*configPanel)
	for i, r := range p.visibleRows() {
		if strings.Contains(r.label(), s) {
			p.cursor = i
			return
		}
	}
	t.Fatalf("no row %q", s)
}

func TestConfigToggleCycleAndEngineKeys(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	// ~/.claude.json is read (never written) for global-config keys.
	os.WriteFile(filepath.Join(g.home, ".claude.json"), []byte(`{"copyFullResponse":true}`), 0o644)
	g.command("config", "")
	g.mustContain(100, "[Config]", "Model and thinking", "Verbose output")

	cursorTo(t, g, "Verbose output")
	g.press(ext.ActSelectAccept)
	if jsonOf(g.applied()[patch.User]) != `{"verbose":true}` {
		t.Errorf("verbose %s", jsonOf(g.applied()[patch.User]))
	}
	g.mustContain(160, "Verbose output", "on")

	cursorTo(t, g, "Time format")
	g.press(ext.ActSelectAccept)
	if v := g.applied()[patch.User]["timeFormat"]; v != "12-hour" {
		t.Errorf("timeFormat %v", v)
	}

	cursorTo(t, g, "Tips while working") // local settings
	g.press(ext.ActSelectAccept)
	if jsonOf(g.applied()[patch.Local]) != `{"spinnerTipsEnabled":false}` {
		t.Errorf("local %s", jsonOf(g.applied()[patch.Local]))
	}

	cursorTo(t, g, "/copy copies") // global config: through the engine
	g.mustContain(160, "Saved by Claude Code")
	g.press(ext.ActSelectAccept)
	if !reflect.DeepEqual(g.eng.sent, []string{"/config copyFullResponse=false"}) {
		t.Errorf("engine commands %v", g.eng.sent)
	}
	if _, err := os.Stat(filepath.Join(g.home, ".claude.json")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(g.home, ".claude.json")); string(b) != `{"copyFullResponse":true}` {
		t.Error("~/.claude.json was modified")
	}
}

func TestConfigSearchEditAndPanels(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("config", "")
	g.press(ext.ActSettingsSearch)
	typeText(g, "langu")
	g.mustContain(100, "Search: langu", "Response language")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	g.press(ext.ActSelectAccept) // edit the text value
	typeText(g, "Japanese")
	g.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if v := g.applied()[patch.User]["language"]; v != "Japanese" {
		t.Errorf("language %v", v)
	}
	// Esc clears the search before it closes the panel.
	g.press(ext.ActConfirmNo)
	if g.dialog == nil {
		t.Fatal("closed instead of clearing search")
	}
	cursorTo(t, g, "Model")
	g.press(ext.ActSelectAccept)
	if g.dlgID != dialogModel {
		t.Errorf("model row opened %q", g.dlgID)
	}
}

func TestConfigMantleSectionAndTabs(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	g.command("config", "")
	cursorTo(t, g, settingModelSwitchWarning)
	g.mustContain(160, "mantle", settingModelSwitchWarning)
	g.press(ext.ActSelectAccept)
	if v := g.c.SettingsV.MantleM[settingModelSwitchWarning]; v != true {
		// The spec default is true but unset reads nil, so the first toggle sets true.
		t.Logf("mantle value %v", v)
	}
	if _, ok := g.c.SettingsV.MantleM[settingModelSwitchWarning]; !ok {
		t.Error("mantle setting not written")
	}
	g.press(ext.ActTabsNext)
	g.mustContain(160, "[Status]", "Model and modes")
	g.press(ext.ActTabsNext)
	g.mustContain(100, "[Usage]", "/usage")
	g.press(ext.ActSelectAccept)
	if !strings.Contains(lastNotice(g), "/usage is not available") {
		t.Errorf("usage notice %q", lastNotice(g))
	}
	g.c.CommandList = append(g.c.CommandList, ext.Command{Name: "stats", Run: func(c ext.Ctx, _ string) tea.Cmd {
		return c.Notify(ext.Notice{Text: "stats ran"})
	}})
	g.press(ext.ActTabsNext, ext.ActSelectAccept)
	if lastNotice(g) != "stats ran" || g.dialog != nil {
		t.Errorf("stats tab: %q dialog=%v", lastNotice(g), g.dialog)
	}
}

func TestConfigCommandArguments(t *testing.T) {
	g := newRig(t)
	g.command("config", "verbose=true")
	if !reflect.DeepEqual(g.eng.sent, []string{"/config verbose=true"}) || g.dialog != nil {
		t.Errorf("passthrough %v", g.eng.sent)
	}
	g.command("config", "status")
	if g.dialog == nil || g.dialog.(*configPanel).tab != tabStatus {
		t.Error("/config status did not open the Status tab")
	}
}

func TestThemePicker(t *testing.T) {
	g := newRig(t)
	dir := filepath.Join(g.home, ".claude", "themes")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ocean.json"), []byte(`{"base":"light"}`), 0o644)
	g.c.SettingsV.ClaudeM["theme"] = "dark"
	g.command("theme", "")
	g.mustContain(100, "Theme", "Dark", "Custom: ocean", "~/.claude/themes", "Preview")
	g.press(ext.ActThemeEditCustom) // on a built-in: explains
	if !strings.Contains(lastNotice(g), "custom theme") {
		t.Errorf("edit notice %q", lastNotice(g))
	}
	g.press(ext.ActSelectLast, ext.ActThemeToggleSyntaxHighlighting)
	g.mustContain(100, "syntax highlighting off")
	g.press(ext.ActSelectAccept)
	user := g.applied()[patch.User]
	if user["theme"] != "custom:ocean" || user["syntaxHighlightingDisabled"] != true {
		t.Errorf("theme settings %s", jsonOf(user))
	}
	if g.closed[len(g.closed)-1].Result != "custom:ocean" {
		t.Errorf("result %v", g.closed[len(g.closed)-1].Result)
	}
	// Every move previews on the host; the timer fallback ends the preview after
	// confirm (the fake clock fires at once), and a later settings reload is a no-op.
	pv := g.previews()
	if len(pv) < 3 || pv[0] != "custom:ocean" || pv[len(pv)-1] != "" {
		t.Errorf("previews %q", pv)
	}
	n := len(g.previews())
	g.deliver(ext.SettingsMsg{Changed: []string{"theme"}})
	if len(g.previews()) != n {
		t.Error("preview ended twice")
	}

	// Cancel writes nothing and restores the theme.
	g2 := newRig(t)
	g2.command("theme", "")
	g2.press(ext.ActSelectNext, ext.ActSelectCancel)
	if len(g2.writes) != 0 {
		t.Errorf("cancel wrote %v", g2.writes)
	}
	if pv := g2.previews(); len(pv) != 2 || pv[0] != "light" || pv[1] != "" {
		t.Errorf("cancel previews %q", pv)
	}
}

func TestOutputStylePicker(t *testing.T) {
	g := newRig(t)
	g.eng.responses[proto.SubReloadOutputStyles] = proto.OutputStylesResponse{
		AvailableOutputStyles: []string{"default", "Explanatory", "Learning", "pirate"}}
	g.a.engine.sys = &proto.SystemInit{OutputStyle: "default"}
	g.command("output-style", "")
	g.mustContain(100, "Output style", "pirate", "Custom style", "Teaches as it goes")
	g.press(ext.ActSelectNext, ext.ActSelectAccept)
	want := []string{proto.SubReloadOutputStyles, proto.SubUpdateSettings, proto.SubReloadOutputStyles}
	if !reflect.DeepEqual(g.eng.subtypes(), want) {
		t.Fatalf("controls %v", g.eng.subtypes())
	}
	if p := jsonOf(g.eng.controls[1].Payload); p != `{"settings":{"outputStyle":"Explanatory"},"source":"localSettings"}` {
		t.Errorf("update_settings %s", p)
	}
	g.command("output-style", "Learning")
	if !reflect.DeepEqual(g.eng.sent, []string{"/output-style Learning"}) {
		t.Errorf("passthrough %v", g.eng.sent)
	}
}
