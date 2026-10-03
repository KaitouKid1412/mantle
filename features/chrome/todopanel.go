package chrome

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// TodosID is the todo panel component's ID.
const TodosID = "chrome.todos"

// todoMaxItems is how many checklist items the expanded panel shows.
const todoMaxItems = 5

// todoPanel shows the main conversation's checklist above the prompt. ctrl+t
// (app:toggleTodos) switches between the full list and a one-line summary; the choice
// is remembered across sessions.
type todoPanel struct {
	list      Todos
	expanded  bool
	enabled   bool
	sessionID string
}

func newTodoPanel() *todoPanel { return &todoPanel{enabled: true} }

func (p *todoPanel) ID() string { return TodosID }

func (p *todoPanel) Init(ctx ext.Ctx) tea.Cmd {
	p.enabled = ext.ClaudeBool(ctx.Settings(), "todoFeatureEnabled", true)
	var expanded bool
	if ok, err := ctx.Store(TodosID).Get("expanded", &expanded); ok && err == nil {
		p.expanded = expanded
	}
	return nil
}

func (p *todoPanel) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	changed := false
	switch m := msg.(type) {
	case ext.SettingsMsg:
		en := ext.ClaudeBool(ctx.Settings(), "todoFeatureEnabled", true)
		changed, p.enabled = en != p.enabled, en
	case ext.SessionChangedMsg:
		if isMain(m.EngineID) && m.Info.SessionID != "" && m.Info.SessionID != p.sessionID {
			p.sessionID = m.Info.SessionID
			changed = p.rebuild(ctx.Transcript())
		}
	case ext.EngineEventMsg:
		if isMain(m.EngineID) {
			changed = p.observe(m.Event)
		}
	}
	if changed {
		ctx.Invalidate(TodosID)
	}
	return nil
}

func (p *todoPanel) observe(ev proto.Event) bool {
	changed := false
	switch e := ev.(type) {
	case *proto.Assistant:
		if e.ParentToolUseID != "" {
			return false // subagents keep their own lists
		}
		for _, b := range e.Message.Content {
			if tu, ok := b.ToolUse(); ok && p.list.OnToolUse(tu.ID, tu.Name, tu.Input) {
				changed = true
			}
		}
	case *proto.User:
		if e.ParentToolUseID != "" {
			return false
		}
		for _, r := range e.ToolResults() {
			if p.list.OnToolResult(r.ToolUseID, r.IsError, r.Content.PlainText(), r.Structured) {
				changed = true
			}
		}
	case *proto.ConversationReset:
		p.list.Reset()
		changed = true
	}
	return changed
}

// rebuild replays checklist tool calls from the transcript (a resumed session).
func (p *todoPanel) rebuild(tr ext.Transcript) bool {
	p.list.Reset()
	if tr == nil {
		return true
	}
	for _, it := range tr.Items() {
		tu, ok := it.Data.(*proto.ToolUse)
		if !ok || it.ParentID != "" {
			continue
		}
		p.list.OnToolUse(tu.ID, tu.Name, tu.Input)
		if r := it.Result; r != nil {
			p.list.OnToolResult(tu.ID, r.IsError, r.Content.PlainText(), r.Structured)
		}
	}
	return true
}

// toggle is app:toggleTodos.
func (p *todoPanel) toggle(ctx ext.Ctx) (bool, tea.Cmd) {
	p.expanded = !p.expanded
	if err := ctx.Store(TodosID).Set("expanded", p.expanded); err != nil {
		ctx.Log().Warn("todos: save expanded state", "err", err)
	}
	ctx.Invalidate(TodosID)
	return true, nil
}

func (p *todoPanel) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if !p.enabled || len(p.list.Items()) == 0 || a.Width <= 0 {
		return ext.Rendered{}
	}
	sr := ctx.Accessibility().ScreenReader
	toggleKey := firstKey(ctx, ext.ContextGlobal, ext.ActAppToggleTodos, "ctrl+t")
	var lines []string
	if p.expanded {
		lines = p.expandedLines(ctx.Theme(), sr, toggleKey)
	} else if line := p.summaryLine(ctx.Theme(), sr, toggleKey); line != "" {
		lines = []string{line}
	}
	return ext.Rendered{Text: strings.Join(truncateLines(lines, a.Width, a.MaxHeight), "\n")}
}

func (p *todoPanel) header(t *theme.Theme, sr bool) string {
	done, total := p.list.Counts()
	h := fmt.Sprintf("Tasks %d/%d done", done, total)
	if sr {
		return h
	}
	return t.Paint(theme.Text, "Tasks") + t.Paint(theme.Inactive, fmt.Sprintf(" %d/%d done", done, total))
}

// summaryLine is the collapsed view: progress and the current item. A finished list
// collapses to nothing.
func (p *todoPanel) summaryLine(t *theme.Theme, sr bool, key string) string {
	if p.list.AllDone() {
		return ""
	}
	line := p.header(t, sr)
	if cur, ok := p.list.Current(); ok {
		line += dim(t, sr, " · ") + paint(t, sr, theme.Text, cur.Label())
	}
	return line + dim(t, sr, " ("+key+" to show)")
}

func (p *todoPanel) expandedLines(t *theme.Theme, sr bool, key string) []string {
	lines := []string{p.header(t, sr) + dim(t, sr, " ("+key+" to hide)")}
	shown, before, after := p.list.Window(todoMaxItems)
	if before > 0 {
		lines = append(lines, dim(t, sr, fmt.Sprintf("  … %d earlier", before)))
	}
	for _, it := range shown {
		lines = append(lines, "  "+todoLine(t, sr, it))
	}
	if after > 0 {
		lines = append(lines, dim(t, sr, fmt.Sprintf("  … %d more", after)))
	}
	return lines
}

func todoLine(t *theme.Theme, sr bool, it Todo) string {
	if sr {
		mark := map[TodoStatus]string{TodoCompleted: "[x]", TodoInProgress: "[>]"}[it.Status]
		if mark == "" {
			mark = "[ ]"
		}
		return mark + " " + it.Label()
	}
	switch it.Status {
	case TodoCompleted:
		return t.Paint(theme.Success, "✓") + " " + t.Fg(theme.Inactive).Strikethrough(true).Render(it.Label())
	case TodoInProgress:
		return t.Paint(theme.Accent, "▸") + " " + t.Fg(theme.Text).Bold(true).Render(it.Label())
	}
	return t.Paint(theme.Inactive, "○") + " " + t.Paint(theme.Text, it.Label())
}

func dim(t *theme.Theme, sr bool, s string) string { return paint(t, sr, theme.Inactive, s) }

func paint(t *theme.Theme, sr bool, tok theme.Token, s string) string {
	if sr {
		return ansi.Strip(s)
	}
	return t.Paint(tok, s)
}
