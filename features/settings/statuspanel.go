package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/settingsfile"
	"github.com/KaitouKid1412/mantle/features/settings/status"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogStatus = "dialog.status"
	statusID     = "settings.status.panel"
)

// mantleVersion is set by the build (-ldflags -X); otherwise read from build info.
var mantleVersion = ""

func versionString() string {
	if mantleVersion != "" {
		return mantleVersion
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			if v == "" || v == "(devel)" {
				return "dev-" + s.Value[:7]
			}
		}
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}

func (a *area) setupStatus(r ext.Registrar) error {
	r.AddDialog(dialogStatus, a.newStatusPanel)
	r.AddCommand(ext.Command{
		Name: "status", Description: "Show versions, account, model, MCP servers and settings files",
		Source: ext.SourceBuiltin,
		Run:    func(c ext.Ctx, _ string) tea.Cmd { return c.OpenDialog(dialogStatus, nil) },
	})
	r.AddCommand(ext.Command{
		Name: "version", Description: "Print the mantle and Claude Code versions", Source: ext.SourceBuiltin,
		Run: func(c ext.Ctx, _ string) tea.Cmd {
			eng := c.Session().ClaudeVersion
			if a.engine.sys != nil && a.engine.sys.ClaudeCodeVersion != "" {
				eng = a.engine.sys.ClaudeCodeVersion
			}
			if eng == "" {
				eng = "unknown"
			}
			return c.Print(fmt.Sprintf("mantle %s · Claude Code %s", versionString(), eng))
		},
	})
	r.AddStory(ext.Story{ID: "settings.status/panel", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		a := storyArea()
		a.engine.sys = &proto.SystemInit{Model: "opus", ClaudeCodeVersion: "2.1.288", CWD: "/work/repo",
			PermissionMode: "acceptEdits", Tools: []string{"Bash", "Read", "Edit", "Write"}}
		a.engine.init = &proto.InitializeResponse{Account: proto.Account{Email: "user@example.com",
			SubscriptionType: "max", APIProvider: "firstParty", TokenSource: "claude.ai"}}
		p := &statusPanel{a: a, loaded: true, version: "0.1.0",
			files: []status.SettingsFile{{Scope: "User", Path: "~/.claude/settings.json", Exists: true},
				{Scope: "Project", Path: "/work/repo/.claude/settings.json", Exists: true, Err: "unexpected end of JSON input"}},
			memory: []status.MemoryFile{{Path: "~/.claude/CLAUDE.md", Scope: "user"}},
			mcp:    []status.MCPServer{{Name: "github", Status: "connected"}, {Name: "linear", Status: "needs-auth"}}}
		return p.View(c, ar)
	}})
	return nil
}

// statusLoadedMsg carries the file checks done off the UI goroutine.
type statusLoadedMsg struct {
	files  []status.SettingsFile
	memory []status.MemoryFile
	docs   map[patch.Scope]map[string]any
}

type statusPanel struct {
	a       *area
	loaded  bool
	version string
	files   []status.SettingsFile
	memory  []status.MemoryFile
	mcp     []status.MCPServer
	offset  int
}

func (a *area) newStatusPanel(ext.Ctx, any) (ext.Dialog, error) {
	return &statusPanel{a: a, version: versionString()}, nil
}

func (p *statusPanel) ID() string                                         { return statusID }
func (p *statusPanel) Placement() ext.Placement                           { return ext.PlaceInline }
func (p *statusPanel) KeyContext() string                                 { return ext.ContextSettings }
func (p *statusPanel) KeyContexts() []string                              { return []string{ext.ContextSettings, ext.ContextSelect} }
func (p *statusPanel) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (p *statusPanel) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }

func (p *statusPanel) Init(c ext.Ctx) tea.Cmd {
	env := p.a.env(c)
	return tea.Batch(func() tea.Msg {
		return ext.AddressedMsg{To: statusID, Msg: statusLoad(env)}
	}, p.refreshMCP(c))
}

func (p *statusPanel) refreshMCP(c ext.Ctx) tea.Cmd {
	if eng := c.Engine(""); eng != nil && eng.Supports(proto.SubMCPStatus) {
		return eng.Control(proto.SubMCPStatus, proto.MCPStatusRequest{})
	}
	return nil
}

// statusLoad checks the settings and memory files.
func statusLoad(env patch.Env) statusLoadedMsg {
	var m statusLoadedMsg
	srcs := settingsfile.ReadAll(env)
	m.docs = settingsfile.Docs(srcs)
	for _, s := range srcs {
		f := status.SettingsFile{Scope: s.Scope.Label(), Path: s.Path, Exists: s.Exists}
		if s.Err != nil {
			f.Err = s.Err.Error()
		}
		if !s.Exists && s.Scope == patch.Policy {
			continue // most machines have no managed settings
		}
		m.files = append(m.files, f)
	}
	for _, mf := range memoryCandidates(env) {
		if _, err := os.Stat(mf.Path); err == nil {
			m.memory = append(m.memory, mf)
		}
	}
	return m
}

