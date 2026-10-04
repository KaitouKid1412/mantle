// Package plugins is the /plugin manager (aliases /plugins, /marketplace):
// Discover, Installed and Marketplaces tabs over `claude plugin … --json`.
// Keys follow Claude Code's Plugin context: space toggles, i installs, f marks
// a favorite, ctrl+s cycles the marketplace filter. After every change the
// panel refreshes its lists and calls reload_plugins; when the reload reports a
// cache impact (or the change needs it, like an update) it offers an engine
// restart. Plugin load errors from system/init are shown on the Installed tab.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package plugins

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.plugins"

// DialogID is the /plugin manager.
const DialogID = "dialog.ecosystem.plugins"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 410, Parity: []string{"EC-06", "EC-07", "EC-08", "EC-41"}, Setup: Setup})
}

// Tab indexes.
const (
	TabDiscover = iota
	TabInstalled
	TabMarketplaces
)

var tabNames = []string{"Discover", "Installed", "Marketplaces"}

// Setup registers /plugin and its dialog.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "plugin", Aliases: []string{"plugins"}, Source: ext.SourceBuiltin,
		Description: "Discover, install and manage plugins",
		Run:         func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, TabDiscover) },
	})
	r.AddCommand(ext.Command{
		Name: "marketplace", Source: ext.SourceBuiltin, Description: "Manage plugin marketplaces",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, TabMarketplaces) },
	})
	r.AddDialog(DialogID, eco.Factory(New))
	for i, name := range tabNames {
		tab := i
		r.AddStory(ext.Story{ID: "ecosystem.plugins/" + strings.ToLower(name), Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			d, _ := New(ctx, tab)
			v := d.Root().(*view)
			v.setData(ctx, StoryInstalled(), StoryAvailable(), StoryMarketplaces())
			return d.View(ctx, a)
		}})
	}
	return nil
}

// New builds the manager; args is the starting tab.
func New(ctx ext.Ctx, args any) (*eco.Dialog, error) {
	tab, _ := args.(int)
	v := &view{tab: tab, market: -1}
	_, _ = ctx.Store(FeatureID).Get("favorites", &v.favorites)
	return eco.NewDialog(DialogID, "Plugins", v), nil
}

type view struct {
	eco.Base
	tab       int
	lists     [3]eco.List
	installed []claudecli.InstalledPlugin
	available []claudecli.AvailablePlugin
	markets   []claudecli.Marketplace
	loaded    bool
	favorites []string
	market    int // index into marketNames(); -1 = all
	filtering bool
	// restartAfter makes the next reload_plugins offer a restart even without a
	// cache impact (plugin updates apply on restart).
	restartAfter bool
}

func (v *view) Subtitle() string {
	var parts []string
	for i, n := range tabNames {
		if i == v.tab {
			parts = append(parts, "["+n+"]")
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "  ")
}

func (v *view) Contexts() []string {
	if v.filtering {
		return []string{ext.ContextPaneField}
	}
	return []string{ext.ContextPlugin, ext.ContextTabs, ext.ContextSelect}
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	eco.Busy(ctx, d, "Loading plugins")
	return v.load(ctx)
}

func (v *view) load(ctx ext.Ctx) tea.Cmd {
	cli := eco.CLI(ctx)
	return tea.Batch(
		eco.Async("plugins.list", func() (any, error) {
			inst, avail, err := cli.PluginListAvailable(eco.Background())
			return listResult{inst, avail}, err
		}),
		eco.Async("plugins.markets", func() (any, error) { return cli.MarketplaceList(eco.Background()) }),
	)
}

type listResult struct {
	installed []claudecli.InstalledPlugin
	available []claudecli.AvailablePlugin
}

func (v *view) setData(ctx ext.Ctx, inst []claudecli.InstalledPlugin, avail []claudecli.AvailablePlugin, mk []claudecli.Marketplace) {
	v.installed, v.available, v.markets, v.loaded = inst, avail, mk, true
	v.rebuild(ctx)
}

func (v *view) isFavorite(id string) bool {
	for _, f := range v.favorites {
		if f == id {
			return true
		}
	}
	return false
}

func (v *view) installedByID() map[string]claudecli.InstalledPlugin {
	m := map[string]claudecli.InstalledPlugin{}
	for _, p := range v.installed {
		m[p.ID] = p
	}
	return m
}

// marketNames lists the marketplaces that offer plugins, sorted.
func (v *view) marketNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range v.available {
		if p.MarketplaceName != "" && !seen[p.MarketplaceName] {
			seen[p.MarketplaceName] = true
			out = append(out, p.MarketplaceName)
		}
	}
	sort.Strings(out)
	return out
}

