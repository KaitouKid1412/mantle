package plugins

import (
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func fixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/09/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeCLI serves the plugin fixtures; mutation results come from result.
func fakeCLI(t *testing.T, result string) string {
	t.Helper()
	return ecotest.FakeClaude(t, map[string]string{
		"--available":        fixture(t, "claudecli/plugin-list-available.json"),
		"marketplace list":   fixture(t, "claudecli/marketplace-list.json"),
		"plugin install":     result,
		"plugin uninstall":   result,
		"plugin enable":      result,
		"plugin disable":     result,
		"plugin update":      result,
		"marketplace add":    result,
		"marketplace remove": result,
		"plugin details":     "Components: 1 skill, 1 agent\nProjected cost: 1.2k tokens\n",
	})
}

func open(t *testing.T, eng *ecotest.Engine, tab int) (*ecotest.Ctx, ext.Dialog, *view) {
	t.Helper()
	ecotest.ResetState(t)
	ctx := ecotest.NewCtx(eng, t.TempDir())
	d, _ := New(ctx, tab)
	ecotest.Open(t, ctx, d)
	return ctx, d, d.Root().(*view)
}

// last returns the newest call that is not a list refresh.
func last(calls [][]string) []string {
	for i := len(calls) - 1; i >= 0; i-- {
		joined := strings.Join(calls[i], " ")
		if strings.HasPrefix(joined, "plugin list") || strings.HasPrefix(joined, "plugin marketplace list") {
			continue
		}
		return calls[i]
	}
	return nil
}

func TestTabsAndListing(t *testing.T) {
	fakeCLI(t, fixture(t, "claudecli/plugin-result-installed.jsonl"))
	ctx, d, _ := open(t, ecotest.NewEngine(), TabDiscover)
	s := ecotest.Screen(ctx, d, 110)
	for _, want := range []string{"[Discover]", "Showing all marketplaces", "✓ demo", "@example-market · 1.2k installs · A demo plugin",
		"remote-tool", "bare"} {
		if !strings.Contains(s, want) {
			t.Errorf("discover lacks %q:\n%s", want, s)
		}
	}
	ecotest.Press(t, ctx, d, "tab")
	s = ecotest.Screen(ctx, d, 110)
	if !strings.Contains(s, "[Installed]") || !strings.Contains(s, "● demo@example-market") || !strings.Contains(s, "v1.2.0 · user · enabled") {
		t.Errorf("installed:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "tab")
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "example-market") || !strings.Contains(s, "github · example/market") {
		t.Errorf("marketplaces:\n%s", s)
	}
	// Enter on a marketplace browses its plugins.
	ecotest.Press(t, ctx, d, "enter")
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "Showing example-market") || strings.Contains(s, "bare") {
		t.Errorf("filtered discover:\n%s", s)
	}
	// ctrl+s cycles marketplaces: example-market → all.
	ecotest.Press(t, ctx, d, "ctrl+s")
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "Showing all marketplaces") {
		t.Errorf("after ctrl+s:\n%s", s)
	}
}

func TestSearch(t *testing.T) {
	fakeCLI(t, "")
	ctx, d, v := open(t, ecotest.NewEngine(), TabDiscover)
	ecotest.Press(t, ctx, d, "/", "g", "i", "t", "space", "s", "u", "b")
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "search: git sub") || !strings.Contains(s, "remote-tool") || strings.Contains(s, "✓ demo") {
		t.Errorf("search:\n%s", s)
	}
	// j and k are typed, not navigation, while searching.
	ecotest.Press(t, ctx, d, "esc")
	if v.filtering || v.lists[TabDiscover].Filter != "" || d.(interface{ Closed() bool }).Closed() {
		t.Errorf("esc clears the search without closing")
	}
	ecotest.Press(t, ctx, d, "/", "j", "k")
	if v.lists[TabDiscover].Filter != "jk" {
		t.Errorf("filter = %q", v.lists[TabDiscover].Filter)
	}
}

