// Package mcp is the /mcp panel. Server state comes from the engine's
// mcp_status (or `claude mcp list` when no engine runs). Enable/disable and
// reconnect use mcp_toggle and mcp_reconnect. OAuth runs `claude mcp login`
// with the terminal, then reconnects; add and remove use `claude mcp add` and
// `claude mcp remove`, then reload_plugins and a fresh mcp_status, offering a
// restart when the engine needs one to see the change.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
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
const FeatureID = "ecosystem.mcp"

// DialogID is the /mcp panel.
const DialogID = "dialog.ecosystem.mcp"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 360,
		Parity: []string{"EC-01", "EC-02", "EC-03", "EC-04", "EC-05", "EC-41"}, Setup: Setup})
}

// pending remembers control requests sent by "/mcp reconnect|enable|disable
// <name>" so their results become notices (UI goroutine only).
var pending = map[string]string{}

// Setup registers /mcp.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "mcp", ArgHint: "[enable|disable|reconnect <server>]", Source: ext.SourceBuiltin,
		Description: "Manage MCP servers", Run: runCommand,
		Complete: func(ctx ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, s := range []string{"enable", "disable", "reconnect"} {
				if strings.HasPrefix(s, prefix) {
					out = append(out, ext.Completion{Value: s + " "})
				}
			}
			return out
		},
	})
	r.AddDialog(DialogID, eco.Factory(New))
	ext.Subscribe(r, FeatureID+".results", func(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
		name, ok := pending[m.Subtype]
		if !ok {
			return nil
		}
		delete(pending, m.Subtype)
		if m.Err != nil {
			return ctx.Notify(ext.Notice{Key: "mcp.action", Level: ext.NoticeError, Text: fmt.Sprintf("MCP %s: %v", name, m.Err)})
		}
		return ctx.Notify(ext.Notice{Key: "mcp.action", Level: ext.NoticeSuccess, Text: fmt.Sprintf("MCP %s: done", name)})
	})
	r.AddStory(ext.Story{ID: "ecosystem.mcp/servers", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setServers(ctx, StoryServers(), false)
		return d.View(ctx, a)
	}})
	r.AddStory(ext.Story{ID: "ecosystem.mcp/detail", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		v := d.Root().(*view)
		v.setServers(ctx, StoryServers(), false)
		d.Push(ctx, v.detail(ctx, StoryServers()[0]))
		return d.View(ctx, a)
	}})
	return nil
}

func runCommand(ctx ext.Ctx, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if args == "" {
		return ctx.OpenDialog(DialogID, nil)
	}
	sub, name, _ := strings.Cut(args, " ")
	name = strings.TrimSpace(name)
	usage := func() tea.Cmd {
		return ctx.Notify(ext.Notice{Key: "mcp.usage", Level: ext.NoticeWarning,
			Text: "Usage: /mcp, or /mcp enable|disable|reconnect <server>"})
	}
	if name == "" {
		return usage()
	}
	if eco.State.Engine("").HasCommand("mcp") {
		if cmd, ok := eco.SendText(ctx, "/mcp "+args); ok {
			return cmd
		}
	}
	var req proto.Request
	switch sub {
	case "reconnect":
		req = proto.MCPReconnectRequest{ServerName: name}
	case "enable", "disable":
		req = proto.MCPToggleRequest{ServerName: name, Enabled: sub == "enable"}
	default:
		return usage()
	}
	cmd, ok := eco.Control(ctx, "", req)
	if !ok {
		return ctx.Notify(ext.Notice{Key: "mcp.usage", Level: ext.NoticeWarning, Text: "Claude isn't running."})
	}
	pending[req.ControlSubtype()] = fmt.Sprintf("%s %s", sub, name)
	return cmd
}

// Server is one MCP server as the panel shows it.
type Server struct {
	Name    string
	Status  string // connected | needs-auth | failed | pending | disabled | …
	Scope   string // local | project | user | claudeai | plugin | dynamic | …
	Type    string // stdio | http | sse | claudeai-proxy | …
	Target  string // command line or URL
	Version string
	Tools   []string
	Error   string
}

