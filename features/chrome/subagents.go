package chrome

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// SubagentsID is the subagent panel component's ID.
const SubagentsID = "chrome.subagents"

// ActFooterSelect moves keyboard focus into the subagent rows (the editor runs it when
// ↓ has no newer history).
const ActFooterSelect ext.ActionID = "mantle:footerSelect"

// subagentMaxRows is how many subagent rows the panel shows.
const subagentMaxRows = 5

type subagentPruneMsg struct{}

type subagentSLMsg struct {
	gen int
	res statusline.Result
}

// subagentPanel lists running and recently finished subagents below the todos. It is
// focusable in the Footer context: up/down select, enter opens the subagent's
// transcript, x stops it (or dismisses a finished one), esc returns to the prompt.
type subagentPanel struct {
	tr      *taskTracker
	s       sessionState
	sel     int
	focused bool
	pruneAt time.Time // the pending linger timer

	// subagentStatusLine
	cfg       slConfig
	runner    *statusline.Runner
	gen       int
	overrides map[string]string
	cols      int
	newRunner func(statusline.Config) *statusline.Runner
}

func newSubagentPanel(tr *taskTracker) *subagentPanel {
	return &subagentPanel{tr: tr, s: newSessionState(), newRunner: statusline.NewRunner}
}

func (p *subagentPanel) ID() string { return SubagentsID }

func (p *subagentPanel) Init(ctx ext.Ctx) tea.Cmd {
	p.cols, _ = ctx.Size()
	p.cfg = parseSLConfig(ctx.Settings(), "subagentStatusLine")
	return nil
}

func (p *subagentPanel) rows(ctx ext.Ctx) []*taskInfo {
	return p.tr.agents(ctx.Clock().Now(), subagentLinger)
}

func (p *subagentPanel) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	p.s.observe(msg)
	now := ctx.Clock().Now()
	changed := false
	var cmds []tea.Cmd
	switch m := msg.(type) {
	case ext.EngineEventMsg:
		if !isMain(m.EngineID) {
			return nil
		}
		changed = p.tr.observe(now, m.Event)
		if changed {
			cmds = append(cmds, p.armPrune(ctx))
		}
	case subagentPruneMsg:
		p.pruneAt = time.Time{}
		changed = p.tr.prune(now, subagentLinger)
		cmds = append(cmds, p.armPrune(ctx))
	case subagentSLMsg:
		if m.gen == p.gen && p.runner != nil {
			if m.res.Err == nil {
				p.overrides = statusline.ParseRows(m.res.Raw)
			}
			ctx.Invalidate(SubagentsID)
			return waitSubagentSL(p.gen, p.runner.Results())
		}
		return nil
	case tea.WindowSizeMsg:
		p.cols = m.Width
		changed = true
	case ext.SettingsMsg:
		cfg := parseSLConfig(ctx.Settings(), "subagentStatusLine")
		if cfg != p.cfg {
			p.cfg = cfg
			p.stopRunner()
			p.overrides = nil
			changed = true
		}
	default:
		return nil
	}
	if !changed {
		return tea.Batch(cmds...)
	}
	rows := p.rows(ctx)
	p.sel = min(p.sel, max(0, len(rows)-1))
	if p.focused && len(rows) == 0 {
		cmds = append(cmds, p.leave(ctx))
	}
	cmds = append(cmds, p.pushStatusLine(ctx, rows))
	ctx.Invalidate(SubagentsID)
	return tea.Batch(cmds...)
}

