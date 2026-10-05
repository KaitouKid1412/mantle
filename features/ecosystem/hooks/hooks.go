// Package hooks is the read-only /hooks browser. It lists the engine's merged
// hooks configuration (get_hooks_listing) grouped by event, then matcher, then
// source; without an engine it reads the settings files and plugins itself.
// "Edit" opens the file that declares a hook in $EDITOR.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package hooks

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli/discovery"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.hooks"

// DialogID is the /hooks browser.
const DialogID = "dialog.ecosystem.hooks"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 370, Parity: []string{"EC-12", "EC-45"}, Setup: Setup})
}

// Setup registers /hooks.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "hooks", Source: ext.SourceBuiltin, Description: "Browse hook configurations",
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, nil) },
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.hooks/events", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setListing(ctx, StoryListing(), "")
		return d.View(ctx, a)
	}})
	r.AddStory(ext.Story{ID: "ecosystem.hooks/event", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		v := d.Root().(*view)
		v.setListing(ctx, StoryListing(), "")
		d.Push(ctx, v.eventView("PreToolUse"))
		return d.View(ctx, a)
	}})
	return nil
}

// Hook is one handler as the browser shows it.
type Hook struct {
	Event, Matcher, Source, SourceLabel, Plugin, Type, Text string
	Timeout                                                 int
	Path                                                    string // file that declares it, when known
}

// Event is one hook event with its summary.
type Event struct {
	Name, Summary   string
	SupportsMatcher bool
}

// Listing is what the browser shows.
type Listing struct {
	Events      []Event // the engine's catalog (or the events that have hooks)
	Hooks       []Hook
	AllDisabled bool // disableAllHooks
	ManagedOnly bool // allowManagedHooksOnly
}

// wireListing is get_hooks_listing's reply, including fields pkg/proto doesn't
// model yet (eventCatalog, policy, commandText).
type wireListing struct {
	Events []proto.HookEventInfo `json:"events"`
	Hooks  []struct {
		proto.HookConfigInfo
		CommandText string `json:"commandText"`
	} `json:"hooks"`
	EventCatalog []proto.HookEventInfo `json:"eventCatalog"`
	Policy       struct {
		AllDisabled      bool `json:"allDisabled"`
		DisabledByPolicy bool `json:"disabledByPolicy"`
		ManagedOnly      bool `json:"managedOnly"`
	} `json:"policy"`
}

// FromWire converts the engine's reply.
func FromWire(w wireListing) Listing {
	var l Listing
	cat := w.EventCatalog
	if len(cat) == 0 {
		cat = w.Events
	}
	for _, e := range cat {
		l.Events = append(l.Events, Event{Name: e.Name, Summary: e.Summary, SupportsMatcher: e.SupportsMatcher})
	}
	for _, h := range w.Hooks {
		text := h.DisplayText
		if text == "" {
			text = h.CommandText
		}
		l.Hooks = append(l.Hooks, Hook{Event: h.Event, Matcher: h.Matcher, Source: h.Source, SourceLabel: h.SourceLabel,
			Plugin: h.PluginName, Type: h.Type, Text: text, Timeout: h.Timeout})
	}
	l.AllDisabled = w.Policy.AllDisabled || w.Policy.DisabledByPolicy
	l.ManagedOnly = w.Policy.ManagedOnly
	return l
}

// scopeSource maps discovery scopes to the engine's source names.
var scopeSource = map[discovery.Scope]string{
	discovery.ScopeUser: "userSettings", discovery.ScopeProject: "projectSettings",
	discovery.ScopeLocal: "localSettings", discovery.ScopeManaged: "policySettings", discovery.ScopePlugin: "pluginHook",
}

// FromDiscovery builds a listing from the files (no engine).
func FromDiscovery(info discovery.HooksInfo) Listing {
	l := Listing{AllDisabled: info.Disabled}
	seen := map[string]bool{}
	for _, h := range info.Hooks {
		l.Hooks = append(l.Hooks, Hook{Event: h.Event, Matcher: h.Matcher, Source: scopeSource[h.Scope],
			SourceLabel: sourceTitle(scopeSource[h.Scope]), Plugin: h.Plugin, Type: h.Type, Text: h.Summary(),
			Timeout: int(h.Timeout), Path: h.Path})
		if !seen[h.Event] {
			seen[h.Event] = true
			l.Events = append(l.Events, Event{Name: h.Event})
		}
	}
	return l
}

func sourceTitle(source string) string {
	switch source {
	case "userSettings":
		return "User settings"
	case "projectSettings":
		return "Project settings"
	case "localSettings":
		return "Local settings"
	case "policySettings":
		return "Managed settings"
	case "pluginHook":
		return "Plugin"
	case "sessionHook", "session":
		return "This session"
	}
	return source
}

// New builds the browser.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Hooks", &view{}), nil
}

type view struct {
	eco.Base
	list    eco.List
	listing Listing
	loaded  bool
	showAll bool
	files   []discovery.SettingsFile
	disc    discovery.HooksInfo
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.refresh(ctx, d) }

