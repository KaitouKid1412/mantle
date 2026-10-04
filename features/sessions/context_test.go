package sessions

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

func sampleContext() proto.ContextUsage {
	return proto.ContextUsage{
		TotalTokens: 60_000, MaxTokens: 200_000, Percentage: 30,
		Categories: []proto.ContextCategory{
			{Name: "System prompt", Tokens: 3_100, Color: "promptBorder"},
			{Name: "System tools", Tokens: 12_000, Color: "inactive"},
			{Name: "Memory files", Tokens: 900, Color: "nonexistent-colour"},
			{Name: "Messages", Tokens: 44_000, Color: "claude"},
			{Name: "Autocompact buffer", Tokens: 33_000, Kind: "buffer"},
			{Name: "Free space", Tokens: 107_000, Kind: "free"},
		},
		MemoryFiles: json.RawMessage(`[{"path":"/work/demo/CLAUDE.md","type":"Project","tokens":900}]`),
	}
}

func TestContextCommand(t *testing.T) {
	h := newHarness(t)
	h.command("context", "")
	if !slices.Equal(h.eng.sent, []string{"/context"}) {
		t.Fatalf("unsupported: sent %q", h.eng.sent)
	}
	h.eng.supports[proto.SubGetContextUsage] = true
	reply, _ := json.Marshal(sampleContext())
	h.eng.reply[proto.SubGetContextUsage] = reply
	h.command("context", "all")
	if req := h.eng.controls[0].req.(proto.GetContextUsageRequest); req.Detail != "full" {
		t.Fatalf("req = %+v", req)
	}
	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 1 || len(hist[0].Items) != 1 || hist[0].Reset || hist[0].Items[0].Key != KeyContextUsage {
		t.Fatalf("history = %+v", hist)
	}
	rep := hist[0].Items[0].Data.(*ContextReport)
	if !rep.Full || rep.Usage.TotalTokens != 60_000 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRenderContextReport(t *testing.T) {
	th := theme.Default()
	it := &ext.Item{Key: KeyContextUsage, Data: &ContextReport{Usage: sampleContext(), Model: "opus", Full: true}}
	for _, w := range []int{40, 100} {
		b := renderContextReport(ext.RenderCtx{Width: w, Theme: &th}, it)
		text := ansi.Strip(strings.Join(b.Lines, "\n"))
		for _, want := range []string{"Context usage", "opus · 60k / 200k tokens (30%)", "Messages: 44k tokens (22.0%)",
			"Free space: 107k tokens", "Memory files", "/work/demo/CLAUDE.md: 900 tokens"} {
			if !strings.Contains(text, want) {
				t.Errorf("width %d lacks %q:\n%s", w, want, text)
			}
		}
		cells := 0
		for _, l := range b.Lines {
			if ansi.StringWidth(l) > w {
				t.Fatalf("width %d: line too wide: %q", w, ansi.Strip(l))
			}
			cells += strings.Count(ansi.Strip(l), cellUsed) + strings.Count(ansi.Strip(l), cellFree) + strings.Count(ansi.Strip(l), cellBuffer)
		}
		// Grid plus one glyph per legend row (4 used, buffer, free).
		gridCells := 100
		if w >= 70 {
			gridCells = 200
		}
		if cells != gridCells+6 {
			t.Errorf("width %d: %d cells", w, cells)
		}
	}
}

func TestUsagePanel(t *testing.T) {
	h := newHarness(t)
	h.eng.supports[proto.SubGetUsage] = true
	h.eng.reply[proto.SubGetUsage] = json.RawMessage(`{
		"session":{"total_cost_usd":0.42,"total_api_duration_ms":63000,"total_duration_ms":720000,"total_lines_added":120,"total_lines_removed":30,
			"model_usage":{"claude-opus-5-5":{"inputTokens":1200,"outputTokens":800,"cacheReadInputTokens":50000,"cacheCreationInputTokens":3000,"costUSD":0.40},
			               "claude-haiku-4-5":{"inputTokens":90000,"outputTokens":100,"costUSD":0.02}}},
		"subscription_type":"max","rate_limits_available":true,
		"rate_limits":{"five_hour":{"utilization":80,"resets_at":"2026-01-02T18:00:00Z"},"seven_day":{"utilization":0.25,"resets_at":null},
			"model_scoped":[{"display_name":"Fable","utilization":12,"resets_at":null}]},
		"behaviors":{"day":{"request_count":40,"session_count":3,"skills":[{"name":"review","pct":60}]},"week":{"request_count":300,"session_count":20}}}`)
	h.command("usage", "")
	if !slices.Equal(h.ctx.Opened, []string{DialogUsage}) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
	cost, _ := h.r.Command("usage")
	if !slices.Equal(cost.Aliases, []string{"cost", "stats"}) {
		t.Fatalf("aliases = %v", cost.Aliases)
	}
	p := h.openDialog(DialogUsage, nil).(*usagePanel)
	v := ansi.Strip(p.View(h.ctx, ext.Area{Width: 140}).Text)
	for _, want := range []string{
		"Cost $0.42", "API time 1m 3s", "session 12m 0s", "+120 −30 lines",
		"Current session (5h)", "80% used", "This week, all models", " 25% used", "This week, Fable",
		"Last 24 hours", "40 requests across 3 sessions", "Skills: review 60%", "sessions on this machine",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("panel lacks %q:\n%s", want, v)
		}
	}
	// By cost the Opus row comes first; t sorts by tokens (Haiku has more).
	if strings.Index(v, "claude-opus") > strings.Index(v, "claude-haiku") {
		t.Fatal("not sorted by cost")
	}
	h.key(tea.KeyPressMsg{Code: 't', Text: "t"})
	v = ansi.Strip(p.View(h.ctx, ext.Area{Width: 140}).Text)
	if strings.Index(v, "claude-haiku") > strings.Index(v, "claude-opus") {
		t.Fatal("t did not sort by tokens")
	}
	h.key(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if v = ansi.Strip(p.View(h.ctx, ext.Area{Width: 140}).Text); !strings.Contains(v, "Last 7 days") || !strings.Contains(v, "300 requests") {
		t.Fatalf("week:\n%s", v)
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !slices.Equal(h.ctx.Closed, []string{DialogUsage}) {
		t.Fatalf("closed = %v", h.ctx.Closed)
	}

	// Without get_usage: the tracker's numbers.
	h2 := newHarness(t)
	h2.run(ext.Msg(event(&proto.Result{TotalCostUSD: 1.5, DurationAPIMS: 2000})))
	p2 := h2.openDialog(DialogUsage, nil).(*usagePanel)
	if v := ansi.Strip(p2.View(h2.ctx, ext.Area{Width: 100}).Text); !strings.Contains(v, "Cost $1.50") || strings.Contains(v, "Plan usage") {
		t.Fatalf("fallback:\n%s", v)
	}
}