// armPrune schedules the next linger expiry.
func (p *subagentPanel) armPrune(ctx ext.Ctx) tea.Cmd {
	var next time.Time
	for _, ti := range p.tr.tasks {
		if !ti.Running() {
			if at := ti.End.Add(subagentLinger); next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	if next.IsZero() || (!p.pruneAt.IsZero() && !next.Before(p.pruneAt)) {
		return nil
	}
	p.pruneAt = next
	d := max(next.Sub(ctx.Clock().Now()), time.Second)
	return ctx.Clock().Tick(d, func(time.Time) tea.Msg {
		return ext.AddressedMsg{To: SubagentsID, Msg: subagentPruneMsg{}}
	})
}

// pushStatusLine sends the rows to the subagentStatusLine command, if configured.
func (p *subagentPanel) pushStatusLine(ctx ext.Ctx, rows []*taskInfo) tea.Cmd {
	if p.cfg.Command == "" || len(rows) == 0 {
		return nil
	}
	var cmd tea.Cmd
	if p.runner == nil {
		p.gen++
		p.runner = p.newRunner(statusline.Config{
			Command: p.cfg.Command, RefreshInterval: p.cfg.RefreshInterval, Dir: p.s.Cwd,
		})
		cmd = waitSubagentSL(p.gen, p.runner.Results())
	}
	payload := statusline.SubagentPayload{
		SessionID: p.s.SessionID,
		TranscriptPath: transcriptPath(claudeConfigDir(terminal.OS()),
			p.s.ProjectDir, p.s.SessionID),
		Cwd:            p.s.Cwd,
		PermissionMode: p.s.Mode,
		Columns:        max(10, p.cols-4),
	}
	for _, ti := range rows {
		payload.Tasks = append(payload.Tasks, statusline.SubagentTask{
			ID: ti.ID, Name: ti.Name(), Type: ti.TaskType, Status: ti.Status,
			Description: ti.Description, Label: ti.Name(), StartTime: ti.Start.UnixMilli(),
			TokenCount: int(ti.Tokens), TokenSamples: ti.TokenSamples, Cwd: p.s.Cwd,
		})
	}
	if data, err := payload.Marshal(); err == nil {
		p.runner.Update(data)
	}
	return cmd
}

func (p *subagentPanel) stopRunner() {
	if p.runner != nil {
		p.runner.Close()
		p.runner = nil
	}
}

func waitSubagentSL(gen int, ch <-chan statusline.Result) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return nil
		}
		return ext.AddressedMsg{To: SubagentsID, Msg: subagentSLMsg{gen: gen, res: res}}
	}
}

// selectFirst is mantle:footerSelect.
func (p *subagentPanel) selectFirst(ctx ext.Ctx) (bool, tea.Cmd) {
	if len(p.rows(ctx)) == 0 {
		return false, nil
	}
	p.sel = 0
	return true, ctx.Focus(SubagentsID)
}

func (p *subagentPanel) leave(ctx ext.Ctx) tea.Cmd {
	return ctx.Focus(EditorID)
}

// Focusable.

func (p *subagentPanel) KeyContext() string { return ext.ContextFooter }

// HandleKey: any other key hands focus back to the prompt.
func (p *subagentPanel) HandleKey(ctx ext.Ctx, _ tea.KeyPressMsg) (bool, tea.Cmd) {
	return true, p.leave(ctx)
}

func (p *subagentPanel) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

func (p *subagentPanel) OnFocus(ctx ext.Ctx) tea.Cmd {
	p.focused = true
	ctx.Invalidate(SubagentsID)
	return nil
}

func (p *subagentPanel) OnBlur(ctx ext.Ctx) tea.Cmd {
	p.focused = false
	ctx.Invalidate(SubagentsID)
	return nil
}

func (p *subagentPanel) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	rows := p.rows(ctx)
	if len(rows) == 0 {
		return true, p.leave(ctx)
	}
	ctx.Invalidate(SubagentsID)
	switch a {
	case ext.ActFooterUp, ext.ActFooterPrevious:
		if p.sel == 0 {
			return true, p.leave(ctx)
		}
		p.sel--
	case ext.ActFooterDown, ext.ActFooterNext:
		p.sel = min(p.sel+1, len(rows)-1)
	case ext.ActFooterClearSelection, ext.ActFooterDismiss:
		return true, p.leave(ctx)
	case ext.ActFooterOpenSelected:
		ti := rows[p.sel]
		return true, ctx.OpenDialog(SubagentViewID, subagentViewArgs{
			TaskID: ti.ID, ToolUseID: ti.ToolUseID, Title: ti.Name() + " · " + ti.Description})
	case ext.ActFooterClose:
		return true, stopOrDismiss(ctx, p.tr, rows[p.sel])
	default:
		return false, nil
	}
	return true, nil
}