func (v *view) refresh(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	roots := eco.Roots(ctx)
	v.files = discovery.SettingsFiles(roots)
	v.disc = discovery.Hooks(roots, v.files)
	if cmd, ok := eco.Control(ctx, "", proto.GetHooksListingRequest{}); ok {
		if !v.loaded {
			eco.Busy(ctx, d, "Loading hooks")
		}
		return cmd
	}
	v.setListing(ctx, FromDiscovery(v.disc), "Claude isn't running: showing hooks from settings files.")
	d.SetStatus(ctx, v.statusText(), theme.Inactive)
	return nil
}

func (v *view) statusText() string {
	switch {
	case v.listing.AllDisabled:
		return "All hooks are disabled (disableAllHooks)."
	case v.listing.ManagedOnly:
		return "Only managed hooks run (allowManagedHooksOnly)."
	}
	return ""
}

func (v *view) count(event string) int {
	n := 0
	for _, h := range v.listing.Hooks {
		if h.Event == event {
			n++
		}
	}
	return n
}

func (v *view) setListing(ctx ext.Ctx, l Listing, note string) {
	v.listing, v.loaded = l, true
	// Attach file paths from discovery: same event, matcher, source and text.
	for i := range v.listing.Hooks {
		h := &v.listing.Hooks[i]
		if h.Path == "" {
			h.Path = v.pathFor(*h)
		}
	}
	var rows []eco.Row
	if note != "" {
		rows = append(rows, eco.Row{Key: "note", Label: note, Info: true})
	}
	for _, e := range v.listing.Events {
		n := v.count(e.Name)
		if n == 0 && !v.showAll {
			continue
		}
		detail := fmt.Sprintf("%d hook", n)
		if n != 1 {
			detail += "s"
		}
		if e.Summary != "" {
			detail += " · " + e.Summary
		}
		g, tok := "●", theme.Accent
		if n == 0 {
			g, tok = "○", theme.Subtle
		}
		rows = append(rows, eco.Row{Key: e.Name, Label: e.Name, Detail: detail, Glyph: g, GlyphTok: tok, Value: e.Name})
	}
	v.list.Empty = "No hooks configured. Press a to list every event."
	v.list.MinLabel = 18
	v.list.SetRows(rows)
}

// pathFor finds the file that declares h.
func (v *view) pathFor(h Hook) string {
	for _, dh := range v.disc.Hooks {
		if dh.Event != h.Event || dh.Matcher != h.Matcher || scopeSource[dh.Scope] != h.Source {
			continue
		}
		if h.Source == "pluginHook" && h.Plugin != "" && !strings.HasPrefix(h.Plugin, dh.Plugin) {
			continue
		}
		if dh.Summary() == h.Text || dh.Command == h.Text || strings.HasPrefix(h.Text, dh.Command) {
			return dh.Path
		}
	}
	// Settings scopes have one file each (managed may have several).
	for _, f := range v.files {
		if scopeSource[f.Scope] == h.Source && f.Exists && f.Scope != discovery.ScopeManaged {
			return f.Path
		}
	}
	return ""
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.ControlResultMsg:
		if m.Subtype != proto.SubGetHooksListing {
			return nil
		}
		w, err := eco.Decode[wireListing](m)
		if err != nil {
			v.setListing(ctx, FromDiscovery(v.disc), "Couldn't ask Claude; showing hooks from settings files.")
			eco.Fail(ctx, d, err)
			return nil
		}
		v.setListing(ctx, FromWire(w), "")
		d.SetStatus(ctx, v.statusText(), theme.Warning)
	case eco.ExecDoneMsg:
		if m.Key != "hooks.edit" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		return v.refresh(ctx, d)
	}
	return nil
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a == ext.ActSelectAccept {
		if r, ok := v.list.Selected(); ok {
			d.Push(ctx, v.eventView(r.Value.(string)))
		}
		return true, nil
	}
	return false, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() == "a" {
		v.showAll = !v.showAll
		v.setListing(ctx, v.listing, "")
		return true, nil
	}
	return false, nil
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if !v.loaded {
		return nil
	}
	return v.list.Render(th, width, height)
}

func (v *view) Hints(ctx ext.Ctx) string {
	all := "show all events"
	if v.showAll {
		all = "hide empty events"
	}
	return eco.Hint("enter", "open", "a", all, "esc", "close")
}

// ---- one event ----

func matcherTitle(m string) string {
	if m == "" || m == "*" {
		return "Every match"
	}
	return "Matcher: " + m
}

var sourceOrder = map[string]int{"policySettings": 0, "userSettings": 1, "projectSettings": 2, "localSettings": 3, "pluginHook": 4}

