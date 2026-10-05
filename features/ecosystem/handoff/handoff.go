// Package handoff registers the commands that open interactive Claude Code for
// cloud and product features mantle does not rebuild (the user's "hand off"
// decision), /upgrade, /feedback and /bug, and the known gaps.
//
// Each hand-off command checks the engine first: a command the engine lists as
// headless-capable (initialize.commands / commands_changed) is sent to it
// instead (E). Otherwise mantle hands the session to interactive Claude Code
// through plan 06's hidden handoff command.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package handoff

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.handoff"

// Entry is one command that opens interactive Claude Code.
type Entry struct {
	Name, Description, ArgHint string
	Aliases                    []string
	Hidden                     bool
	Parity                     string
}

// Entries is the default hand-off table. The engine's command list overrides it
// at runtime: anything the engine accepts headlessly is passed through instead.
var Entries = []Entry{
	{Name: "remote-control", Aliases: []string{"rc"}, Description: "Control this session from claude.ai (opens Claude Code)", Parity: "CL-01"},
	{Name: "teleport", Aliases: []string{"tp"}, Description: "Resume a cloud session here (opens Claude Code)", Parity: "CL-02"},
	{Name: "desktop", Aliases: []string{"app"}, Description: "Continue in Claude Desktop (opens Claude Code)", Parity: "CL-03"},
	{Name: "mobile", Aliases: []string{"ios", "android"}, Description: "Show a QR code for the mobile app (opens Claude Code)", Parity: "CL-04"},
	{Name: "session", Aliases: []string{"remote"}, Description: "Cloud session URL and QR code (opens Claude Code)", Parity: "CL-05"},
	{Name: "web-setup", Description: "Set up Claude Code on the web (opens Claude Code)", Parity: "CL-06"},
	{Name: "remote-env", Description: "Configure the remote environment (opens Claude Code)", Parity: "CL-07"},
	{Name: "ultraplan", ArgHint: "<prompt>", Description: "Draft an editable plan in the cloud (opens Claude Code)", Parity: "CL-08"},
	{Name: "autofix-pr", Description: "Watch the current PR and fix it (opens Claude Code)", Parity: "CL-09"},
	{Name: "background", Aliases: []string{"bg"}, ArgHint: "[prompt]", Description: "Send this session to the background (opens Claude Code)", Parity: "CL-12"},
	{Name: "chrome", Description: "Claude in Chrome settings (opens Claude Code)", Parity: "CL-14"},
	{Name: "ide", ArgHint: "[open]", Description: "IDE integrations (opens Claude Code)", Parity: "CL-15"},
	{Name: "artifacts", Description: "Browse published artifacts (opens Claude Code)", Parity: "CL-16"},
	{Name: "design-login", Description: "Sign in for design-system access (opens Claude Code)", Parity: "CL-17"},
	{Name: "passes", Description: "Share a free week of Claude Code (opens Claude Code)", Parity: "CL-18"},
	{Name: "usage-credits", Aliases: []string{"extra-usage"}, Description: "Usage credits (opens Claude Code)", Parity: "CL-19"},
	{Name: "rate-limit-options", Hidden: true, Description: "What to do at the usage limit (opens Claude Code)", Parity: "CL-20"},
	{Name: "setup-bedrock", Description: "Set up Amazon Bedrock (opens Claude Code)", Parity: "CL-21"},
	{Name: "setup-vertex", Description: "Set up Google Vertex AI (opens Claude Code)", Parity: "CL-21"},
	{Name: "install-github-app", Description: "Install the Claude GitHub app (opens Claude Code)", Parity: "CL-22"},
	{Name: "install-slack-app", Description: "Install the Claude Slack app (opens Claude Code)", Parity: "CL-23"},
	{Name: "privacy-settings", Description: "Privacy settings (opens Claude Code)", Parity: "CL-24"},
	{Name: "cloud-plugins", Description: "Use local plugins in cloud sessions (opens Claude Code)", Parity: "CL-25"},
	{Name: "daemon", Description: "Background services and routines (opens Claude Code)", Parity: "CL-26"},
	{Name: "pro-trial-expired", Hidden: true, Description: "Trial ended (opens Claude Code)", Parity: "CL-27"},
	{Name: "statusline", ArgHint: "[instructions]", Description: "Set up the status line with Claude (opens Claude Code)", Parity: "EC-24"},
	{Name: "feedback", ArgHint: "[report]", Description: "Send feedback to Anthropic (opens Claude Code)", Parity: "EC-20"},
	{Name: "bug", Aliases: []string{"share", "report"}, ArgHint: "[report]", Description: "Report a bug or share this conversation (opens Claude Code)", Parity: "EC-20"},
	{Name: "powerup", Description: "Interactive lessons (opens Claude Code)", Parity: "EC-37"},
	{Name: "stickers", Description: "Get Claude Code stickers (opens Claude Code)", Parity: "EC-38"},
	{Name: "radio", Description: "Lo-fi radio (opens Claude Code)", Parity: "EC-38"},
	{Name: "wellbeing", Aliases: []string{"breaks", "break-reminder", "downtime"}, Description: "Break reminders and downtime (opens Claude Code)", Parity: "EC-39"},
	{Name: "workflows", Description: "Workflow progress (opens Claude Code)", Parity: "EC-40"},
}