func (v *view) marketFilter() string {
	names := v.marketNames()
	if v.market < 0 || v.market >= len(names) {
		return ""
	}
	return names[v.market]
}

func (v *view) rebuild(ctx ext.Ctx) {
	inst := v.installedByID()

	// Discover.
	avail := append([]claudecli.AvailablePlugin(nil), v.available...)
	sort.SliceStable(avail, func(i, j int) bool {
		fi, fj := v.isFavorite(avail[i].PluginID), v.isFavorite(avail[j].PluginID)
		if fi != fj {
			return fi
		}
		if avail[i].InstallCount != avail[j].InstallCount {
			return avail[i].InstallCount > avail[j].InstallCount
		}
		return avail[i].PluginID < avail[j].PluginID
	})
	mk := v.marketFilter()
	var rows []eco.Row
	for _, p := range avail {
		if mk != "" && p.MarketplaceName != mk {
			continue
		}
		g, tok := "", theme.Inactive
		switch {
		case inst[p.PluginID].ID != "":
			g, tok = "✓", theme.Success
		case v.isFavorite(p.PluginID):
			g, tok = "★", theme.Warning
		default:
			g = " "
		}
		detail := "@" + p.MarketplaceName
		if p.InstallCount > 0 {
			detail += fmt.Sprintf(" · %s installs", count(p.InstallCount))
		}
		if p.Description != "" {
			detail += " · " + p.Description
		}
		rows = append(rows, eco.Row{Key: p.PluginID, Label: p.Name, Detail: detail, Glyph: g, GlyphTok: tok, Value: p})
	}
	v.lists[TabDiscover].Empty = "No plugins available. Add a marketplace on the Marketplaces tab."
	v.lists[TabDiscover].MinLabel = 20
	v.lists[TabDiscover].SetRows(rows)

	// Installed, with plugin load errors first.
	rows = nil
	for _, e := range pluginErrors() {
		rows = append(rows, eco.Row{Key: "err:" + e, Label: "Load error: " + e, Info: true})
	}
	for _, p := range v.installed {
		g, tok, state := "●", theme.Success, "enabled"
		if !p.Enabled {
			g, tok, state = "○", theme.Inactive, "disabled"
		}
		detail := fmt.Sprintf("%s · %s", p.Scope, state)
		if p.Version != "" {
			detail = "v" + p.Version + " · " + detail
		}
		rows = append(rows, eco.Row{Key: p.ID, Label: p.ID, Detail: detail, Glyph: g, GlyphTok: tok, Value: p})
	}
	v.lists[TabInstalled].Empty = "No plugins installed. Find some on the Discover tab."
	v.lists[TabInstalled].MinLabel = 24
	v.lists[TabInstalled].SetRows(rows)

	// Marketplaces.
	rows = nil
	for _, m := range v.markets {
		src := m.Repo
		if src == "" {
			src = m.URL
		}
		if src == "" {
			src = m.Path
		}
		rows = append(rows, eco.Row{Key: m.Name, Label: m.Name, Detail: m.Source + " · " + src, Value: m})
	}
	v.lists[TabMarketplaces].Empty = "No marketplaces added. Press a to add one."
	v.lists[TabMarketplaces].MinLabel = 22
	v.lists[TabMarketplaces].SetRows(rows)
}

