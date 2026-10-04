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

// HandoffDialogID is plan 06's generic hand-off (B5): it stops the engine, runs
// interactive `claude --resume <sid> [args...]` with the terminal, then resumes
// the engine and reprints new history. Its args are a []string of extra claude
// arguments.
const HandoffDialogID = "dialog.handoff"

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

// Handoff opens interactive Claude Code on this session through plan 06's
// hand-off dialog. A non-empty cmdline (e.g. "/remote-control") is passed as
// the initial input, so Claude Code runs that command at startup.
func Handoff(ctx ext.Ctx, cmdline string) tea.Cmd {
	var args []string
	if cmdline = strings.TrimSpace(cmdline); cmdline != "" {
		args = []string{cmdline}
	}
	return ctx.OpenDialog(HandoffDialogID, args)
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