// Gap is a Claude Code command mantle cannot provide, with the reason.
type Gap struct {
	Name, Reason, Parity string
	Aliases              []string
}

// Gaps are commands that stay known gaps (X) unless the engine accepts them.
var Gaps = []Gap{
	{Name: "voice", Reason: "Voice dictation isn't available in mantle: Claude Code's speech-to-text is not exposed to headless clients.", Parity: "GAP-01"},
	{Name: "loops", Reason: "/loops is behind a feature flag in this Claude Code version.", Parity: "GAP-09"},
}

func init() {
	parity := []string{"EC-19"}
	for _, e := range Entries {
		parity = append(parity, e.Parity)
	}
	for _, g := range Gaps {
		parity = append(parity, g.Parity)
	}
	ext.Register(ext.Feature{ID: FeatureID, Order: 320, Parity: parity, Setup: Setup})
}

// Setup registers the hand-off commands, the gaps and /upgrade.
func Setup(r ext.Registrar) error {
	for _, e := range Entries {
		r.AddCommand(ext.Command{
			Name: e.Name, Aliases: e.Aliases, Description: e.Description, ArgHint: e.ArgHint,
			Hidden: e.Hidden, Source: ext.SourceBuiltin, Run: Run(e.Name),
		})
	}
	for _, g := range Gaps {
		r.AddCommand(ext.Command{
			Name: g.Name, Aliases: g.Aliases, Description: "Not available in mantle", Hidden: true,
			Source: ext.SourceBuiltin, Run: gap(g),
		})
	}
	r.AddCommand(ext.Command{
		Name: "upgrade", Description: "Update Claude Code, or upgrade your plan", Source: ext.SourceBuiltin,
		Run: func(ctx ext.Ctx, args string) tea.Cmd { return ctx.OpenDialog(UpgradeDialogID, nil) },
	})
	r.AddDialog(UpgradeDialogID, eco.Factory(NewUpgradeDialog))
	r.AddStory(ext.Story{ID: "ecosystem.handoff/upgrade", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := NewUpgradeDialog(ctx, nil)
		return d.View(ctx, a)
	}})
	return nil
}

// Line builds the slash command line for name and args.
func Line(name, args string) string {
	line := "/" + name
	if args = strings.TrimSpace(args); args != "" {
		line += " " + args
	}
	return line
}

// Run returns the command func for a hand-off command: passed to the engine when
// it accepts the command headlessly, handed off otherwise.
func Run(name string) ext.CommandFunc {
	return func(ctx ext.Ctx, args string) tea.Cmd {
		line := Line(name, args)
		if eco.State.Engine("").HasCommand(name) {
			if cmd, ok := eco.SendText(ctx, line); ok {
				return cmd
			}
		}
		return eco.Handoff(ctx, line)
	}
}

func gap(g Gap) ext.CommandFunc {
	return func(ctx ext.Ctx, args string) tea.Cmd {
		if eco.State.Engine("").HasCommand(g.Name) {
			if cmd, ok := eco.SendText(ctx, Line(g.Name, args)); ok {
				return cmd
			}
		}
		return ctx.Notify(ext.Notice{Key: "ecosystem.gap." + g.Name, Level: ext.NoticeInfo, Text: g.Reason, Source: FeatureID})
	}
}

// UpgradeDialogID is the /upgrade menu.
const UpgradeDialogID = "dialog.ecosystem.upgrade"

// NewUpgradeDialog offers updating the claude binary (claude update, then a
// restart so the engine runs the new version and the conformance probe runs) or
// upgrading the subscription in Claude Code.
func NewUpgradeDialog(ctx ext.Ctx, _ any) (*eco.Dialog, error) {
	menu := eco.NewMenu("", nil,
		eco.MenuItem{Label: "Update Claude Code", Detail: "runs claude update",
			Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
				cmd, err := eco.CLI(ctx).UpdateCmd()
				if err != nil {
					return eco.ExecErr("handoff.update", err)
				}
				return eco.Exec("handoff.update", cmd)
			}},
		eco.MenuItem{Label: "Upgrade your plan", Detail: "opens Claude Code",
			Run: func(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
				d.CloseLine = ""
				return tea.Batch(d.Close(ctx), eco.Handoff(ctx, "/upgrade"))
			}},
	)
	d := eco.NewDialog(UpgradeDialogID, "Upgrade", &upgradeView{MenuView: menu})
	return d, nil
}

type upgradeView struct{ *eco.MenuView }

func (v *upgradeView) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	m, ok := msg.(eco.ExecDoneMsg)
	if !ok || m.Key != "handoff.update" {
		return nil
	}
	if m.Err != nil {
		eco.Fail(ctx, d, m.Err)
		return nil
	}
	d.CloseLine = ""
	return tea.Batch(d.Close(ctx),
		ctx.Notify(ext.Notice{Key: "ecosystem.update", Level: ext.NoticeSuccess, Text: "Claude Code update finished"}),
		eco.RestartEngine(ctx, "to run the updated Claude Code"))
}