// pluginErrors reads init.plugin_errors (strings or objects with a message).
func pluginErrors() []string {
	in := eco.State.Engine("").Init
	if in == nil || len(in.PluginErrors) == 0 {
		return nil
	}
	var raw []json.RawMessage
	if json.Unmarshal(in.PluginErrors, &raw) != nil {
		return nil
	}
	var out []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
			continue
		}
		var o struct {
			Plugin, Source, Message, Error string
		}
		if json.Unmarshal(r, &o) == nil {
			msg := o.Message
			if msg == "" {
				msg = o.Error
			}
			who := o.Plugin
			if who == "" {
				who = o.Source
			}
			if who != "" {
				msg = who + ": " + msg
			}
			out = append(out, msg)
		}
	}
	return out
}

func count(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func (v *view) list() *eco.List { return &v.lists[v.tab] }

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ResultMsg:
		return v.onResult(ctx, d, m)
	case ext.ControlResultMsg:
		if m.Subtype != proto.SubReloadPlugins {
			return nil
		}
		return v.onReload(ctx, d, m)
	}
	return nil
}

func (v *view) onResult(ctx ext.Ctx, d *eco.Dialog, m eco.ResultMsg) tea.Cmd {
	switch m.Key {
	case "plugins.list":
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		r := m.Value.(listResult)
		if !v.loaded {
			d.SetStatus(ctx, "", "") // keep messages from later changes visible
		}
		v.installed, v.available, v.loaded = r.installed, r.available, true
		v.rebuild(ctx)
	case "plugins.markets":
		if m.Err == nil {
			v.markets, _ = m.Value.([]claudecli.Marketplace)
			v.rebuild(ctx)
		}
	case "plugins.details":
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		d.SetStatus(ctx, "", "")
		text, _ := m.Value.(string)
		d.Push(ctx, &eco.TextView{Lines: strings.Split(strings.TrimRight(text, "\n"), "\n"), Sub: "Details"})
	case "plugins.mutate":
		res, _ := m.Value.(mutation)
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return v.load(ctx)
		}
		if res.result.NeedsConfirmation() {
			d.SetStatus(ctx, "", "")
			return v.confirmCommand(ctx, d, res)
		}
		eco.Done(ctx, d, res.done)
		v.restartAfter = v.restartAfter || res.restart
		cmds := []tea.Cmd{v.load(ctx)}
		if cmd, ok := eco.Control(ctx, "", proto.ReloadPluginsRequest{HoldOnCacheImpact: true}); ok {
			cmds = append(cmds, cmd)
		} else if v.restartAfter {
			v.restartAfter = false
			d.SetStatus(ctx, res.done+" It applies when Claude next starts.", theme.Success)
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (v *view) onReload(ctx ext.Ctx, d *eco.Dialog, m ext.ControlResultMsg) tea.Cmd {
	resp, err := eco.Decode[proto.ReloadPluginsResponse](m)
	restart := v.restartAfter
	v.restartAfter = false
	if err != nil {
		eco.Fail(ctx, d, err)
		return nil
	}
	if resp.Held || (len(resp.CacheImpact) > 0 && string(resp.CacheImpact) != "null") {
		restart = true
	}
	if !restart {
		return nil
	}
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{"Claude needs a restart to apply the plugin change.", "Restart now? The conversation is kept."},
		YesLabel: "Restart now",
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			return tea.Batch(d.Close(ctx), eco.RestartEngine(ctx, "to apply plugin changes"))
		},
	})
	return nil
}

// mutation is the result of one CLI change.
type mutation struct {
	result  claudecli.PluginResult
	done    string // success message
	restart bool
	retry   func(acceptSHA string) tea.Cmd
}