// FromStatus converts an mcp_status entry.
func FromStatus(s proto.MCPServerStatus) Server {
	out := Server{Name: s.Name, Status: s.Status, Scope: s.Scope, Error: s.Error}
	if out.Scope == "" {
		out.Scope = s.Source
	}
	if s.ServerInfo != nil {
		out.Version = s.ServerInfo.Version
	}
	for _, t := range s.Tools {
		out.Tools = append(out.Tools, t.Name)
	}
	var cfg struct {
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
		URL     string   `json:"url"`
	}
	if json.Unmarshal(s.Config, &cfg) == nil {
		out.Type = cfg.Type
		out.Target = cfg.URL
		if cfg.Command != "" {
			out.Target = strings.TrimSpace(cfg.Command + " " + strings.Join(cfg.Args, " "))
			if out.Type == "" {
				out.Type = "stdio"
			}
		}
	}
	return out
}

// FromCLI converts a `claude mcp list` row.
func FromCLI(s claudecli.MCPServer) Server {
	status := map[claudecli.MCPState]string{
		claudecli.MCPConnected: "connected", claudecli.MCPDegraded: "connected", claudecli.MCPNeedsAuth: "needs-auth",
		claudecli.MCPFailed: "failed", claudecli.MCPPending: "pending", claudecli.MCPRejected: "disabled",
		claudecli.MCPDisabled: "disabled", claudecli.MCPNotConfigured: "failed",
	}[s.State]
	if status == "" {
		status = strings.ToLower(s.Status)
	}
	out := Server{Name: s.Name, Status: status, Target: s.Target, Error: s.Issue}
	if strings.HasPrefix(s.Name, "claude.ai ") {
		out.Scope = "claudeai"
	}
	if strings.HasPrefix(s.Target, "http") {
		out.Type = "http"
	}
	return out
}

// Counts summarises servers for ext.MCPStatusMsg.
func Counts(servers []Server) ext.MCPStatusMsg {
	m := ext.MCPStatusMsg{EngineID: ext.MainEngine, Total: len(servers)}
	for _, s := range servers {
		switch s.Status {
		case "connected":
			m.Connected++
		case "needs-auth":
			m.NeedsAuth++
		case "failed":
			m.Failed++
		}
	}
	return m
}

func statusLook(status string) (glyph, label string, tok theme.Token) {
	switch status {
	case "connected":
		return "✓", "connected", theme.Success
	case "needs-auth":
		return "!", "needs sign-in", theme.Warning
	case "failed":
		return "✗", "failed", theme.Error
	case "pending":
		return "…", "pending", theme.Inactive
	case "disabled":
		return "○", "disabled", theme.Inactive
	}
	return "?", status, theme.Inactive
}

var scopeOrder = []string{"local", "project", "user", "plugin", "managed", "enterprise", "dynamic", "claudeai"}

func scopeTitle(scope string) string {
	switch scope {
	case "local":
		return "Local · this project, only you"
	case "project":
		return "Project · .mcp.json"
	case "user":
		return "User · all projects"
	case "claudeai":
		return "claude.ai connectors"
	case "plugin":
		return "Plugins"
	case "managed", "enterprise":
		return "Managed"
	case "dynamic":
		return "This session"
	case "":
		return "Servers"
	}
	return strings.ToUpper(scope[:1]) + scope[1:]
}

func scopeRank(scope string) int {
	for i, s := range scopeOrder {
		if s == scope {
			return i
		}
	}
	return len(scopeOrder)
}

// cliScope is the --scope value for claude mcp remove ("" = whichever has it).
func cliScope(scope string) string {
	switch scope {
	case "local", "project", "user":
		return scope
	}
	return ""
}

