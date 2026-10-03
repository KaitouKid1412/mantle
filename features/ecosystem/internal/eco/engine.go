package eco

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// RestartDialogID is the confirm dialog shown before a restart that would kill
// background tasks. The ecosystem root feature registers it.
const RestartDialogID = "dialog.ecosystem.restart"

// HandoffCommand is the hidden command plan 06 registers for the generic
// hand-off to interactive Claude Code: its args are the slash command to run
// there (empty to just open the session). See
// docs/plans/requests/09-06-handoff-command.md.
const HandoffCommand = "handoff"

// RestartArgs are the RestartDialogID arguments.
type RestartArgs struct {
	Reason string // why, shown in the dialog and the notice ("to load the new login")
}

// RestartEngine restarts the main engine on the same session (--resume) so it
// picks up new credentials, plugins or MCP servers. If background tasks are
// running, which a restart kills, it asks first.
func RestartEngine(ctx ext.Ctx, reason string) tea.Cmd {
	if ctx.Engine("") == nil {
		return ctx.Notify(ext.Notice{Key: "ecosystem.restart", Level: ext.NoticeInfo,
			Text: "Claude isn't running; the change applies when it starts."})
	}
	if len(State.Engine("").ForegroundTasks()) > 0 {
		return ctx.OpenDialog(RestartDialogID, RestartArgs{Reason: reason})
	}
	return restartNow(ctx, reason)
}

func restartNow(ctx ext.Ctx, reason string) tea.Cmd {
	eng := ctx.Engine("")
	if eng == nil {
		return nil
	}
	s := ctx.Session()
	text := "Restarting Claude"
	if reason != "" {
		text += " " + reason
	}
	return tea.Batch(
		ctx.Notify(ext.Notice{Key: "ecosystem.restart", Level: ext.NoticeInfo, Text: text + "…"}),
		eng.Restart(ext.SpawnOpts{Cwd: s.Cwd, Resume: s.SessionID, Model: s.Model, PermissionMode: s.PermissionMode}),
	)
}

// NewRestartDialog builds the confirm dialog behind RestartDialogID.
func NewRestartDialog(ctx ext.Ctx, args any) (*Dialog, error) {
	a, _ := args.(RestartArgs)
	tasks := State.Engine("").ForegroundTasks()
	q := []string{fmt.Sprintf("Restarting Claude stops %d background task(s):", len(tasks))}
	for i, t := range tasks {
		if i == 5 {
			q = append(q, fmt.Sprintf("  … and %d more", len(tasks)-5))
			break
		}
		desc := t.Description
		if desc == "" {
			desc = t.TaskType
		}
		q = append(q, "  • "+desc)
	}
	if a.Reason != "" {
		q = append(q, "", "The restart is needed "+a.Reason+".")
	}
	c := &ConfirmView{Question: q, YesLabel: "Restart now", Danger: true,
		OnYes: func(ctx ext.Ctx, d *Dialog) tea.Cmd { return restartNow(ctx, a.Reason) }}
	return NewDialog(RestartDialogID, "Restart Claude", c), nil
}

// Handoff opens interactive Claude Code on this session to run cmdline (e.g.
// "/remote-control"), through plan 06's hidden handoff command, which stops the
// engine first and resumes it afterwards. Without it, a notice explains how to
// do it by hand.
func Handoff(ctx ext.Ctx, cmdline string) tea.Cmd {
	if c, ok := ctx.Command(HandoffCommand); ok && c.Run != nil {
		return c.Run(ctx, cmdline)
	}
	sid := ctx.Session().SessionID
	how := "claude"
	if sid != "" {
		how += " --resume " + sid
	}
	text := fmt.Sprintf("Opening Claude Code from mantle isn't available yet. Run `%s`", how)
	if cmdline = strings.TrimSpace(cmdline); cmdline != "" {
		text += " and type " + cmdline
	}
	return ctx.Notify(ext.Notice{Key: "ecosystem.handoff", Level: ext.NoticeWarning, Text: text + "."})
}

// Busy sets a "working" status on the dialog.
func Busy(ctx ext.Ctx, d *Dialog, text string) {
	d.SetStatus(ctx, text+"…", theme.Inactive)
}

// Fail shows an error on the dialog's status line.
func Fail(ctx ext.Ctx, d *Dialog, err error) {
	d.SetStatus(ctx, err.Error(), theme.Error)
}

// Done shows a success message on the dialog's status line.
func Done(ctx ext.Ctx, d *Dialog, text string) {
	d.SetStatus(ctx, text, theme.Success)
}