func (v *view) mutate(ctx ext.Ctx, d *eco.Dialog, busy, done string, restart bool, run func(*claudecli.Runner) (claudecli.PluginResult, error), retry func(string) tea.Cmd) tea.Cmd {
	eco.Busy(ctx, d, busy)
	cli := eco.CLI(ctx)
	return eco.Async("plugins.mutate", func() (any, error) {
		res, err := run(cli)
		return mutation{result: res, done: done, restart: restart, retry: retry}, err
	})
}

func (v *view) install(ctx ext.Ctx, d *eco.Dialog, p claudecli.AvailablePlugin, scope, accept string) tea.Cmd {
	id := p.PluginID
	return v.mutate(ctx, d, "Installing "+id, "Installed "+id+".", false,
		func(c *claudecli.Runner) (claudecli.PluginResult, error) {
			return c.PluginInstall(eco.Background(), claudecli.PluginInstallOptions{Plugin: id, Scope: scope, AcceptCommand: accept})
		},
		func(sha string) tea.Cmd { return v.install(ctx, d, p, scope, sha) })
}

// confirmCommand shows a marketplace-declared install command and re-runs
// with --accept-command only if the person agrees.
func (v *view) confirmCommand(ctx ext.Ctx, d *eco.Dialog, m mutation) tea.Cmd {
	sc := m.result.ShownCommand
	q := []string{"This plugin is installed by running a command from its marketplace:", ""}
	q = append(q, "  "+sc.Command, "")
	if m.result.Message != "" {
		q = append(q, m.result.Message, "")
	}
	q = append(q, "Only run it if you trust this marketplace.")
	d.Push(ctx, &eco.ConfirmView{Question: q, YesLabel: "Run it and install", Danger: true,
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return m.retry(sc.SHA256) }})
	return nil
}

func (v *view) toggle(ctx ext.Ctx, d *eco.Dialog, p claudecli.InstalledPlugin) tea.Cmd {
	if p.Enabled {
		return v.mutate(ctx, d, "Disabling "+p.ID, "Disabled "+p.ID+".", false,
			func(c *claudecli.Runner) (claudecli.PluginResult, error) {
				return c.PluginDisable(eco.Background(), p.ID, p.Scope, false)
			}, nil)
	}
	return v.mutate(ctx, d, "Enabling "+p.ID, "Enabled "+p.ID+".", false,
		func(c *claudecli.Runner) (claudecli.PluginResult, error) {
			return c.PluginEnable(eco.Background(), p.ID, p.Scope)
		}, nil)
}

func (v *view) update(ctx ext.Ctx, d *eco.Dialog, p claudecli.InstalledPlugin, accept string) tea.Cmd {
	return v.mutate(ctx, d, "Updating "+p.ID, "Updated "+p.ID+".", true,
		func(c *claudecli.Runner) (claudecli.PluginResult, error) {
			return c.PluginUpdate(eco.Background(), p.ID, p.Scope, accept)
		},
		func(sha string) tea.Cmd { return v.update(ctx, d, p, sha) })
}

func (v *view) uninstall(ctx ext.Ctx, d *eco.Dialog, p claudecli.InstalledPlugin) tea.Cmd {
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{fmt.Sprintf("Uninstall %s (%s scope)?", p.ID, p.Scope), "Its saved data is removed too."},
		YesLabel: "Uninstall", Danger: true,
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			return v.mutate(ctx, d, "Uninstalling "+p.ID, "Uninstalled "+p.ID+".", false,
				func(c *claudecli.Runner) (claudecli.PluginResult, error) {
					return c.PluginUninstall(eco.Background(), p.ID, p.Scope, false, false)
				}, nil)
		},
	})
	return nil
}