func removable(s Server) string {
	switch s.Scope {
	case "claudeai":
		return "manage claude.ai connectors on claude.ai"
	case "plugin":
		return "comes from a plugin; use /plugin"
	case "managed", "enterprise":
		return "set by your administrator"
	case "dynamic":
		return "added for this session only"
	}
	return ""
}

func usesOAuth(s Server) bool { return s.Type != "stdio" }

// New builds the panel.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "MCP servers", &view{}), nil
}

type view struct {
	eco.Base
	list    eco.List
	servers []Server
	loaded  bool
	fromCLI bool
	// expect is a server whose arrival (add) or departure (remove) the next
	// mcp_status should show; otherwise a restart is offered.
	expect       string
	expectGone   bool
	loginPending string
}

func (v *view) Subtitle() string {
	if !v.loaded {
		return ""
	}
	c := Counts(v.servers)
	return fmt.Sprintf("%d connected of %d", c.Connected, c.Total)
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.refresh(ctx, d) }

func (v *view) refresh(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	if cmd, ok := eco.Control(ctx, "", proto.MCPStatusRequest{}); ok {
		if !v.loaded {
			eco.Busy(ctx, d, "Loading servers")
		}
		return cmd
	}
	eco.Busy(ctx, d, "Checking servers with claude mcp list")
	cli := eco.CLI(ctx)
	return eco.Async("mcp.cli-list", func() (any, error) { return cli.MCPList(eco.Background()) })
}

func (v *view) setServers(ctx ext.Ctx, servers []Server, fromCLI bool) {
	sort.SliceStable(servers, func(i, j int) bool {
		ri, rj := scopeRank(servers[i].Scope), scopeRank(servers[j].Scope)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(servers[i].Name) < strings.ToLower(servers[j].Name)
	})
	v.servers, v.loaded, v.fromCLI = servers, true, fromCLI
	rows := make([]eco.Row, 0, len(servers))
	for _, s := range servers {
		g, label, tok := statusLook(s.Status)
		detail := label
		if n := len(s.Tools); n == 1 {
			detail += " · 1 tool"
		} else if n > 1 {
			detail += fmt.Sprintf(" · %d tools", n)
		}
		if s.Error != "" {
			detail += " · " + firstLine(s.Error)
		}
		rows = append(rows, eco.Row{Key: s.Name, Label: s.Name, Detail: detail, Glyph: g, GlyphTok: tok,
			Section: scopeTitle(s.Scope), Value: s})
	}
	v.list.Empty = "No MCP servers configured. Press n to add one."
	v.list.MinLabel = 18
	v.list.SetRows(rows)
}

func (v *view) find(name string) (Server, bool) {
	for _, s := range v.servers {
		if s.Name == name {
			return s, true
		}
	}
	return Server{}, false
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.ControlResultMsg:
		return v.onControl(ctx, d, m)
	case eco.ResultMsg:
		return v.onResult(ctx, d, m)
	case eco.ExecDoneMsg:
		if m.Key != "mcp.login" {
			return nil
		}
		name := v.loginPending
		v.loginPending = ""
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		eco.Busy(ctx, d, "Reconnecting "+name)
		if cmd, ok := eco.Control(ctx, "", proto.MCPReconnectRequest{ServerName: name}); ok {
			return cmd
		}
		return v.refresh(ctx, d)
	}
	return nil
}

func (v *view) onControl(ctx ext.Ctx, d *eco.Dialog, m ext.ControlResultMsg) tea.Cmd {
	switch m.Subtype {
	case proto.SubMCPStatus:
		resp, err := eco.Decode[proto.MCPStatusResponse](m)
		if err != nil {
			eco.Fail(ctx, d, err)
			return nil
		}
		servers := make([]Server, 0, len(resp.MCPServers))
		for _, s := range resp.MCPServers {
			servers = append(servers, FromStatus(s))
		}
		v.setServers(ctx, servers, false)
		d.SetStatus(ctx, "", "")
		if cmd := v.checkExpected(ctx, d); cmd != nil {
			return tea.Batch(ext.Msg(Counts(servers)), cmd)
		}
		return ext.Msg(Counts(servers))
	case proto.SubMCPToggle, proto.SubMCPReconnect:
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return v.refresh(ctx, d)
		}
		eco.Done(ctx, d, "Done.")
		return v.refresh(ctx, d)
	case proto.SubReloadPlugins:
		if m.Err != nil && v.expect != "" {
			eco.Fail(ctx, d, m.Err)
		}
		return v.refresh(ctx, d)
	}
	return nil
}