func TestInstallAndReload(t *testing.T) {
	log := fakeCLI(t, fixture(t, "claudecli/plugin-result-installed.jsonl"))
	eng := ecotest.NewEngine().Respond(proto.SubReloadPlugins, fixture(t, "control/reload_plugins.json"))
	ctx, d, v := open(t, eng, TabDiscover)
	v.lists[TabDiscover].Select("remote-tool@example-market")
	ecotest.Press(t, ctx, d, "i")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "install", "--json", "--", "remote-tool@example-market"}) {
		t.Fatalf("install argv = %q", got)
	}
	if c := eng.Controls[len(eng.Controls)-1]; c.Subtype != proto.SubReloadPlugins || c.Req != (proto.ReloadPluginsRequest{HoldOnCacheImpact: true}) {
		t.Errorf("reload = %#v", c)
	}
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "Installed remote-tool@example-market") || strings.Contains(s, "restart") {
		t.Errorf("after install:\n%s", s)
	}
	// Installing an installed plugin is refused.
	n := len(ecotest.Calls(t, log))
	v.lists[TabDiscover].Select("demo@example-market")
	ecotest.Press(t, ctx, d, "i")
	if len(ecotest.Calls(t, log)) != n || !strings.Contains(ecotest.Screen(ctx, d, 110), "already installed") {
		t.Errorf("reinstall should be refused")
	}
}

func TestInstallNeedsConfirmation(t *testing.T) {
	log := fakeCLI(t, fixture(t, "claudecli/plugin-result-confirm.jsonl"))
	ctx, d, v := open(t, ecotest.NewEngine(), TabDiscover)
	v.lists[TabDiscover].Select("bare@directory")
	ecotest.Press(t, ctx, d, "enter", "2") // install for this project
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "install", "--json", "--scope=project", "--", "bare@directory"}) {
		t.Fatalf("install argv = %q", got)
	}
	s := ecotest.Screen(ctx, d, 110)
	if !strings.Contains(s, "npx example-installer") || !strings.Contains(s, "Only run it if you trust") {
		t.Fatalf("confirmation:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "y")
	want := []string{"plugin", "install", "--json", "--scope=project", "--accept-command=" + strings.Repeat("a", 64), "--", "bare@directory"}
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, want) {
		t.Errorf("accepted install argv = %q", got)
	}
}

func TestInstalledActions(t *testing.T) {
	log := fakeCLI(t, fixture(t, "claudecli/plugin-result-installed.jsonl"))
	eng := ecotest.NewEngine().Respond(proto.SubReloadPlugins, fixture(t, "control/reload_plugins.json"))
	ctx, d, _ := open(t, eng, TabInstalled)
	ecotest.Press(t, ctx, d, "space")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "disable", "--json", "--scope=user", "--", "demo@example-market"}) {
		t.Fatalf("disable argv = %q", got)
	}
	// Updates apply on restart, so one is offered.
	ecotest.Press(t, ctx, d, "u")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "update", "--json", "--scope=user", "--", "demo@example-market"}) {
		t.Fatalf("update argv = %q", got)
	}
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "needs a restart") {
		t.Fatalf("restart offer:\n%s", s)
	}
	ecotest.Press(t, ctx, d, "y")
	if len(eng.Restarts) != 1 {
		t.Errorf("restarts = %d", len(eng.Restarts))
	}
}

func TestCacheImpactOffersRestart(t *testing.T) {
	fakeCLI(t, fixture(t, "claudecli/plugin-result-installed.jsonl"))
	eng := ecotest.NewEngine().Respond(proto.SubReloadPlugins, fixture(t, "control/reload_plugins_cache_impact.json"))
	ctx, d, _ := open(t, eng, TabInstalled)
	ecotest.Press(t, ctx, d, "space")
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "needs a restart") {
		t.Errorf("cache impact should offer a restart:\n%s", s)
	}
}