func (v *view) toggleFavorite(ctx ext.Ctx, d *eco.Dialog, id string) {
	if v.isFavorite(id) {
		out := v.favorites[:0]
		for _, f := range v.favorites {
			if f != id {
				out = append(out, f)
			}
		}
		v.favorites = out
	} else {
		v.favorites = append(v.favorites, id)
	}
	if err := ctx.Store(FeatureID).Set("favorites", v.favorites); err != nil {
		eco.Fail(ctx, d, err)
	}
	v.rebuild(ctx)
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if !v.loaded || v.filtering {
		return false, nil
	}
	if v.list().HandleAction(a, 10) {
		return true, nil
	}
	switch a {
	case ext.ActTabsNext:
		v.tab = (v.tab + 1) % len(tabNames)
		return true, nil
	case ext.ActTabsPrevious:
		v.tab = (v.tab + len(tabNames) - 1) % len(tabNames)
		return true, nil
	case ext.ActPluginCycleMarketplace:
		names := v.marketNames()
		v.market++
		if v.market >= len(names) {
			v.market = -1
		}
		v.tab = TabDiscover
		v.rebuild(ctx)
		return true, nil
	}
	r, ok := v.list().Selected()
	if !ok {
		return a == ext.ActSelectAccept, nil
	}
	switch p := r.Value.(type) {
	case claudecli.AvailablePlugin:
		switch a {
		case ext.ActSelectAccept:
			d.Push(ctx, v.availableDetail(ctx, p))
			return true, nil
		case ext.ActPluginInstall:
			if v.installedByID()[p.PluginID].ID != "" {
				d.SetStatus(ctx, p.PluginID+" is already installed.", theme.Inactive)
				return true, nil
			}
			return true, v.install(ctx, d, p, "", "")
		case ext.ActPluginFavorite:
			v.toggleFavorite(ctx, d, p.PluginID)
			return true, nil
		case ext.ActPluginToggle:
			if ip := v.installedByID()[p.PluginID]; ip.ID != "" {
				return true, v.toggle(ctx, d, ip)
			}
			return true, v.install(ctx, d, p, "", "")
		}
	case claudecli.InstalledPlugin:
		switch a {
		case ext.ActSelectAccept:
			d.Push(ctx, v.installedDetail(ctx, p))
			return true, nil
		case ext.ActPluginToggle:
			return true, v.toggle(ctx, d, p)
		case ext.ActPluginFavorite:
			v.toggleFavorite(ctx, d, p.ID)
			return true, nil
		}
	case claudecli.Marketplace:
		if a == ext.ActSelectAccept {
			for i, n := range v.marketNames() {
				if n == p.Name {
					v.market, v.tab = i, TabDiscover
					v.rebuild(ctx)
				}
			}
			return true, nil
		}
	}
	return false, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if v.filtering {
		l := &v.lists[TabDiscover]
		switch k.String() {
		case "esc", "escape":
			v.filtering = false
			l.SetFilter("")
		case "enter":
			v.filtering = false
		case "backspace":
			if f := []rune(l.Filter); len(f) > 0 {
				l.SetFilter(string(f[:len(f)-1]))
			}
		case "up":
			l.Move(-1)
		case "down":
			l.Move(1)
		default:
			if k.Text != "" {
				l.SetFilter(l.Filter + k.Text)
			}
		}
		return true, nil
	}
	if !v.loaded {
		return false, nil
	}
	if k.String() == "/" && v.tab == TabDiscover {
		v.filtering = true
		return true, nil
	}
	r, ok := v.list().Selected()
	switch v.tab {
	case TabInstalled:
		if !ok {
			return false, nil
		}
		p := r.Value.(claudecli.InstalledPlugin)
		switch k.String() {
		case "u":
			return true, v.update(ctx, d, p, "")
		case "x":
			return true, v.uninstall(ctx, d, p)
		}
	case TabMarketplaces:
		switch k.String() {
		case "a":
			d.Push(ctx, v.addMarketForm())
			return true, nil
		}
		if !ok {
			return false, nil
		}
		m := r.Value.(claudecli.Marketplace)
		switch k.String() {
		case "u":
			return true, v.mutate(ctx, d, "Updating "+m.Name, "Updated "+m.Name+".", false,
				func(c *claudecli.Runner) (claudecli.PluginResult, error) {
					return c.MarketplaceUpdate(eco.Background(), m.Name)
				}, nil)
		case "x":
			d.Push(ctx, &eco.ConfirmView{Question: []string{"Remove the " + m.Name + " marketplace?",
				"Plugins installed from it stay installed."}, YesLabel: "Remove", Danger: true,
				OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
					return v.mutate(ctx, d, "Removing "+m.Name, "Removed "+m.Name+".", false,
						func(c *claudecli.Runner) (claudecli.PluginResult, error) {
							return c.MarketplaceRemove(eco.Background(), m.Name, "")
						}, nil)
				}})
			return true, nil
		}
	}
	return false, nil
}