// checkExpected offers a restart when an add or remove isn't visible to the
// engine yet.
func (v *view) checkExpected(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	if v.expect == "" {
		return nil
	}
	name, gone := v.expect, v.expectGone
	v.expect, v.expectGone = "", false
	_, present := v.find(name)
	if present != gone {
		eco.Done(ctx, d, "Done.")
		return nil
	}
	what := "connect " + name
	if gone {
		what = "stop " + name
	}
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{fmt.Sprintf("Claude needs a restart to %s.", what), "Restart now? The conversation is kept."},
		YesLabel: "Restart now",
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			return tea.Batch(d.Close(ctx), eco.RestartEngine(ctx, "to load MCP changes"))
		},
	})
	return nil
}

func (v *view) onResult(ctx ext.Ctx, d *eco.Dialog, m eco.ResultMsg) tea.Cmd {
	switch m.Key {
	case "mcp.cli-list":
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		rows, _ := m.Value.([]claudecli.MCPServer)
		servers := make([]Server, 0, len(rows))
		for _, s := range rows {
			servers = append(servers, FromCLI(s))
		}
		v.setServers(ctx, servers, true)
		d.SetStatus(ctx, "Claude isn't running: showing claude mcp list.", theme.Inactive)
		return v.checkExpected(ctx, d)
	case "mcp.logout":
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		name, _ := m.Value.(string)
		if cmd, ok := eco.Control(ctx, "", proto.MCPReconnectRequest{ServerName: name}); ok {
			return cmd
		}
		return v.refresh(ctx, d)
	case "mcp.add", "mcp.remove":
		if m.Err != nil {
			v.expect = ""
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		if cmd, ok := eco.Control(ctx, "", proto.ReloadPluginsRequest{}); ok {
			return cmd
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
		if s, ok := v.selected(); ok {
			d.Push(ctx, v.detail(ctx, s))
		}
		return true, nil
	}
	return false, nil
}

func (v *view) selected() (Server, bool) {
	r, ok := v.list.Selected()
	if !ok {
		return Server{}, false
	}
	s, ok := r.Value.(Server)
	return s, ok
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "n":
		d.Push(ctx, v.addForm())
		return true, nil
	case "ctrl+r":
		return true, v.refresh(ctx, d)
	}
	s, ok := v.selected()
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "space":
		return true, v.toggle(ctx, d, s)
	case "r":
		return true, v.reconnect(ctx, d, s)
	case "a":
		return true, v.login(ctx, d, s)
	case "x":
		return true, v.confirmRemove(ctx, d, s)
	}
	return false, nil
}

func (v *view) needEngine(ctx ext.Ctx, d *eco.Dialog) bool {
	if ctx.Engine("") == nil {
		d.SetStatus(ctx, "Claude isn't running; start a session first.", theme.Warning)
		return false
	}
	return true
}

func (v *view) toggle(ctx ext.Ctx, d *eco.Dialog, s Server) tea.Cmd {
	if !v.needEngine(ctx, d) {
		return nil
	}
	enable := s.Status == "disabled"
	verb := "Disabling"
	if enable {
		verb = "Enabling"
	}
	cmd, ok := eco.Control(ctx, "", proto.MCPToggleRequest{ServerName: s.Name, Enabled: enable})
	if !ok {
		d.SetStatus(ctx, "This Claude version can't toggle servers.", theme.Warning)
		return nil
	}
	eco.Busy(ctx, d, verb+" "+s.Name)
	return cmd
}

