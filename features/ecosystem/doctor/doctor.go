// Package doctor is the /doctor panel: mantle's own health checks
// (internal/claudecli/doctor), Claude Code's installation check (`claude
// doctor`, run with the terminal), and "ask Claude to diagnose", which sends
// the engine's bundled /doctor skill.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package doctor

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	checks "github.com/KaitouKid1412/mantle/internal/claudecli/doctor"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.doctor"

// DialogID is the /doctor panel.
const DialogID = "dialog.ecosystem.doctor"

// RunChecks runs the health checks; tests replace it.
var RunChecks = func(ctx context.Context, env checks.Env) []checks.Check { return checks.Run(ctx, env) }

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 340, Parity: []string{"EC-16", "EC-47"}, Setup: Setup})
}

// Setup registers /doctor. The engine's /doctor skill stays reachable from the
// panel and through its /checkup alias.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "doctor", Source: ext.SourceBuiltin,
		Description: "Check mantle's and Claude Code's health",
		Run:         func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(DialogID, nil) },
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.doctor/results", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, nil)
		d.Root().(*view).setChecks(ctx, StoryChecks())
		return d.View(ctx, a)
	}})
	return nil
}

// StoryChecks are fixed results for the story.
func StoryChecks() []checks.Check {
	return []checks.Check{
		{ID: "claude", Title: "Claude Code", Status: checks.OK, Detail: "2.1.288 (/usr/local/bin/claude)"},
		{ID: "go", Title: "Go toolchain", Status: checks.Warn, Detail: "go not found on PATH; mantle runs, but /mantle cannot rebuild it",
			Fix: "Install Go 1.27 or newer."},
		{ID: "terminal", Title: "Terminal", Status: checks.OK, Detail: "ghostty"},
		{ID: "settings", Title: "Settings files", Status: checks.Warn, Detail: "1 of 3 file(s) are invalid",
			Items: []checks.Check{{Title: "local settings", Status: checks.Warn, Detail: ".claude/settings.local.json: line 3, column 1: invalid character '}'"}}},
		{ID: "mantle-home", Title: "mantle install", Status: checks.Skip, Detail: "~/.mantle does not exist yet"},
		{ID: "trust", Title: "Workspace trust", Status: checks.OK, Detail: "trusted in Claude Code"},
	}
}

// New builds the panel and starts the checks.
func New(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	return eco.NewDialog(DialogID, "Doctor", &view{}), nil
}