func (v *view) addMarketForm() eco.View {
	return &eco.FormView{Sub: "Add marketplace",
		Intro: []string{"A GitHub repo (owner/repo), a git URL, or a local path."},
		Fields: []eco.Field{
			{Label: "Source"},
			{Label: "Scope", Value: "user", Options: []string{"user", "project", "local"}},
		},
		OnSubmit: func(ctx ext.Ctx, d *eco.Dialog, vals []string) tea.Cmd {
			d.Pop(ctx)
			src, scope := vals[0], vals[1]
			return v.mutate(ctx, d, "Adding "+src, "Added "+src+".", false,
				func(c *claudecli.Runner) (claudecli.PluginResult, error) {
					return c.MarketplaceAdd(eco.Background(), src, scope, false, nil)
				}, nil)
		}}
}

func (v *view) details(ctx ext.Ctx, d *eco.Dialog, id string) tea.Cmd {
	eco.Busy(ctx, d, "Loading details")
	cli := eco.CLI(ctx)
	return eco.Async("plugins.details", func() (any, error) { return cli.PluginDetails(eco.Background(), id) })
}

func (v *view) availableDetail(ctx ext.Ctx, p claudecli.AvailablePlugin) eco.View {
	intro := []string{p.PluginID}
	if p.Description != "" {
		intro = append(intro, "", p.Description)
	}
	intro = append(intro, "")
	if p.Version != "" {
		intro = append(intro, "Version: "+p.Version)
	}
	if p.InstallCount > 0 {
		intro = append(intro, "Installs: "+count(p.InstallCount))
	}
	src := p.Source.Kind
	if p.Source.URL != "" {
		src += " " + p.Source.URL
	} else if p.Source.Path != "" {
		src += " " + p.Source.Path
	}
	intro = append(intro, "Source: "+strings.TrimSpace(src))
	installed := ""
	if v.installedByID()[p.PluginID].ID != "" {
		installed = "already installed"
	}
	inst := func(scope string) func(ext.Ctx, *eco.Dialog) tea.Cmd {
		return func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			d.Pop(ctx)
			return v.install(ctx, d, p, scope, "")
		}
	}
	return eco.NewMenu(p.Name, intro,
		eco.MenuItem{Label: "Install for you", Detail: "user scope, every project", Disabled: installed, Run: inst("user")},
		eco.MenuItem{Label: "Install for this project", Detail: "project scope, shared through the repo", Disabled: installed, Run: inst("project")},
		eco.MenuItem{Label: "Install for you in this project", Detail: "local scope", Disabled: installed, Run: inst("local")},
		eco.MenuItem{Label: "Show components and token cost", Detail: "claude plugin details",
			Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.details(ctx, d, p.PluginID) }},
	)
}