func memoryCandidates(env patch.Env) []status.MemoryFile {
	out := []status.MemoryFile{{Path: filepath.Join(env.ClaudeDir(), "CLAUDE.md"), Scope: "user"}}
	if env.ProjectRoot != "" {
		out = append(out,
			status.MemoryFile{Path: filepath.Join(env.ProjectRoot, "CLAUDE.md"), Scope: "project"},
			status.MemoryFile{Path: filepath.Join(env.ProjectRoot, ".claude", "CLAUDE.md"), Scope: "project"},
			status.MemoryFile{Path: filepath.Join(env.ProjectRoot, "CLAUDE.local.md"), Scope: "local"})
	}
	return out
}

func (p *statusPanel) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.AddressedMsg:
		if m.To == p.ID() {
			return p.Update(c, m.Msg)
		}
	case statusLoadedMsg:
		p.files, p.memory, p.loaded = m.files, m.memory, true
		c.Invalidate(p.ID())
	case ext.ControlResultMsg:
		if m.Subtype == proto.SubMCPStatus && isMain(m.EngineID) && m.Err == nil {
			var r proto.MCPStatusResponse
			if json.Unmarshal(m.Resp, &r) == nil {
				p.mcp = status.MCPFromStatus(r)
				c.Invalidate(p.ID())
			}
		}
	}
	return nil
}

func (p *statusPanel) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActSelectNext:
		p.offset++
	case ext.ActSelectPrevious:
		p.offset--
	case ext.ActSelectPageDown, ext.ActScrollHalfPageDown:
		p.offset += 10
	case ext.ActSelectPageUp, ext.ActScrollHalfPageUp:
		p.offset -= 10
	case ext.ActSelectFirst:
		p.offset = 0
	case ext.ActSelectLast:
		p.offset = 1 << 20
	case ext.ActSettingsRetry:
		return true, p.refreshMCP(c)
	case ext.ActSelectCancel, ext.ActConfirmNo, ext.ActSelectAccept:
		return true, c.CloseDialog(dialogStatus)
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

// inputs gathers everything /status shows.
func (p *statusPanel) inputs(c ext.Ctx) status.Inputs {
	s := c.Session()
	env := p.a.env(c)
	in := status.Inputs{
		MantleVersion: p.version, EngineVersion: s.ClaudeVersion, Model: s.Model,
		SessionID: s.SessionID, SessionName: s.Title, Cwd: s.Cwd,
		PermissionMode: s.PermissionMode, OutputStyle: s.OutputStyle,
		SettingsFiles: p.files, Memory: p.memory, Home: env.Home,
		Settings: settingsDoc(c, "sandbox"),
	}
	in.FromEngine(p.a.engine.init, p.a.engine.sys)
	if len(p.mcp) > 0 {
		in.MCP = p.mcp
	}
	r := p.a.currentRow(c)
	if r.Value != "" {
		in.ModelDisplay = r.Label
		in.ResolvedModel = r.Info.ResolvedModel
	}
	if perms, ok := settingsDoc(c, "permissions")["permissions"].(map[string]any); ok {
		if dirs, ok := perms["additionalDirectories"].([]any); ok {
			for _, d := range dirs {
				if s, ok := d.(string); ok {
					in.AdditionalDirs = append(in.AdditionalDirs, s)
				}
			}
		}
	}
	return in
}

func (p *statusPanel) lines(c ext.Ctx, t *theme.Theme, width int) []string {
	var out []string
	labelW := 16
	for _, sec := range status.Build(p.inputs(c)).Sections() {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lipgloss.NewStyle().Bold(true).Render(sec.Title))
		for _, l := range sec.Lines {
			label := fit(l.Label, labelW)
			label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
			v := l.Value
			if l.Problem {
				v = t.Paint(theme.Warning, v)
			}
			out = append(out, "  "+t.Paint(theme.Inactive, label)+"  "+fit(v, width-labelW-4))
		}
	}
	if !p.loaded {
		out = append(out, "", t.Paint(theme.Inactive, "Checking settings files…"))
	}
	return out
}

func (p *statusPanel) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	lines := p.lines(c, t, ar.Width-4)
	rows := listHeight(ar.MaxHeight, termH, 6)
	if p.offset > len(lines)-rows {
		p.offset = len(lines) - rows
	}
	if p.offset < 0 {
		p.offset = 0
	}
	end := min(p.offset+rows, len(lines))
	body := lines[p.offset:end]
	if more := len(lines) - end; more > 0 {
		body = append(body, t.Paint(theme.Inactive, fmt.Sprintf("↓ %d more", more)))
	}
	return ext.Rendered{Text: frame(t, ar.Width, "Status", "", body, hintLine(
		"↑/↓", "scroll", keyName(c.KeysFor(ext.ContextSettings, ext.ActSettingsRetry), "r"), "refresh MCP status",
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "close"))}
}

var _ ext.ActionHandler = (*statusPanel)(nil)
