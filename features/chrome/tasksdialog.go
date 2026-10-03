package chrome

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// TasksDialogID is the /tasks dialog.
const TasksDialogID = "dialog.tasks"

// taskOutputPoll is how often the output view refreshes.
const taskOutputPoll = time.Second

type taskPollMsg struct{ gen int }

// tasksDialog lists background work (shells and backgrounded subagents). Enter shows a
// task's output (get_task_output, refreshed every second); x stops a running task or
// clears a finished one; esc goes back, then closes.
type tasksDialog struct {
	tr  *taskTracker
	sel int

	viewing   string // task whose output is shown ("" = the list)
	output    string
	truncated bool
	total     int64
	outErr    string
	pollGen   int
}

func newTasksDialogFactory(tr *taskTracker) ext.DialogFactory {
	return func(ext.Ctx, any) (ext.Dialog, error) { return &tasksDialog{tr: tr}, nil }
}

func (d *tasksDialog) ID() string               { return TasksDialogID }
func (d *tasksDialog) Init(ext.Ctx) tea.Cmd     { return nil }
func (d *tasksDialog) Placement() ext.Placement { return ext.PlaceInline }
func (d *tasksDialog) KeyContext() string       { return ext.ContextSelect }
func (d *tasksDialog) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) {
	return true, nil
}

func (d *tasksDialog) list(ctx ext.Ctx) []*taskInfo {
	return d.tr.backgroundTasks(ctx.Clock().Now(), subagentLinger)
}

func (d *tasksDialog) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.EngineEventMsg:
		if isMain(m.EngineID) {
			ctx.Invalidate(TasksDialogID)
		}
	case taskPollMsg:
		if m.gen == d.pollGen && d.viewing != "" {
			return d.requestOutput(ctx)
		}
	case ext.ControlResultMsg:
		if !isMain(m.EngineID) || m.Subtype != proto.SubGetTaskOutput || d.viewing == "" {
			return nil
		}
		if m.Err != nil {
			d.outErr = m.Err.Error()
		} else {
			var out proto.TaskOutput
			if err := json.Unmarshal(m.Resp, &out); err != nil {
				d.outErr = "unreadable output: " + err.Error()
			} else {
				d.output, d.truncated, d.total, d.outErr = out.Output, out.Truncated, out.TotalBytes, ""
			}
		}
		ctx.Invalidate(TasksDialogID)
		gen := d.pollGen
		return ctx.Clock().Tick(taskOutputPoll, func(time.Time) tea.Msg {
			return ext.AddressedMsg{To: TasksDialogID, Msg: taskPollMsg{gen}}
		})
	}
	return nil
}

func (d *tasksDialog) requestOutput(ctx ext.Ctx) tea.Cmd {
	eng := ctx.Engine("")
	if eng == nil || !eng.Supports(proto.SubGetTaskOutput) {
		d.outErr = "This engine can't show task output."
		ctx.Invalidate(TasksDialogID)
		return nil
	}
	return eng.Control(proto.SubGetTaskOutput, proto.GetTaskOutputRequest{TaskID: d.viewing})
}

func (d *tasksDialog) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	ctx.Invalidate(TasksDialogID)
	rows := d.list(ctx)
	if d.viewing != "" {
		if a == ext.ActSelectCancel {
			d.viewing, d.output, d.outErr = "", "", ""
			d.pollGen++
			return true, nil
		}
		return true, nil
	}
	switch a {
	case ext.ActSelectCancel:
		return true, ctx.CloseDialog(TasksDialogID)
	case ext.ActSelectPrevious:
		d.sel = max(0, d.sel-1)
	case ext.ActSelectNext:
		d.sel = min(d.sel+1, max(0, len(rows)-1))
	case ext.ActSelectFirst, ext.ActSelectPageUp:
		d.sel = 0
	case ext.ActSelectLast, ext.ActSelectPageDown:
		d.sel = max(0, len(rows)-1)
	case ext.ActSelectAccept:
		if len(rows) == 0 {
			return true, nil
		}
		d.viewing, d.output, d.outErr = rows[d.sel].ID, "", ""
		d.pollGen++
		return true, d.requestOutput(ctx)
	default:
		return false, nil
	}
	return true, nil
}

func (d *tasksDialog) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "x", "delete", "backspace":
		rows := d.list(ctx)
		id := d.viewing
		if id == "" && d.sel < len(rows) {
			id = rows[d.sel].ID
		}
		if ti, ok := d.tr.tasks[id]; ok {
			ctx.Invalidate(TasksDialogID)
			return true, stopOrDismiss(ctx, d.tr, ti)
		}
	case "q":
		return true, ctx.CloseDialog(TasksDialogID)
	}
	return true, nil
}

func (d *tasksDialog) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t, sr, now := ctx.Theme(), ctx.Accessibility().ScreenReader, ctx.Clock().Now()
	var lines []string
	if d.viewing != "" {
		lines = d.outputLines(ctx, a)
	} else {
		rows := d.list(ctx)
		d.sel = min(d.sel, max(0, len(rows)-1))
		lines = append(lines, paint(t, sr, theme.Text, "Background tasks"))
		if len(rows) == 0 {
			lines = append(lines, dim(t, sr, "  Nothing is running in the background."))
		}
		for i, ti := range rows {
			mark := "  "
			if i == d.sel {
				mark = paint(t, sr, theme.Suggestion, "❯ ")
			}
			lines = append(lines, mark+subagentRow(t, sr, ti, now))
		}
		lines = append(lines, dim(t, sr, "enter output · x stop · esc close"))
	}
	if !sr {
		rule := t.Paint(theme.Permission, strings.Repeat("─", max(0, a.Width)))
		lines = append([]string{rule}, lines...)
	}
	return ext.Rendered{Text: strings.Join(truncateLines(lines, a.Width, a.MaxHeight), "\n")}
}

// outputLines shows the tail of the viewed task's output that fits.
func (d *tasksDialog) outputLines(ctx ext.Ctx, a ext.Area) []string {
	t, sr := ctx.Theme(), ctx.Accessibility().ScreenReader
	name := d.viewing
	if ti, ok := d.tr.tasks[d.viewing]; ok {
		name = ti.Name() + " · " + ti.Description
		if !ti.Running() {
			name += " (" + ti.Status + ")"
		}
	}
	lines := []string{paint(t, sr, theme.Text, "Output: "+name)}
	switch {
	case d.outErr != "":
		lines = append(lines, paint(t, sr, theme.Error, d.outErr))
	case d.output == "":
		lines = append(lines, dim(t, sr, "  (no output yet)"))
	default:
		_, h := ctx.Size()
		if a.MaxHeight > 0 {
			h = a.MaxHeight
		}
		room := max(3, h-4)
		out := strings.Split(strings.TrimRight(d.output, "\n"), "\n")
		if len(out) > room {
			lines = append(lines, dim(t, sr, fmt.Sprintf("  … %d earlier lines", len(out)-room)))
			out = out[len(out)-room:]
		}
		for _, l := range out {
			lines = append(lines, "  "+sanitizeOutput(l))
		}
		if d.truncated {
			lines = append(lines, dim(t, sr, fmt.Sprintf("  (showing part of %d bytes)", d.total)))
		}
	}
	return append(lines, dim(t, sr, "x stop · esc back"))
}

// sanitizeOutput strips control sequences from task output before display.
func sanitizeOutput(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, ansi.Strip(s))
}

var _ ext.ActionHandler = (*tasksDialog)(nil)