func (v *view) installedDetail(ctx ext.Ctx, p claudecli.InstalledPlugin) eco.View {
	intro := []string{p.ID, "", "Version: " + p.Version, "Scope: " + p.Scope, "Path: " + p.InstallPath}
	toggle := "Disable"
	if !p.Enabled {
		toggle = "Enable"
	}
	back := func(f func(ext.Ctx, *eco.Dialog) tea.Cmd) func(ext.Ctx, *eco.Dialog) tea.Cmd {
		return func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { d.Pop(ctx); return f(ctx, d) }
	}
	return eco.NewMenu(p.Name(), intro,
		eco.MenuItem{Label: toggle, Run: back(func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.toggle(ctx, d, p) })},
		eco.MenuItem{Label: "Update", Detail: "applies after a restart", Run: back(func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.update(ctx, d, p, "") })},
		eco.MenuItem{Label: "Show components and token cost", Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.details(ctx, d, p.ID) }},
		eco.MenuItem{Label: "Uninstall", Run: back(func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.uninstall(ctx, d, p) })},
	)
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if !v.loaded {
		return nil
	}
	var head []string
	if v.tab == TabDiscover {
		filter := "all marketplaces"
		if mk := v.marketFilter(); mk != "" {
			filter = mk
		}
		line := th.Paint(theme.Inactive, "Showing "+filter)
		if v.filtering || v.lists[TabDiscover].Filter != "" {
			line += th.Paint(theme.Inactive, " · search: ") + th.Paint(theme.Text, v.lists[TabDiscover].Filter)
			if v.filtering {
				line += th.Paint(theme.Suggestion, "▏")
			}
		}
		head = append(head, line, "")
	}
	h := height
	if h > 0 {
		h -= len(head)
	}
	return append(head, v.list().Render(th, width, h)...)
}

func (v *view) Hints(ctx ext.Ctx) string {
	if v.filtering {
		return eco.Hint("type", "to search", "enter", "done", "esc", "clear")
	}
	tabs := eco.KeyName(ctx, ext.ContextTabs, ext.ActTabsNext, "tab")
	switch v.tab {
	case TabDiscover:
		return eco.Hint(tabs, "switch tab", "/", "search", "i", "install", "f", "favorite",
			eco.KeyName(ctx, ext.ContextPlugin, ext.ActPluginCycleMarketplace, "ctrl+s"), "marketplace", "enter", "details", "esc", "close")
	case TabInstalled:
		return eco.Hint(tabs, "switch tab", "space", "enable/disable", "u", "update", "x", "uninstall", "enter", "details", "esc", "close")
	}
	return eco.Hint(tabs, "switch tab", "a", "add", "u", "update", "x", "remove", "enter", "browse", "esc", "close")
}

// Story data.

// StoryInstalled is fixed data for the stories.
func StoryInstalled() []claudecli.InstalledPlugin {
	return []claudecli.InstalledPlugin{
		{ID: "demo@example-market", Version: "1.2.0", Scope: "user", Enabled: true, InstallPath: "~/.claude/plugins/cache/example-market/demo/1.2.0"},
		{ID: "linter@other-market", Version: "0.3.1", Scope: "project", Enabled: false},
	}
}

// StoryAvailable is fixed data for the stories.
func StoryAvailable() []claudecli.AvailablePlugin {
	return []claudecli.AvailablePlugin{
		{PluginID: "demo@example-market", Name: "demo", MarketplaceName: "example-market", Description: "A demo plugin", InstallCount: 1200},
		{PluginID: "formatter@example-market", Name: "formatter", MarketplaceName: "example-market", Description: "Formats code on save", InstallCount: 54000},
		{PluginID: "linter@other-market", Name: "linter", MarketplaceName: "other-market", Description: "Lints as you go", InstallCount: 300},
		{PluginID: "notes@other-market", Name: "notes", MarketplaceName: "other-market", Description: "Keeps project notes"},
	}
}

// StoryMarketplaces is fixed data for the stories.
func StoryMarketplaces() []claudecli.Marketplace {
	return []claudecli.Marketplace{
		{Name: "example-market", Source: "github", Repo: "example/market"},
		{Name: "other-market", Source: "directory", Path: "~/src/market"},
	}
}
