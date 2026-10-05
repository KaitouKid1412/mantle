package sessions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// KeyContextUsage is the transcript item /context adds: a grid of the context window
// by category. Data is *ContextReport.
const KeyContextUsage ext.ContentKey = "system.context_usage"

// ContextReport is the /context item's payload.
type ContextReport struct {
	Usage  proto.ContextUsage
	Detail ContextDetail // the rest of the get_context_usage reply
	Model  string
	Full   bool // /context all
}

// ContextDetail is the part of the get_context_usage reply proto.ContextUsage leaves
// raw: the engine's own grid, the model, and the per-item breakdowns.
type ContextDetail struct {
	GridRows [][]struct {
		Color          string  `json:"color"`
		IsFilled       bool    `json:"isFilled"`
		CategoryName   string  `json:"categoryName"`
		SquareFullness float64 `json:"squareFullness"`
	} `json:"gridRows"`
	Model       string `json:"model"`
	MemoryFiles []struct {
		Path   string `json:"path"`
		Type   string `json:"type"`
		Tokens int64  `json:"tokens"`
	} `json:"memoryFiles"`
	MCPTools []struct {
		Name       string `json:"name"`
		ServerName string `json:"serverName"`
		Tokens     int64  `json:"tokens"`
		IsLoaded   *bool  `json:"isLoaded"`
	} `json:"mcpTools"`
	Agents []struct {
		AgentType string `json:"agentType"`
		Source    string `json:"source"`
		Tokens    int64  `json:"tokens"`
	} `json:"agents"`
	Skills *struct {
		Tokens           int64 `json:"tokens"`
		SkillFrontmatter []struct {
			Name       string `json:"name"`
			Source     string `json:"source"`
			PluginName string `json:"pluginName"`
			Tokens     int64  `json:"tokens"`
		} `json:"skillFrontmatter"`
	} `json:"skills"`
	AutoCompactThreshold int64 `json:"autoCompactThreshold"`
	IsAutoCompactEnabled *bool `json:"isAutoCompactEnabled"`
}

func (f *feature) registerContext(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("context"), Name: "context", Source: ext.SourceBuiltin,
		Description: "Show what fills the context window",
		ArgHint:     "[all]",
		Run:         f.runContext,
	})
	r.AddRenderer(KeyContextUsage, renderContextReport)
	ext.Subscribe(r, "sessions.context-report", f.onContextReport)
}

type contextReportMsg struct {
	report *ContextReport
	err    error
}

// runContext asks the engine for its context breakdown and adds it to the transcript.
// Engines without get_context_usage get their own /context command.
func (f *feature) runContext(ctx ext.Ctx, args string) tea.Cmd {
	full := strings.TrimSpace(args) == "all"
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	if !eng.Supports(proto.SubGetContextUsage) {
		return sendCommand(ctx, ext.MainEngine, joinCommand("/context", args))
	}
	req := proto.GetContextUsageRequest{}
	if full {
		req.Detail = "full"
	}
	model := f.usage(ext.MainEngine).model
	if model == "" {
		model = ctx.Session().Model
	}
	return controlCmd(eng.Control(proto.SubGetContextUsage, req), func(r ext.ControlResultMsg) tea.Msg {
		var cu proto.ContextUsage
		if err := decodeControl(r, &cu); err != nil {
			return contextReportMsg{err: err}
		}
		rep := &ContextReport{Usage: cu, Model: model, Full: full}
		_ = json.Unmarshal(r.Resp, &rep.Detail)
		if rep.Detail.Model != "" {
			rep.Model = rep.Detail.Model
		}
		return contextReportMsg{report: rep}
	})
}

func (f *feature) onContextReport(ctx ext.Ctx, m contextReportMsg) tea.Cmd {
	if m.err != nil {
		return notice(ctx, "context", "Could not read context usage: "+m.err.Error(), ext.NoticeError)
	}
	u := f.usage(ext.MainEngine)
	u.ctx, u.ctxAt = &m.report.Usage, f.now()
	ctx.Invalidate(contextComponentID)
	f.seq++
	it := &ext.Item{
		ID: "context:" + itoa(f.seq), EngineID: ext.MainEngine, Key: KeyContextUsage,
		Data: m.report, State: ext.Done, Start: f.now(), End: f.now(),
	}
	return ext.Msg(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{it}})
}

// Grid cells.
const (
	cellUsed    = "■"
	cellPartial = "▪"
	cellBuffer  = "▣"
	cellFree    = "□"
)

// fallbackColors colour categories whose engine colour is not a theme token.
var fallbackColors = []theme.Token{theme.Accent, theme.Permission, theme.PlanMode, theme.AutoAccept,
	theme.Remember, theme.Skill, theme.IDE, theme.Warning, theme.Success}

const bufferCategory = "Autocompact buffer"

