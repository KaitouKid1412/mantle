package sessions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func assistantWithUsage(model string, input, cacheRead int64) *proto.Assistant {
	return &proto.Assistant{Message: proto.Message{Model: model, Usage: &proto.Usage{InputTokens: input, CacheReadInputTokens: cacheRead, OutputTokens: 100}}}
}

func event(e proto.Event) ext.EngineEventMsg {
	return ext.EngineEventMsg{EngineID: ext.MainEngine, Event: e}
}

func contextLine(h *harness) string {
	c := &contextWarning{f: h.f}
	return c.View(h.ctx, ext.Area{Width: 60}).Text
}

func TestContextCountdownEstimate(t *testing.T) {
	h := newHarness(t)
	h.run(ext.Msg(event(assistantWithUsage("opus", 1000, 50_000))))
	if contextLine(h) != "" {
		t.Fatal("countdown shown with plenty of context")
	}
	// 170k of a 200k window, minus the 13k buffer: 9% left, shown.
	h.run(ext.Msg(event(assistantWithUsage("opus", 20_000, 149_900))))
	h.run(ext.Msg(event(&proto.Result{TotalCostUSD: 1.25, DurationAPIMS: 900,
		ModelUsage: map[string]proto.ModelUsage{"opus": {ContextWindow: 200_000}}})))
	line := contextLine(h)
	if !strings.Contains(ansi.Strip(line), "9% left until auto-compact") || ansi.StringWidth(line) != 60 {
		t.Fatalf("line = %q (width %d)", ansi.Strip(line), ansi.StringWidth(line))
	}
	u := h.f.usage(ext.MainEngine)
	if u.costUSD != 1.25 || u.turns != 1 || u.window != 200_000 {
		t.Fatalf("usage = %+v", u)
	}
	// Auto-compact off: a stronger warning against the whole window.
	h.ctx.SettingsV = exttest.NewSettings(map[string]any{"autoCompactEnabled": false})
	if l := ansi.Strip(contextLine(h)); !strings.Contains(l, "Context almost full (15% left)") {
		t.Fatalf("auto-compact off: %q", l)
	}
	// /clear resets.
	h.run(ext.Msg(event(&proto.ConversationReset{NewConversationID: "n"})))
	if contextLine(h) != "" || h.f.usage(ext.MainEngine).costUSD != 0 {
		t.Fatal("reset did not clear usage")
	}
}

// With get_context_usage, the engine's own numbers decide (buffer category included).
func TestContextCountdownMeasured(t *testing.T) {
	h := newHarness(t)
	h.eng.supports[proto.SubGetContextUsage] = true
	reply, _ := json.Marshal(proto.ContextUsage{
		TotalTokens: 150_000, MaxTokens: 200_000, Percentage: 75,
		Categories: []proto.ContextCategory{
			{Name: "Messages", Tokens: 150_000},
			{Name: "Autocompact buffer", Tokens: 33_000, Kind: "buffer"},
			{Name: "Free space", Tokens: 17_000, Kind: "free"},
		},
	})
	h.eng.reply[proto.SubGetContextUsage] = reply
	h.run(ext.Msg(event(assistantWithUsage("opus", 10, 10))))
	h.run(ext.Msg(event(&proto.Result{})))
	if len(h.eng.controls) != 1 || h.eng.controls[0].subtype != proto.SubGetContextUsage {
		t.Fatalf("controls = %+v", h.eng.controls)
	}
	lv := h.f.usage(ext.MainEngine).level(true)
	// limit 167k, used 150k: 10% left, within 20k of the limit.
	if !lv.measured || !lv.show || lv.pctLeft != 10 || lv.limit != 167_000 {
		t.Fatalf("level = %+v", lv)
	}
	// A compact boundary drops the measurement until the next one.
	h.run(ext.Msg(event(&proto.CompactBoundary{})))
	if contextLine(h) != "" {
		t.Fatal("countdown after compaction")
	}
}

func TestRateLimitTracking(t *testing.T) {
	h := newHarness(t)
	h.run(ext.Msg(event(&proto.RateLimitEvent{RateLimitInfo: proto.RateLimitInfo{Status: "allowed_warning", RateLimitType: "five_hour", Utilization: 0.8}})))
	if r := h.f.usage(ext.MainEngine).rate["five_hour"]; r.Utilization != 0.8 {
		t.Fatalf("rate = %+v", r)
	}
}