func TestUninstallAndFailure(t *testing.T) {
	log := fakeCLI(t, fixture(t, "claudecli/plugin-result-failed.jsonl"))
	ctx, d, _ := open(t, ecotest.NewEngine(), TabInstalled)
	ecotest.Press(t, ctx, d, "x", "y")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "uninstall", "--json", "--scope=user", "--", "demo@example-market"}) {
		t.Fatalf("uninstall argv = %q", got)
	}
	if s := ecotest.Screen(ctx, d, 110); !strings.Contains(s, "was not found in any editable scope") {
		t.Errorf("failure message:\n%s", s)
	}
}

func TestFavoritesPersist(t *testing.T) {
	fakeCLI(t, "")
	ctx, d, v := open(t, ecotest.NewEngine(), TabDiscover)
	v.lists[TabDiscover].Select("bare@directory")
	ecotest.Press(t, ctx, d, "f")
	var favs []string
	if ok, _ := ctx.Store(FeatureID).Get("favorites", &favs); !ok || !reflect.DeepEqual(favs, []string{"bare@directory"}) {
		t.Fatalf("favorites = %v", favs)
	}
	if r, _ := v.lists[TabDiscover].Selected(); r.Key != "bare@directory" || r.Glyph != "★" {
		t.Errorf("favorite row = %+v", r)
	}
	// Favorites sort first.
	if rows := v.lists[TabDiscover].Rows(); rows[0].Key != "bare@directory" {
		t.Errorf("first row = %s", rows[0].Key)
	}
	d2, _ := New(ctx, TabDiscover)
	if !d2.Root().(*view).isFavorite("bare@directory") {
		t.Error("favorites should load from the store")
	}
}

func TestMarketplaces(t *testing.T) {
	log := fakeCLI(t, fixture(t, "claudecli/plugin-result-installed.jsonl"))
	ctx, d, _ := open(t, ecotest.NewEngine(), TabMarketplaces)
	ecotest.Press(t, ctx, d, "a")
	d.HandlePaste(ctx, teaPaste("owner/new-market"))
	ecotest.Press(t, ctx, d, "ctrl+s")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "marketplace", "add", "--json", "--scope=user", "--", "owner/new-market"}) {
		t.Fatalf("add argv = %q", got)
	}
	ecotest.Press(t, ctx, d, "x", "y")
	if got := last(ecotest.Calls(t, log)); !reflect.DeepEqual(got, []string{"plugin", "marketplace", "remove", "--json", "--", "example-market"}) {
		t.Errorf("remove argv = %q", got)
	}
}

func TestPluginErrorsShown(t *testing.T) {
	fakeCLI(t, "")
	ecotest.ResetState(t)
	ecotest.Observe(&proto.SystemInit{PluginErrors: []byte(`[{"plugin":"broken@mk","message":"manifest is invalid"},"plain error"]`)})
	ctx := ecotest.NewCtx(ecotest.NewEngine(), t.TempDir())
	d, _ := New(ctx, TabInstalled)
	ecotest.Open(t, ctx, d)
	s := ecotest.Screen(ctx, d, 110)
	if !strings.Contains(s, "Load error: broken@mk: manifest is invalid") || !strings.Contains(s, "Load error: plain error") {
		t.Errorf("errors:\n%s", s)
	}
}

func TestCommands(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	ctx := ecotest.NewCtx(nil, "/work")
	p, _ := r.Command("plugin")
	m, _ := r.Command("marketplace")
	p.Run(ctx, "")
	m.Run(ctx, "")
	if !reflect.DeepEqual(ctx.DialogArgs, []any{TabDiscover, TabMarketplaces}) || !reflect.DeepEqual(p.Aliases, []string{"plugins"}) {
		t.Errorf("args = %v aliases = %v", ctx.DialogArgs, p.Aliases)
	}
}

func TestStories(t *testing.T) {
	r, _ := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) { testkit.RunStory(t, s) })
	}
}

func teaPaste(s string) tea.PasteMsg { return tea.PasteMsg{Content: s} }