// renderContextReport draws /context like Claude Code's: the grid with the model, the
// totals and the category legend beside it, then the auto-compact window and what
// memory files, MCP tools, agents and skills cost. It hangs under a ⎿ like other
// command output.
func renderContextReport(rc ext.RenderCtx, it *ext.Item) ext.Block {
	rep, ok := it.Data.(*ContextReport)
	if !ok || rep == nil {
		return ext.Block{}
	}
	th := rc.Theme
	cu, d := rep.Usage, rep.Detail
	w := max(rc.Width, 20)
	const hang, body = "  ⎿  ", "     "
	if cu.MaxTokens <= 0 {
		return ext.Block{Lines: []string{fit(hang+th.Paint(theme.Inactive, "No context usage reported"), w)}}
	}

	colour := map[string]theme.Token{}
	fi := 0
	tokenFor := func(name, col string) theme.Token {
		if t, ok := colour[name]; ok {
			return t
		}
		t := theme.Token(col)
		if col == "" || !th.Has(t) {
			t = fallbackColors[fi%len(fallbackColors)]
			fi++
		}
		colour[name] = t
		return t
	}

	// Legend: used categories, free space, then the buffer (as Claude Code orders it).
	var legend []string
	var free, buffer *proto.ContextCategory
	for i := range cu.Categories {
		c := &cu.Categories[i]
		switch {
		case c.Kind == "free" || c.Name == "Free space":
			free = c
		case c.Kind == "buffer" || c.Name == bufferCategory:
			buffer = c
		case c.IsDeferred || c.Kind == "deferred":
		default:
			legend = append(legend, th.Paint(tokenFor(c.Name, c.Color), cellUsed)+" "+c.Name+
				th.Paint(theme.Inactive, fmt.Sprintf(": %s tokens (%.1f%%)", compactTokens(c.Tokens), share(c.Tokens, cu.MaxTokens))))
		}
	}
	if free != nil {
		legend = append(legend, th.Paint(theme.Subtle, cellFree)+" Free space"+
			th.Paint(theme.Inactive, fmt.Sprintf(": %s (%.1f%%)", compactTokens(free.Tokens), share(free.Tokens, cu.MaxTokens))))
	}
	if buffer != nil {
		legend = append(legend, th.Paint(theme.Inactive, cellBuffer)+" "+buffer.Name+
			th.Paint(theme.Inactive, fmt.Sprintf(": %s tokens (%.1f%%)", compactTokens(buffer.Tokens), share(buffer.Tokens, cu.MaxTokens))))
	}

	grid := engineGrid(th, d, tokenFor)
	if grid == nil {
		grid = localGrid(th, cu, w, tokenFor)
	}
	pct := cu.Percentage
	if pct == 0 {
		pct = share(cu.TotalTokens, cu.MaxTokens)
	}
	right := []string{}
	if rep.Model != "" {
		right = append(right, rep.Model)
	}
	right = append(right, fmt.Sprintf("%s/%s tokens (%.0f%%)", compactTokens(cu.TotalTokens), compactTokens(cu.MaxTokens), pct), "",
		th.Paint(theme.Inactive, "Estimated usage by category"))
	right = append(right, legend...)

	lines := []string{hang + th.Fg(theme.Text).Bold(true).Render("Context usage")}
	gridW := 0
	if len(grid) > 0 {
		gridW = visibleWidth(grid[0])
	}
	if w-len(body)-gridW-3 >= 28 {
		for i := 0; i < max(len(grid), len(right)); i++ {
			left := strings.Repeat(" ", gridW)
			if i < len(grid) {
				left = grid[i]
			}
			line := body + left
			if i < len(right) {
				line += "   " + right[i]
			}
			lines = append(lines, line)
		}
	} else {
		for _, g := range grid {
			lines = append(lines, body+g)
		}
		lines = append(lines, "")
		for _, r := range right {
			lines = append(lines, body+r)
		}
	}

	if d.AutoCompactThreshold > 0 && (d.IsAutoCompactEnabled == nil || *d.IsAutoCompactEnabled) {
		lines = append(lines, "", body+"Auto-compact window: "+compactTokens(d.AutoCompactThreshold)+" tokens")
	}
	section := func(title string, groups []tokenGroup) {
		if len(groups) == 0 {
			return
		}
		lines = append(lines, "", body+th.Fg(theme.Text).Bold(true).Render(title))
		for _, g := range groups {
			lines = append(lines, "")
			if g.label != "" {
				lines = append(lines, body+th.Paint(theme.Inactive, g.label))
			}
			for i, e := range g.entries {
				branch := "├ "
				if i == len(g.entries)-1 {
					branch = "└ "
				}
				lines = append(lines, body+th.Paint(theme.Subtle, branch)+e.name+th.Paint(theme.Inactive, ": ~"+compactTokens(e.tokens)+" tokens"))
			}
		}
	}
	section("Memory files · /memory", memoryGroups(d))
	section("MCP tools · /mcp", mcpGroups(d))
	section("Custom agents · /agents", agentGroups(d))
	section("Skills · /skills", skillGroups(d))
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return ext.Block{Lines: lines}
}

