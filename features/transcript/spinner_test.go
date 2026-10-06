package transcript

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// spinnerView renders the spinner inline, without the blank row above it.
func (g *rig) spinnerView() string {
	v := render.Strip(g.f.spinner.View(g.c, ext.Area{Width: 100, MaxHeight: 3, Mode: ext.Inline}).Text)
	if v != "" && !strings.HasPrefix(v, "\n") {
		g.t.Fatalf("no blank row above the spinner: %q", v)
	}
	return strings.TrimPrefix(v, "\n")
}

func TestSpinnerLifecycle(t *testing.T) {
	g := newRig(t, 101)
	if g.spinnerView() != "" {
		t.Fatal("spinner shown while idle")
	}
	cmd := g.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: decodeLines(t, `{"type":"system","subtype":"session_state_changed","state":"running"}`)[0]})
	if cmd == nil {
		t.Fatal("no tick scheduled when the turn started")
	}
	v := g.spinnerView()
	if !strings.Contains(v, g.f.spinner.verb+"…") || !strings.Contains(v, "esc to interrupt") || !strings.Contains(v, "0s") {
		t.Fatalf("spinner = %q", v)
	}
	// Ticks advance the animation and keep it going.
	if next := g.f.spinner.Update(g.c, spinTick{g.f.spinner.gen}); next == nil || g.f.spinner.frame != 1 {
		t.Fatalf("tick: frame %d, next %v", g.f.spinner.frame, next)
	}
	if g.f.spinner.Update(g.c, spinTick{g.f.spinner.gen - 1}) != nil {
		t.Fatal("stale tick kept animating")
	}
	// Elapsed time and streamed tokens.
	g.c.ClockV.Advance(65 * time.Second)
	g.send(streamTwoParagraphs)
	if v := g.spinnerView(); !strings.Contains(v, "1m 5s") || !strings.Contains(v, "↓ 6 tokens") {
		t.Fatalf("spinner = %q", v)
	}
	// Like claude, a tip is picked when a turn ends: none in the first turn.
	if v := g.spinnerView(); strings.Contains(v, "Tip: ") {
		t.Fatalf("tip in the first turn: %q", v)
	}
	g.send(`{"type":"result","subtype":"success","uuid":"r","duration_ms":65000,"is_error":false,"num_turns":1,"total_cost_usd":0}`)
	if v := g.spinnerView(); v != "" {
		t.Fatalf("spinner after result = %q", v)
	}
	// The next turn shows one from its start, and keeps it.
	g.send(`{"type":"system","subtype":"session_state_changed","state":"running"}`)
	tip := g.spinnerView()
	if !strings.Contains(tip, "Tip: ") {
		t.Fatalf("no tip in the second turn: %q", tip)
	}
	g.c.ClockV.Advance(40 * time.Second)
	if v := g.spinnerView(); v[strings.Index(v, "Tip: "):] != tip[strings.Index(tip, "Tip: "):] {
		t.Fatalf("tip changed during the turn: %q → %q", tip, v)
	}
	// The fullscreen layout has no blank row above the spinner (its
	// transcript view ends with one).
	if v := g.f.spinner.View(g.c, ext.Area{Width: 100, MaxHeight: 3, Mode: ext.Fullscreen}).Text; strings.HasPrefix(v, "\n") {
		t.Fatalf("fullscreen spinner = %q", v)
	}
}

func TestSpinnerCompactingAndWaiting(t *testing.T) {
	g := newRig(t, 101)
	g.send(`{"type":"system","subtype":"status","status":"compacting"}`)
	if v := g.spinnerView(); !strings.Contains(v, "Compacting conversation…") {
		t.Fatalf("spinner = %q", v)
	}
	g.send(`{"type":"system","subtype":"session_state_changed","state":"requires_action"}`)
	if v := g.spinnerView(); v != "" {
		t.Fatalf("spinner shown under a dialog: %q", v)
	}
}

func TestSpinnerSettings(t *testing.T) {
	g := newRig(t, 101)
	g.c.SettingsV = exttest.NewSettings(map[string]any{
		"spinnerVerbs":         map[string]any{"mode": "replace", "verbs": []any{"Frobnicating"}},
		"spinnerTipsEnabled":   false,
		"prefersReducedMotion": true,
	})
	g.deliver(ext.SettingsMsg{Changed: []string{"spinnerVerbs"}})
	g.send(`{"type":"system","subtype":"session_state_changed","state":"running"}`)
	g.c.ClockV.Advance(30 * time.Second)
	v := g.spinnerView()
	if !strings.HasPrefix(v, "✻ Frobnicating…") || strings.Contains(v, "Tip:") {
		t.Fatalf("spinner = %q", v)
	}
	g.f.spinner.Update(g.c, spinTick{g.f.spinner.gen})
	if v2 := g.spinnerView(); !strings.HasPrefix(v2, "✻ ") {
		t.Fatalf("reduced motion still animates: %q", v2)
	}
	// Append mode keeps the built-in verbs.
	if got := spinnerVerbs(map[string]any{"verbs": []any{"Extra"}}); len(got) != len(builtinVerbs)+1 {
		t.Fatalf("append gave %d verbs", len(got))
	}
}

func TestSpinnerTipsOverride(t *testing.T) {
	g := newRig(t, 101)
	g.c.SettingsV = exttest.NewSettings(map[string]any{
		"spinnerTipsOverride": map[string]any{"tips": []any{"Only tip"}},
	})
	g.deliver(ext.SettingsMsg{})
	g.send(`{"type":"system","subtype":"session_state_changed","state":"running"}
{"type":"result","subtype":"success","uuid":"r","duration_ms":1000,"is_error":false,"num_turns":1,"total_cost_usd":0}
{"type":"system","subtype":"session_state_changed","state":"running"}`)
	if v := g.spinnerView(); !strings.Contains(v, "Tip: Only tip") {
		t.Fatalf("spinner = %q", v)
	}
}

func TestRateLimitAndNotices(t *testing.T) {
	g := newRig(t, 81)
	g.send(`
{"type":"rate_limit_event","uuid":"rl1","rate_limit_info":{"status":"allowed_warning","utilization":0.85}}
{"type":"rate_limit_event","uuid":"rl2","rate_limit_info":{"status":"allowed_warning","utilization":0.86}}
{"type":"system","subtype":"notification","key":"k","text":"MCP server reconnected","timeout_ms":3000}
`)
	if got := join(g.printed()); !strings.Contains(got, "Approaching the usage limit (85% used)") || strings.Count(got, "Approaching") != 1 {
		t.Fatalf("printed = %q", got)
	}
	if len(g.c.Notices) != 1 || g.c.Notices[0].Text != "MCP server reconnected" || g.c.Notices[0].Timeout != 3*time.Second {
		t.Fatalf("notices = %+v", g.c.Notices)
	}
}

var _ tea.Msg = spinTick{}