func (v *view) reconnect(ctx ext.Ctx, d *eco.Dialog, s Server) tea.Cmd {
	if !v.needEngine(ctx, d) {
		return nil
	}
	cmd, ok := eco.Control(ctx, "", proto.MCPReconnectRequest{ServerName: s.Name})
	if !ok {
		d.SetStatus(ctx, "This Claude version can't reconnect servers.", theme.Warning)
		return nil
	}
	eco.Busy(ctx, d, "Reconnecting "+s.Name)
	return cmd
}

func (v *view) login(ctx ext.Ctx, d *eco.Dialog, s Server) tea.Cmd {
	if !usesOAuth(s) {
		d.SetStatus(ctx, s.Name+" is a local (stdio) server; it doesn't sign in.", theme.Inactive)
		return nil
	}
	cmd, err := eco.CLI(ctx).MCPLoginCmd(s.Name, os.Getenv("SSH_TTY") != "")
	if err != nil {
		return eco.ExecErr("mcp.login", err)
	}
	v.loginPending = s.Name
	return eco.Exec("mcp.login", cmd)
}

func (v *view) logout(ctx ext.Ctx, d *eco.Dialog, s Server) tea.Cmd {
	cli, name := eco.CLI(ctx), s.Name
	eco.Busy(ctx, d, "Clearing sign-in for "+name)
	return eco.Async("mcp.logout", func() (any, error) {
		return name, cli.MCPLogout(eco.Background(), name)
	})
}

func (v *view) confirmRemove(ctx ext.Ctx, d *eco.Dialog, s Server) tea.Cmd {
	if why := removable(s); why != "" {
		d.SetStatus(ctx, s.Name+" can't be removed here: "+why+".", theme.Inactive)
		return nil
	}
	d.Push(ctx, &eco.ConfirmView{
		Question: []string{fmt.Sprintf("Remove %s from %s settings?", s.Name, s.Scope)},
		YesLabel: "Remove", Danger: true,
		OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			cli, name, scope := eco.CLI(ctx), s.Name, cliScope(s.Scope)
			v.expect, v.expectGone = name, true
			eco.Busy(ctx, d, "Removing "+name)
			return eco.Async("mcp.remove", func() (any, error) {
				return nil, cli.MCPRemove(eco.Background(), name, scope)
			})
		},
	})
	return nil
}

// detail is the per-server page: facts and actions.
func (v *view) detail(ctx ext.Ctx, s Server) eco.View {
	_, label, _ := statusLook(s.Status)
	intro := []string{"Status: " + label}
	if s.Scope != "" {
		intro = append(intro, "Scope: "+scopeTitle(s.Scope))
	}
	if s.Type != "" {
		intro = append(intro, "Type: "+s.Type)
	}
	if s.Target != "" {
		intro = append(intro, "Runs: "+s.Target)
	}
	if s.Version != "" {
		intro = append(intro, "Version: "+s.Version)
	}
	if n := len(s.Tools); n > 0 {
		names := s.Tools
		if n > 8 {
			names = append(append([]string(nil), names[:8]...), fmt.Sprintf("… %d more", n-8))
		}
		intro = append(intro, fmt.Sprintf("Tools (%d): %s", n, strings.Join(names, ", ")))
	}
	if s.Error != "" {
		intro = append(intro, "Error: "+s.Error)
	}
	noEngine := ""
	if ctx.Engine("") == nil {
		noEngine = "Claude isn't running"
	}
	toggleLabel := "Disable"
	if s.Status == "disabled" {
		toggleLabel = "Enable"
	}
	oauth := ""
	if !usesOAuth(s) {
		oauth = "local servers don't sign in"
	}
	back := func(f func(ext.Ctx, *eco.Dialog, Server) tea.Cmd) func(ext.Ctx, *eco.Dialog) tea.Cmd {
		return func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return tea.Batch(d.Pop(ctx), f(ctx, d, s)) }
	}
	return eco.NewMenu(s.Name, intro,
		eco.MenuItem{Label: toggleLabel, Disabled: noEngine, Run: back(v.toggle)},
		eco.MenuItem{Label: "Reconnect", Disabled: noEngine, Run: back(v.reconnect)},
		eco.MenuItem{Label: "Sign in", Detail: "claude mcp login", Disabled: oauth, Run: back(v.login)},
		eco.MenuItem{Label: "Clear sign-in", Detail: "claude mcp logout", Disabled: oauth, Run: back(v.logout)},
		eco.MenuItem{Label: "Remove", Disabled: removable(s), Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
			d.Pop(ctx)
			return v.confirmRemove(ctx, d, s)
		}},
	)
}

