package sessions

import (
	"encoding/json"
	"fmt"
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
	Usage proto.ContextUsage
	Model string
	Full  bool // /context all: per-file detail
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
		return contextReportMsg{report: &ContextReport{Usage: cu, Model: model, Full: full}}
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
	cellUsed   = "■"
	cellBuffer = "▣"
	cellFree   = "□"
)

// fallbackColors colour categories whose engine colour is not a theme token.
var fallbackColors = []theme.Token{theme.Accent, theme.Permission, theme.PlanMode, theme.AutoAccept,
	theme.Remember, theme.Skill, theme.IDE, theme.Warning, theme.Success}

// renderContextReport draws the grid and the legend.
func renderContextReport(rc ext.RenderCtx, it *ext.Item) ext.Block {
	rep, ok := it.Data.(*ContextReport)
	if !ok || rep == nil {
		return ext.Block{}
	}
	th := rc.Theme
	cu := rep.Usage
	w := max(rc.Width, 20)
	if cu.MaxTokens <= 0 {
		return ext.Block{Lines: []string{fit(th.Paint(theme.Inactive, "No context usage reported"), w)}}
	}

	cols := 10
	if w >= 70 {
		cols = 20
	}
	const rows = 10
	cells := cols * rows
	perCell := float64(cu.MaxTokens) / float64(cells)

	type cat struct {
		name   string
		tokens int64
		tok    theme.Token
		glyph  string
	}
	var cats []cat
	var free, buffer int64
	fi := 0
	for _, c := range cu.Categories {
		switch {
		case c.Kind == "free":
			free += c.Tokens
			continue
		case c.IsDeferred || c.Kind == "deferred":
			continue
		}
		tok := theme.Token(c.Color)
		if c.Color == "" || !th.Has(tok) {
			tok = fallbackColors[fi%len(fallbackColors)]
			fi++
		}
		glyph := cellUsed
		if c.Kind == "buffer" {
			glyph, tok = cellBuffer, theme.Inactive
			buffer += c.Tokens
		}
		cats = append(cats, cat{name: c.Name, tokens: c.Tokens, tok: tok, glyph: glyph})
	}

	// Fill cells in category order; what is left is free space.
	grid := make([]string, 0, cells)
	for _, c := range cats {
		n := int(float64(c.tokens)/perCell + 0.5)
		if n == 0 && c.tokens > 0 {
			n = 1
		}
		for i := 0; i < n && len(grid) < cells; i++ {
			grid = append(grid, th.Paint(c.tok, c.glyph))
		}
	}
	for len(grid) < cells {
		grid = append(grid, th.Paint(theme.Subtle, cellFree))
	}

	used := cu.TotalTokens
	pct := cu.Percentage
	if pct == 0 && cu.MaxTokens > 0 {
		pct = float64(used) / float64(cu.MaxTokens) * 100
	}
	title := th.Fg(theme.Accent).Bold(true).Render("Context usage")
	sub := fmt.Sprintf("%s / %s tokens (%.0f%%)", compactTokens(used), compactTokens(cu.MaxTokens), pct)
	if rep.Model != "" {
		sub = rep.Model + " · " + sub
	}
	lines := []string{title + th.Paint(theme.Inactive, "  "+sub), ""}
	if visibleWidth(lines[0]) > w {
		lines = []string{title, fit(th.Paint(theme.Inactive, sub), w), ""}
	}

	// Legend beside the grid when there is room, else below it.
	var legend []string
	for _, c := range cats {
		legend = append(legend, th.Paint(c.tok, c.glyph)+" "+c.name+th.Paint(theme.Inactive,
			fmt.Sprintf(": %s tokens (%.1f%%)", compactTokens(c.tokens), share(c.tokens, cu.MaxTokens))))
	}
	if free > 0 {
		legend = append(legend, th.Paint(theme.Subtle, cellFree)+" Free space"+th.Paint(theme.Inactive,
			fmt.Sprintf(": %s tokens (%.1f%%)", compactTokens(free), share(free, cu.MaxTokens))))
	}
	gridW := cols*2 - 1
	side := w-gridW-3 >= 30
	for r := 0; r < rows; r++ {
		line := strings.Join(grid[r*cols:(r+1)*cols], " ")
		if side && r < len(legend) {
			line += "   " + fit(legend[r], w-gridW-3)
		}
		lines = append(lines, line)
	}
	if !side || len(legend) > rows {
		start := 0
		if side {
			start = rows
		}
		if !side {
			lines = append(lines, "")
		}
		for _, l := range legend[min(start, len(legend)):] {
			lines = append(lines, fit(l, w))
		}
	}
	if rep.Full {
		lines = append(lines, memoryFileLines(th, cu.MemoryFiles, w)...)
	}
	return ext.Block{Lines: lines}
}

// memoryFileLines lists the memory files (CLAUDE.md and friends) of a full report.
func memoryFileLines(th *theme.Theme, raw json.RawMessage, w int) []string {
	var files []struct {
		Path   string `json:"path"`
		Type   string `json:"type"`
		Tokens int64  `json:"tokens"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &files) != nil || len(files) == 0 {
		return nil
	}
	out := []string{"", th.Fg(theme.Text).Bold(true).Render("Memory files")}
	for _, f := range files {
		line := "  " + f.Path + th.Paint(theme.Inactive, ": "+compactTokens(f.Tokens)+" tokens")
		out = append(out, fit(line, w))
	}
	return out
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