func (v *view) eventView(event string) eco.View {
	var hs []Hook
	for _, h := range v.listing.Hooks {
		if h.Event == event {
			hs = append(hs, h)
		}
	}
	sort.SliceStable(hs, func(i, j int) bool {
		if hs[i].Matcher != hs[j].Matcher {
			return hs[i].Matcher < hs[j].Matcher
		}
		return sourceOrder[hs[i].Source] < sourceOrder[hs[j].Source]
	})
	ev := &eventView{event: event, parent: v}
	rows := make([]eco.Row, len(hs))
	for i, h := range hs {
		label := h.SourceLabel
		if label == "" {
			label = sourceTitle(h.Source)
		}
		if h.Plugin != "" {
			label += " (" + h.Plugin + ")"
		}
		rows[i] = eco.Row{Key: fmt.Sprint(i), Label: label, Detail: h.Type + " · " + h.Text,
			Section: matcherTitle(h.Matcher), Value: h}
	}
	ev.list.Empty = "No hooks for this event."
	ev.list.SetRows(rows)
	return ev
}

type eventView struct {
	eco.Base
	event  string
	parent *view
	list   eco.List
}

func (e *eventView) Subtitle() string { return e.event }

func (e *eventView) selected() (Hook, bool) {
	r, ok := e.list.Selected()
	if !ok {
		return Hook{}, false
	}
	return r.Value.(Hook), true
}

func (e *eventView) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if e.list.HandleAction(a, 5) {
		return true, nil
	}
	if a == ext.ActSelectAccept {
		if h, ok := e.selected(); ok {
			d.Push(ctx, hookDetail(h))
		}
		return true, nil
	}
	return false, nil
}

func (e *eventView) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() != "e" {
		return false, nil
	}
	h, ok := e.selected()
	if !ok {
		return true, nil
	}
	return true, edit(ctx, d, h)
}

func edit(ctx ext.Ctx, d *eco.Dialog, h Hook) tea.Cmd {
	switch {
	case h.Source == "policySettings":
		d.SetStatus(ctx, "Managed hooks are set by your administrator.", theme.Inactive)
		return nil
	case h.Path == "":
		d.SetStatus(ctx, "Couldn't find the file that declares this hook.", theme.Warning)
		return nil
	}
	return eco.Edit("hooks.edit", h.Path)
}

func (e *eventView) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	return e.list.Render(th, width, height)
}

func (e *eventView) Hints(ctx ext.Ctx) string {
	return eco.Hint("enter", "details", "e", "edit file", "esc", "back")
}

// hookDetail shows one hook in full.
func hookDetail(h Hook) eco.View {
	lines := []string{
		"Event: " + h.Event,
		"Matcher: " + strings.TrimPrefix(matcherTitle(h.Matcher), "Matcher: "),
		"Source: " + sourceTitle(h.Source),
	}
	if h.Plugin != "" {
		lines = append(lines, "Plugin: "+h.Plugin)
	}
	lines = append(lines, "Type: "+h.Type, "Runs: "+h.Text)
	if h.Timeout > 0 {
		lines = append(lines, fmt.Sprintf("Timeout: %ds", h.Timeout))
	}
	if h.Path != "" {
		lines = append(lines, "File: "+h.Path)
	}
	tv := &eco.TextView{Lines: lines, Sub: h.Event}
	tv.HintFn = func(ext.Ctx) string { return eco.Hint("e", "edit file", "esc", "back") }
	return &detailView{TextView: tv, hook: h}
}

type detailView struct {
	*eco.TextView
	hook Hook
}

func (v *detailView) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() == "e" {
		return true, edit(ctx, d, v.hook)
	}
	return false, nil
}

// StoryListing is fixed data for the stories.
func StoryListing() Listing {
	return Listing{
		Events: []Event{
			{Name: "PreToolUse", Summary: "Before a tool runs", SupportsMatcher: true},
			{Name: "PostToolUse", Summary: "After a tool runs", SupportsMatcher: true},
			{Name: "SessionStart", Summary: "When a session starts", SupportsMatcher: true},
			{Name: "Stop", Summary: "Before Claude finishes its reply"},
		},
		Hooks: []Hook{
			{Event: "PreToolUse", Matcher: "Bash", Source: "userSettings", SourceLabel: "User settings", Type: "command",
				Text: "~/.claude/hooks/check-bash.sh", Timeout: 10, Path: filepath.Join("~", ".claude", "settings.json")},
			{Event: "PreToolUse", Matcher: "Edit|Write", Source: "projectSettings", SourceLabel: "Project settings",
				Type: "command", Text: "./scripts/fmt.sh --fix"},
			{Event: "PreToolUse", Matcher: "Edit|Write", Source: "pluginHook", SourceLabel: "Plugin hooks",
				Plugin: "demo@example-market", Type: "command", Text: "${CLAUDE_PLUGIN_ROOT}/bin/guard pre-edit"},
			{Event: "SessionStart", Source: "policySettings", SourceLabel: "Managed settings", Type: "http",
				Text: "https://hooks.example.test/start"},
			{Event: "Stop", Source: "userSettings", SourceLabel: "User settings", Type: "prompt", Text: "Check the goal is met"},
		},
	}
}