// engineGrid draws the engine's own grid (gridRows), nil when it sent none.
func engineGrid(th *theme.Theme, d ContextDetail, tokenFor func(name, col string) theme.Token) []string {
	if len(d.GridRows) == 0 {
		return nil
	}
	out := make([]string, 0, len(d.GridRows))
	for _, row := range d.GridRows {
		cells := make([]string, 0, len(row))
		for _, c := range row {
			switch {
			case c.CategoryName == bufferCategory:
				cells = append(cells, th.Paint(theme.Inactive, cellBuffer))
			case !c.IsFilled || c.CategoryName == "Free space":
				cells = append(cells, th.Paint(theme.Subtle, cellFree))
			case c.SquareFullness > 0 && c.SquareFullness < 0.7:
				cells = append(cells, th.Paint(tokenFor(c.CategoryName, c.Color), cellPartial))
			default:
				cells = append(cells, th.Paint(tokenFor(c.CategoryName, c.Color), cellUsed))
			}
		}
		out = append(out, strings.Join(cells, " "))
	}
	return out
}

// localGrid fills a grid from the categories when the engine sent no gridRows.
func localGrid(th *theme.Theme, cu proto.ContextUsage, w int, tokenFor func(name, col string) theme.Token) []string {
	cols := 10
	if w >= 70 {
		cols = 20
	}
	const rows = 10
	cells := cols * rows
	perCell := float64(cu.MaxTokens) / float64(cells)
	var used, buf []string
	for _, c := range cu.Categories {
		if c.Kind == "free" || c.IsDeferred || c.Kind == "deferred" || c.Name == "Free space" {
			continue
		}
		n := int(float64(c.Tokens)/perCell + 0.5)
		if n == 0 && c.Tokens > 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			if c.Kind == "buffer" || c.Name == bufferCategory {
				buf = append(buf, th.Paint(theme.Inactive, cellBuffer))
			} else {
				used = append(used, th.Paint(tokenFor(c.Name, c.Color), cellUsed))
			}
		}
	}
	grid := used
	for len(grid)+len(buf) < cells {
		grid = append(grid, th.Paint(theme.Subtle, cellFree))
	}
	grid = append(grid, buf...)
	grid = grid[:cells]
	out := make([]string, rows)
	for r := 0; r < rows; r++ {
		out[r] = strings.Join(grid[r*cols:(r+1)*cols], " ")
	}
	return out
}

type tokenEntry struct {
	name   string
	tokens int64
}

type tokenGroup struct {
	label   string
	entries []tokenEntry
}

// groupBy keeps groups in first-seen order.
func groupBy(n int, label func(i int) string, entry func(i int) tokenEntry) []tokenGroup {
	var out []tokenGroup
	idx := map[string]int{}
	for i := 0; i < n; i++ {
		l := label(i)
		j, ok := idx[l]
		if !ok {
			j = len(out)
			idx[l] = j
			out = append(out, tokenGroup{label: l})
		}
		out[j].entries = append(out[j].entries, entry(i))
	}
	for _, g := range out {
		sort.SliceStable(g.entries, func(a, b int) bool { return g.entries[a].tokens > g.entries[b].tokens })
	}
	return out
}

// sourceLabel names where a skill or agent comes from.
func sourceLabel(source, plugin string) string {
	switch source {
	case "", "builtin", "bundled", "built-in":
		return "Built-in"
	case "plugin":
		if plugin != "" {
			return "Plugin · " + plugin
		}
		return "Plugin"
	case "userSettings", "user":
		return "User"
	case "projectSettings", "project":
		return "Project"
	case "localSettings", "local":
		return "Local"
	case "policySettings", "managed":
		return "Managed"
	}
	return source
}

func memoryGroups(d ContextDetail) []tokenGroup {
	return groupBy(len(d.MemoryFiles), func(int) string { return "" }, func(i int) tokenEntry {
		return tokenEntry{d.MemoryFiles[i].Path, d.MemoryFiles[i].Tokens}
	})
}

func mcpGroups(d ContextDetail) []tokenGroup {
	return groupBy(len(d.MCPTools), func(i int) string { return d.MCPTools[i].ServerName }, func(i int) tokenEntry {
		return tokenEntry{d.MCPTools[i].Name, d.MCPTools[i].Tokens}
	})
}

func agentGroups(d ContextDetail) []tokenGroup {
	return groupBy(len(d.Agents), func(i int) string { return sourceLabel(d.Agents[i].Source, "") }, func(i int) tokenEntry {
		return tokenEntry{d.Agents[i].AgentType, d.Agents[i].Tokens}
	})
}

func skillGroups(d ContextDetail) []tokenGroup {
	if d.Skills == nil {
		return nil
	}
	fm := d.Skills.SkillFrontmatter
	return groupBy(len(fm), func(i int) string { return sourceLabel(fm[i].Source, fm[i].PluginName) }, func(i int) tokenEntry {
		return tokenEntry{fm[i].Name, fm[i].Tokens}
	})
}

func share(n, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

// compactTokens prints 950, 3.1k, 1.2M.
func compactTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
}