func (v *view) addForm() *eco.FormView {
	return &eco.FormView{
		Sub:   "Add server",
		Intro: []string{"Adds a server with claude mcp add."},
		Fields: []eco.Field{
			{Label: "Name"},
			{Label: "Transport", Value: "stdio", Options: []string{"stdio", "http", "sse"}},
			{Label: "Command or URL", Help: "stdio: the program to run; http/sse: the server URL"},
			{Label: "Arguments", Optional: true, Help: "stdio only, separated by spaces"},
			{Label: "Scope", Value: "local", Options: []string{"local", "project", "user"},
				Help: "local: this project, only you · project: .mcp.json · user: all projects"},
			{Label: "Environment", Optional: true, Help: "stdio only: KEY=value KEY2=value"},
			{Label: "Headers", Optional: true, Help: "http/sse only: Name: value; Other: value"},
		},
		OnSubmit: func(ctx ext.Ctx, d *eco.Dialog, vals []string) tea.Cmd {
			o := claudecli.MCPAddOptions{Name: vals[0], Transport: vals[1], CommandOrURL: vals[2], Scope: vals[4]}
			if o.Transport == "stdio" {
				o.Args = strings.Fields(vals[3])
				o.Env = strings.Fields(vals[5])
			} else {
				for _, h := range strings.Split(vals[6], ";") {
					if h = strings.TrimSpace(h); h != "" {
						o.Headers = append(o.Headers, h)
					}
				}
			}
			d.Pop(ctx)
			v.expect, v.expectGone = o.Name, false
			eco.Busy(ctx, d, "Adding "+o.Name)
			cli := eco.CLI(ctx)
			return eco.Async("mcp.add", func() (any, error) { return nil, cli.MCPAdd(eco.Background(), o) })
		},
	}
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if !v.loaded {
		return nil
	}
	return v.list.Render(th, width, height)
}

func (v *view) Hints(ctx ext.Ctx) string {
	return eco.Hint("enter", "details", "space", "enable/disable", "r", "reconnect", "a", "sign in",
		"n", "add", "x", "remove", "esc", "close")
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// StoryServers is fixed data for the stories.
func StoryServers() []Server {
	return []Server{
		{Name: "filesystem", Status: "connected", Scope: "local", Type: "stdio",
			Target: "npx -y @example/server-filesystem /tmp/work", Version: "1.4.0",
			Tools: []string{"read_file", "write_file", "list_directory"}},
		{Name: "search", Status: "needs-auth", Scope: "user", Type: "http", Target: "https://search.example.test/mcp"},
		{Name: "broken", Status: "failed", Scope: "project", Type: "stdio", Target: "/usr/local/bin/broken-mcp",
			Error: "spawn /usr/local/bin/broken-mcp ENOENT"},
		{Name: "quiet", Status: "disabled", Scope: "user", Type: "sse", Target: "https://quiet.example.test/sse"},
		{Name: "claude.ai Example Docs", Status: "connected", Scope: "claudeai", Type: "claudeai-proxy",
			Tools: []string{"search_docs"}},
	}
}
