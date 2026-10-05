package chrome

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// WelcomeID names the welcome feature.
const WelcomeID = "chrome.welcome"

// welcome prints mantle's banner into scrollback and raises the startup notices
// (company announcements, invalid settings files, MCP servers that need sign-in).
//
// Like Claude Code it prints the banner at startup, as soon as the engine answers
// initialize (before any turn, so before the session id is known), and again for each
// later session (/clear, a resume from the picker).
type welcome struct {
	s        sessionState
	account  proto.Account
	started  bool   // the startup banner is out
	printed  string // session ID the last banner was for ("" = the startup one, before an id)
	onScreen bool   // the last banner hasn't been cleared away (Reprint) since
	mcpShown bool
	version  string
	home     string
}

func newWelcome() *welcome {
	home, _ := os.UserHomeDir()
	return &welcome{s: newSessionState(), version: mantleVersion(), home: home}
}

func (w *welcome) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	w.s.observe(msg)
	switch m := msg.(type) {
	case ext.ControlResultMsg:
		if isMain(m.EngineID) && m.Subtype == proto.SubInitialize && m.Err == nil {
			var r proto.InitializeResponse
			if json.Unmarshal(m.Resp, &r) == nil {
				w.account = r.Account
			}
			if !w.started {
				w.observeStartup(ctx)
				return w.print(ctx, w.s.SessionID)
			}
		}
	case ext.SessionChangedMsg:
		if !isMain(m.EngineID) || w.s.SessionID == "" || w.s.SessionID == w.printed {
			return nil
		}
		if w.started && w.printed == "" && w.onScreen {
			// The first session id after the startup banner, which is still on screen:
			// that banner was this session's. (A /resume before the first turn clears
			// the screen first, and the picked session gets its own banner.)
			w.printed = w.s.SessionID
			return nil
		}
		return w.print(ctx, w.s.SessionID)
	case ext.ScreenClearedMsg:
		w.onScreen = false
	case ext.EngineEventMsg:
		if init, ok := m.Event.(*proto.SystemInit); ok && isMain(m.EngineID) && !w.mcpShown {
			w.mcpShown = true
			return mcpNotice(ctx, init.MCPServers)
		}
	}
	return nil
}

// observeStartup fills in what the host knew at launch (working directory, a --model
// or resumed session) before any engine event.
func (w *welcome) observeStartup(ctx ext.Ctx) {
	info := ctx.Session()
	set(&w.s.Cwd, info.Cwd)
	set(&w.s.Model, info.Model)
	set(&w.s.SessionID, info.SessionID)
	if w.s.Model == "" {
		w.s.Model = ext.ClaudeString(ctx.Settings(), "model", "")
	}
}

// print prints the banner for sessionID ("" before the engine has reported one); the
// first banner also raises the startup notices.
func (w *welcome) print(ctx ext.Ctx, sessionID string) tea.Cmd {
	first := !w.started
	w.started, w.printed, w.onScreen = true, sessionID, true
	cmds := []tea.Cmd{ctx.Print(w.banner(ctx, max(20, termWidth(ctx))))}
	if first {
		seed := sessionID
		if seed == "" {
			seed = ctx.Clock().Now().String() // any per-launch value: it only picks an announcement
		}
		cmds = append(cmds, startupNotices(ctx, seed)...)
	}
	return tea.Batch(cmds...)
}

func termWidth(ctx ext.Ctx) int {
	w, _ := ctx.Size()
	return w
}

// banner is mantle's startup header: names and versions, model and account, the
// working directory, and where to start.
func (w *welcome) banner(ctx ext.Ctx, cols int) string {
	t, sr := ctx.Theme(), ctx.Accessibility().ScreenReader
	head := "mantle " + w.version
	if w.s.Version != "" {
		head += " · Claude Code " + w.s.Version
	}
	var who []string
	if w.s.Model != "" {
		who = append(who, modelDisplayName(w.s.Model))
	}
	if label := subscriptionLabel(w.account.SubscriptionType); label != "" {
		who = append(who, label)
	}
	if w.account.Organization != "" {
		who = append(who, w.account.Organization)
	}
	lines := []string{paint(t, sr, theme.Accent, "▟▙ ") + paint(t, sr, theme.Text, head)}
	if len(who) > 0 {
		lines = append(lines, "   "+dim(t, sr, strings.Join(who, " · ")))
	}
	if w.s.Cwd != "" {
		lines = append(lines, "   "+dim(t, sr, abbreviateHome(w.s.Cwd, w.home)))
	}
	lines = append(lines, "   "+dim(t, sr, "/help for commands · /mantle to change mantle itself"))
	if sr {
		lines[0] = head
	}
	return strings.Join(truncateLines(lines, cols, 0), "\n")
}

func subscriptionLabel(s string) string {
	switch strings.ToLower(s) {
	case "":
		return ""
	case "max":
		return "Claude Max"
	case "pro":
		return "Claude Pro"
	case "team":
		return "Claude Team"
	case "enterprise":
		return "Claude Enterprise"
	}
	return s
}

func abbreviateHome(p, home string) string {
	if home != "" && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// startupNotices raises the once-per-start notices.
func startupNotices(ctx ext.Ctx, sessionID string) []tea.Cmd {
	var cmds []tea.Cmd
	if a := pickAnnouncement(ctx.Settings(), sessionID); a != "" {
		cmds = append(cmds, ctx.Notify(ext.Notice{Key: "chrome:announcement", Text: a,
			Level: ext.NoticeInfo, Timeout: -1, Source: WelcomeID}))
	}
	if ss, ok := ctx.Settings().(ext.ScopedSettings); ok {
		for _, src := range ss.ClaudeSources() {
			if src.Err == nil || src.Path == "" {
				continue
			}
			cmds = append(cmds, ctx.Notify(ext.Notice{
				Key:   "chrome:settings:" + src.Path,
				Text:  fmt.Sprintf("Ignored invalid settings file %s: %v", src.Path, src.Err),
				Level: ext.NoticeWarning, Timeout: -1, Source: WelcomeID,
			}))
		}
	}
	return cmds
}

// pickAnnouncement chooses one companyAnnouncements entry, stable for a session.
func pickAnnouncement(s ext.Settings, sessionID string) string {
	v, ok := s.Claude("companyAnnouncements")
	if !ok {
		return ""
	}
	list, _ := v.([]any)
	var texts []string
	for _, it := range list {
		switch a := it.(type) {
		case string:
			texts = append(texts, a)
		case map[string]any: // tolerate {text: …} objects
			if s, ok := a["text"].(string); ok {
				texts = append(texts, s)
			}
		}
	}
	if len(texts) == 0 {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(sessionID))
	return strings.TrimSpace(texts[int(h.Sum32())%len(texts)])
}

// mcpNotice reports MCP servers that need sign-in.
func mcpNotice(ctx ext.Ctx, servers []proto.MCPServerInfo) tea.Cmd {
	var names []string
	for _, s := range servers {
		if s.Status == "needs-auth" {
			names = append(names, s.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	text := fmt.Sprintf("MCP server %s needs sign-in · /mcp", names[0])
	if len(names) > 1 {
		text = fmt.Sprintf("%d MCP servers need sign-in · /mcp", len(names))
	}
	return ctx.Notify(ext.Notice{Key: "chrome:mcp-auth", Text: text, Level: ext.NoticeWarning,
		Timeout: -1, Source: WelcomeID})
}

// mantleVersion is the module version, or "dev+<revision>" for a source build.
func mantleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "dev+" + s.Value[:7]
		}
	}
	return "dev"
}