// stopOrDismiss stops a running task (stop_task) or forgets a finished one.
func stopOrDismiss(ctx ext.Ctx, tr *taskTracker, ti *taskInfo) tea.Cmd {
	if !ti.Running() {
		tr.dismiss(ti.ID)
		return nil
	}
	eng := ctx.Engine("")
	if eng == nil || !eng.Supports(proto.SubStopTask) {
		return ctx.Notify(ext.Notice{Key: "chrome:stop", Text: "This engine can't stop tasks", Level: ext.NoticeWarning, Source: SubagentsID})
	}
	return tea.Batch(
		eng.Control(proto.SubStopTask, proto.StopTaskRequest{TaskID: ti.ID}),
		ctx.Notify(ext.Notice{Key: "chrome:stop:" + ti.ID, Text: "Stopping " + ti.Name() + "…", Source: SubagentsID}),
	)
}

func (p *subagentPanel) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	rows := p.rows(ctx)
	if len(rows) == 0 || a.Width <= 0 {
		return ext.Rendered{}
	}
	t, sr, now := ctx.Theme(), ctx.Accessibility().ScreenReader, ctx.Clock().Now()
	shown := rows
	start := 0
	if len(rows) > subagentMaxRows {
		start = min(max(0, p.sel-subagentMaxRows+1), len(rows)-subagentMaxRows)
		shown = rows[start : start+subagentMaxRows]
	}
	var lines []string
	for i, ti := range shown {
		body, ok := p.overrides[ti.ID]
		if ok && body == "" {
			continue // the subagentStatusLine command hid this row
		}
		if !ok {
			body = subagentRow(t, sr, ti, now)
		}
		mark := "  "
		if p.focused && start+i == p.sel {
			mark = paint(t, sr, theme.Suggestion, "❯ ")
		}
		lines = append(lines, mark+body)
	}
	if hidden := len(rows) - len(shown); hidden > 0 {
		lines = append(lines, dim(t, sr, fmt.Sprintf("  … %d more", hidden)))
	}
	if p.focused {
		hint := fmt.Sprintf("  %s view · %s stop · %s back",
			firstKey(ctx, ext.ContextFooter, ext.ActFooterOpenSelected, "enter"),
			firstKey(ctx, ext.ContextFooter, ext.ActFooterClose, "x"),
			firstKey(ctx, ext.ContextFooter, ext.ActFooterClearSelection, "esc"))
		lines = append(lines, dim(t, sr, hint))
	}
	return ext.Rendered{Text: strings.Join(truncateLines(lines, a.Width, a.MaxHeight), "\n")}
}

// subagentRow is the default row: status, name, description, last tool, tokens, time.
func subagentRow(t *theme.Theme, sr bool, ti *taskInfo, now time.Time) string {
	glyph, tok := "◐", theme.Accent
	switch ti.Status {
	case TaskCompleted:
		glyph, tok = "✓", theme.Success
	case TaskFailed:
		glyph, tok = "✗", theme.Error
	case TaskStopped:
		glyph, tok = "■", theme.Inactive
	}
	if sr {
		glyph = map[string]string{TaskCompleted: "[done]", TaskFailed: "[failed]", TaskStopped: "[stopped]"}[ti.Status]
		if glyph == "" {
			glyph = "[running]"
		}
	}
	parts := []string{paint(t, sr, tok, glyph), paint(t, sr, theme.Text, ti.Name()+":")}
	if ti.Description != "" {
		parts = append(parts, paint(t, sr, theme.Text, ti.Description))
	}
	var meta []string
	if ti.Running() && ti.LastTool != "" {
		meta = append(meta, ti.LastTool)
	}
	if ti.Tokens > 0 {
		meta = append(meta, formatTokens(ti.Tokens)+" tokens")
	}
	if d := ti.Elapsed(now); d > 0 {
		meta = append(meta, shortDuration(d))
	}
	if ti.Status == TaskFailed && ti.Error != "" {
		meta = append(meta, ansi.Truncate(ti.Error, 40, "…"))
	}
	line := strings.Join(parts, " ")
	if len(meta) > 0 {
		line += dim(t, sr, " · "+strings.Join(meta, " · "))
	}
	return line
}

var (
	_ ext.Focusable     = (*subagentPanel)(nil)
	_ ext.ActionHandler = (*subagentPanel)(nil)
	_ ext.FocusAware    = (*subagentPanel)(nil)
)