type view struct {
	eco.Base
	list    eco.List
	checks  []checks.Check
	running bool
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd { return v.run(ctx, d) }

func (v *view) run(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	v.running = true
	eco.Busy(ctx, d, "Running checks")
	env := checks.DefaultEnv(ctx.Session().Cwd)
	env.Claude = eco.CLI(ctx)
	return eco.Async("doctor.run", func() (any, error) {
		return RunChecks(eco.Background(), env), nil
	})
}

func glyph(s checks.Status) (string, theme.Token) {
	switch s {
	case checks.OK:
		return "✓", theme.Success
	case checks.Warn:
		return "!", theme.Warning
	case checks.Fail:
		return "✗", theme.Error
	}
	return "–", theme.Inactive
}

type action int

const (
	actClaudeDoctor action = iota + 1
	actAskClaude
	actUpdate
	actRerun
)

func (v *view) setChecks(ctx ext.Ctx, cs []checks.Check) {
	v.checks, v.running = cs, false
	rows := make([]eco.Row, 0, len(cs)+4)
	for _, c := range cs {
		g, tok := glyph(c.Status)
		rows = append(rows, eco.Row{Key: "check." + c.ID, Label: c.Title, Detail: c.Detail, Glyph: g, GlyphTok: tok,
			Section: "mantle", Value: c})
	}
	engine := ctx.Engine("") != nil
	rows = append(rows,
		eco.Row{Key: "act.claude-doctor", Label: "Run Claude Code's installation check", Detail: "claude doctor", Section: "More", Value: actClaudeDoctor},
		eco.Row{Key: "act.ask", Label: "Ask Claude to diagnose", Detail: "runs the /doctor skill", Section: "More", Value: actAskClaude, Info: !engine},
		eco.Row{Key: "act.update", Label: "Update Claude Code", Detail: "claude update", Section: "More", Value: actUpdate},
		eco.Row{Key: "act.rerun", Label: "Run the checks again", Section: "More", Value: actRerun},
	)
	v.list.MinLabel = 16
	v.list.SetRows(rows)
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case eco.ResultMsg:
		if m.Key != "doctor.run" {
			return nil
		}
		cs, _ := m.Value.([]checks.Check)
		v.setChecks(ctx, cs)
		switch checks.Worst(cs) {
		case checks.OK:
			eco.Done(ctx, d, "Everything looks good.")
		case checks.Warn:
			d.SetStatus(ctx, "Some checks need attention.", theme.Warning)
		default:
			d.SetStatus(ctx, "Some checks failed.", theme.Error)
		}
	case eco.ExecDoneMsg:
		switch m.Key {
		case "doctor.claude":
			if m.Err != nil {
				eco.Fail(ctx, d, m.Err)
			}
		case "doctor.update":
			if m.Err != nil {
				eco.Fail(ctx, d, m.Err)
				return nil
			}
			return tea.Batch(v.run(ctx, d), eco.RestartEngine(ctx, "to run the updated Claude Code"))
		}
	}
	return nil
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if v.list.HandleAction(a, 5) {
		return true, nil
	}
	if a != ext.ActSelectAccept {
		return false, nil
	}
	r, ok := v.list.Selected()
	if !ok {
		return true, nil
	}
	switch val := r.Value.(type) {
	case checks.Check:
		d.Push(ctx, detail(val))
		return true, nil
	case action:
		return true, v.do(ctx, d, val)
	}
	return true, nil
}

func (v *view) Key(ctx ext.Ctx, d *eco.Dialog, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.String() == "r" && !v.running {
		return true, v.run(ctx, d)
	}
	return false, nil
}

func (v *view) do(ctx ext.Ctx, d *eco.Dialog, a action) tea.Cmd {
	switch a {
	case actClaudeDoctor:
		cmd, err := eco.CLI(ctx).DoctorCmd()
		if err != nil {
			return eco.ExecErr("doctor.claude", err)
		}
		return eco.Exec("doctor.claude", cmd)
	case actAskClaude:
		if cmd, ok := eco.SendText(ctx, "/doctor"); ok {
			return tea.Batch(d.Close(ctx), cmd)
		}
	case actUpdate:
		cmd, err := eco.CLI(ctx).UpdateCmd()
		if err != nil {
			return eco.ExecErr("doctor.update", err)
		}
		return eco.Exec("doctor.update", cmd)
	case actRerun:
		if !v.running {
			return v.run(ctx, d)
		}
	}
	return nil
}

func detail(c checks.Check) eco.View {
	var lines []string
	g, _ := glyph(c.Status)
	lines = append(lines, fmt.Sprintf("%s %s", g, c.Detail))
	if c.Fix != "" {
		lines = append(lines, "", "Fix: "+c.Fix)
	}
	for _, it := range c.Items {
		ig, _ := glyph(it.Status)
		lines = append(lines, "", fmt.Sprintf("%s %s", ig, it.Title))
		if it.Detail != "" {
			lines = append(lines, "  "+it.Detail)
		}
		if it.Fix != "" {
			lines = append(lines, "  Fix: "+it.Fix)
		}
	}
	return &eco.TextView{Lines: lines, Sub: c.Title}
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	if v.checks == nil {
		return nil
	}
	return v.list.Render(th, width, height)
}

func (v *view) Hints(ctx ext.Ctx) string {
	return eco.Hint("enter", "details / run", "r", "re-run", "esc", "close")
}
