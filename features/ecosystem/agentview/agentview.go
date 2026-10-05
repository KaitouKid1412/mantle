// Package agentview lists Claude Code's background sessions (`claude agents
// --json`). Enter attaches to one with the terminal (`claude attach <id>`), l
// shows its recent output (`claude logs <id>`), x stops it (`claude stop
// <id>`; the conversation is kept). Interactive sessions in other terminals
// are listed for reference only.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package agentview

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.agentview"

// DialogID is the agent view; other plans may open it by this ID.
const DialogID = "dialog.ecosystem.agentview"

// ActionOpen opens the agent view (mantle-only; no default binding).
const ActionOpen ext.ActionID = "mantle:agentView"

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 420, Parity: []string{"CL-10", "CL-11"}, Setup: Setup})
}

// Setup registers /agent-view, its action and dialog.
func Setup(r ext.Registrar) error {
	open := func(ctx ext.Ctx) tea.Cmd { return ctx.OpenDialog(DialogID, nil) }
	r.AddCommand(ext.Command{
		// mantle-only, so kept out of the / menu like Claude Code's; typing it works.
		Name: "agent-view", Aliases: []string{"bg-sessions"}, Hidden: true, Source: ext.SourceBuiltin,
		Description: "Background Claude Code sessions: attach, logs, stop",
		Run:         func(ctx ext.Ctx, args string) tea.Cmd { return open(ctx) },
	})
	r.AddAction(ext.Action{ID: ActionOpen, Context: ext.ContextGlobal, Description: "Open the agent view",
		Run: func(ctx ext.Ctx) (bool, tea.Cmd) { return true, open(ctx) }})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.agentview/sessions", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setSessions(ctx, StorySessions(ctx.Clock().Now()))
		return d.View(ctx, a)
	}})
	return nil
}

// New builds the view.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Background sessions", &view{}), nil
}

type view struct {
	eco.Base
	list     eco.List
	sessions []claudecli.BackgroundSession
	all      bool
	loaded   bool
}

func (v *view) Subtitle() string {
	if v.all {
		return "including finished"
	}
	return ""
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.refresh(ctx, d) }

func (v *view) refresh(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	if !v.loaded {
		eco.Busy(ctx, d, "Loading sessions")
	}
	cli, all := eco.CLI(ctx), v.all
	return eco.Async("agentview.list", func() (any, error) { return cli.Agents(eco.Background(), all, "") })
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func stateLook(s claudecli.BackgroundSession) (string, theme.Token) {
	switch strings.ToLower(s.Status) {
	case "busy":
		return "●", theme.Warning
	case "idle":
		return "○", theme.Success
	case "done", "completed", "stopped", "exited":
		return "✓", theme.Inactive
	}
	return "·", theme.Inactive
}

func (v *view) setSessions(ctx ext.Ctx, sessions []claudecli.BackgroundSession) {
	sort.SliceStable(sessions, func(i, j int) bool {
		bi, bj := sessions[i].Kind == "background", sessions[j].Kind == "background"
		if bi != bj {
			return bi
		}
		return sessions[i].StartedAtMs > sessions[j].StartedAtMs
	})
	v.sessions, v.loaded = sessions, true
	now := ctx.Clock().Now()
	rows := make([]eco.Row, 0, len(sessions))
	for _, s := range sessions {
		g, tok := stateLook(s)
		name := s.Name
		if name == "" {
			name = s.ID
		}
		state := s.Status
		if s.State != "" && s.State != s.Status {
			state += " · " + s.State
		}
		parts := []string{state, filepath.Base(s.CWD)}
		if a := ago(now, s.StartedAt()); a != "" {
			parts = append(parts, "started "+a)
		}
		section := "Background"
		if s.Kind != "background" {
			section = "Interactive · other terminals"
		}
		key := s.ID
		if key == "" {
			key = s.SessionID
		}
		rows = append(rows, eco.Row{Key: key, Label: name, Detail: strings.Join(parts, " · "), Glyph: g, GlyphTok: tok,
			Section: section, Value: s})
	}
	v.list.Empty = "No background sessions. Start one with claude --bg, or /background in Claude Code."
	v.list.MinLabel = 18
	v.list.SetRows(rows)
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ResultMsg:
		switch m.Key {
		case "agentview.list":
			if m.Err != nil {
				eco.Fail(ctx, d, m.Err)
				return nil
			}
			if !v.loaded {
				d.SetStatus(ctx, "", "")
			}
			s, _ := m.Value.([]claudecli.BackgroundSession)
			v.setSessions(ctx, s)
		case "agentview.logs":
			if m.Err != nil {
				eco.Fail(ctx, d, m.Err)
				return nil
			}
			d.SetStatus(ctx, "", "")
			r := m.Value.(logs)
			lines := strings.Split(strings.TrimRight(r.text, "\n"), "\n")
			if strings.TrimSpace(r.text) == "" {
				lines = []string{"No output yet."}
			}
			tv := &eco.TextView{Lines: lines, Sub: r.name}
			d.Push(ctx, tv)
		case "agentview.stop":
			if m.Err != nil {
				eco.Fail(ctx, d, m.Err)
			} else {
				eco.Done(ctx, d, fmt.Sprint(m.Value)+" stopped; resume it later with claude attach.")
			}
			return v.refresh(ctx, d)
		}
	case eco.ExecDoneMsg:
		if m.Key != "agentview.attach" {
			return nil
		}
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
		}
		return v.refresh(ctx, d)
	}
	return nil
}

type logs struct{ name, text string }

func (v *view) selected() (claudecli.BackgroundSession, bool) {
	r, ok := v.list.Selected()
	if !ok {
		return claudecli.BackgroundSession{}, false
	}
	return r.Value.(claudecli.BackgroundSession), true
}

// needBackground explains why an interactive session can't be acted on.
func needBackground(ctx ext.Ctx, d *eco.Dialog, s claudecli.BackgroundSession) bool {
	if s.Kind == "background" && s.ID != "" {
		return true
	}
	d.SetStatus(ctx, "That session runs in another terminal; switch to it there.", theme.Inactive)
	return false
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a != ext.ActSelectAccept {
		return false, nil
	}
	s, ok := v.selected()
	if !ok || !needBackground(ctx, d, s) {
		return true, nil
	}
	cmd, err := eco.CLI(ctx).AttachCmd(s.ID)
	if err != nil {
		return true, eco.ExecErr("agentview.attach", err)
	}
	return true, eco.Exec("agentview.attach", cmd)
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "a":
		v.all = !v.all
		return true, v.refresh(ctx, d)
	case "r":
		return true, v.refresh(ctx, d)
	}
	s, ok := v.selected()
	if !ok {
		return false, nil
	}
	switch k.String() {
	case "l":
		if !needBackground(ctx, d, s) {
			return true, nil
		}
		eco.Busy(ctx, d, "Loading output")
		cli, id, name := eco.CLI(ctx), s.ID, s.Name
		return true, eco.Async("agentview.logs", func() (any, error) {
			out, err := cli.Logs(eco.Background(), id)
			return logs{name: name, text: out}, err
		})
	case "x":
		if !needBackground(ctx, d, s) {
			return true, nil
		}
		d.Push(ctx, &eco.ConfirmView{
			Question: []string{fmt.Sprintf("Stop %s?", s.Name), "The conversation is kept; you can resume it with claude attach."},
			YesLabel: "Stop", Danger: true,
			OnYes: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
				cli, id, name := eco.CLI(ctx), s.ID, s.Name
				return eco.Async("agentview.stop", func() (any, error) { return name, cli.StopSession(eco.Background(), id) })
			},
		})
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
	all := "show finished"
	if v.all {
		all = "hide finished"
	}
	return eco.Hint("enter", "attach", "l", "logs", "x", "stop", "a", all, "r", "refresh", "esc", "close")
}

// StorySessions is fixed data for the story.
func StorySessions(now time.Time) []claudecli.BackgroundSession {
	return []claudecli.BackgroundSession{
		{ID: "2a3c85ff", Name: "repo-task", Kind: "background", Status: "busy", State: "working", CWD: "/work/repo",
			StartedAtMs: now.Add(-12 * time.Minute).UnixMilli()},
		{ID: "9be41c07", Name: "docs-refresh", Kind: "background", Status: "idle", CWD: "/work/docs",
			StartedAtMs: now.Add(-3 * time.Hour).UnixMilli()},
		{Name: "home-1", Kind: "interactive", Status: "idle", CWD: "/home/user", StartedAtMs: now.Add(-26 * time.Hour).UnixMilli()},
	}
}
