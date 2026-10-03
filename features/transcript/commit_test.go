package transcript

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

// rig is a transcript feature wired to a fake Ctx.
type rig struct {
	t *testing.T
	f *Feature
	r *exttest.Registrar
	c *exttest.Ctx
}

func newRig(t *testing.T, width int) *rig {
	t.Helper()
	f, r := setupFeature(t)
	c := exttest.NewCtx()
	c.W, c.H = width, 30
	c.Renderers = r.Renderers
	c.TranscriptV = f.store
	for _, s := range r.Starts {
		s.Value.(func(ext.Ctx) tea.Cmd)(c)
	}
	return &rig{t: t, f: f, r: r, c: c}
}

// deliver calls the subscription for a message type, as the host would.
func (g *rig) deliver(msg tea.Msg) tea.Cmd {
	g.t.Helper()
	var cmds []tea.Cmd
	for _, s := range g.r.Subs {
		if sameType(s.Sample, msg) {
			cmds = append(cmds, s.Fn(g.c, msg))
		}
	}
	return tea.Batch(cmds...)
}

func sameType(a, b tea.Msg) bool { return reflect.TypeOf(a) == reflect.TypeOf(b) }

// send applies NDJSON engine events.
func (g *rig) send(ndjson string) {
	g.t.Helper()
	for _, ev := range decodeLines(g.t, ndjson) {
		g.deliver(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	}
}

// printed returns everything printed so far, stripped, as lines.
func (g *rig) printed() []string {
	var out []string
	for _, b := range g.c.Printed {
		for _, l := range strings.Split(b, "\n") {
			out = append(out, strings.TrimRight(render.Strip(l), " "))
		}
	}
	return out
}

// live renders the live area, stripped.
func (g *rig) live(height int) []string {
	out := g.f.live.View(g.c, ext.Area{Width: g.c.W, MaxHeight: height, Mode: ext.Inline}).Text
	if out == "" {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		lines = append(lines, strings.TrimRight(render.Strip(l), " "))
	}
	return lines
}

func join(lines []string) string { return strings.Join(lines, "\n") }

const streamTwoParagraphs = `
{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1","content":[]}}}
{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"First paragraph.\n\nSecond"}}}
`

func TestCommitProgressive(t *testing.T) {
	g := newRig(t, 41)
	g.send(`{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"hi"}}`)
	if got := join(g.printed()); got != "\n> hi" {
		t.Fatalf("prompt not committed: %q", got)
	}
	g.send(streamTwoParagraphs)
	if got := join(g.printed()); got != "\n> hi\n\n⏺ First paragraph." {
		t.Fatalf("closed block not committed:\n%s", got)
	}
	if got := join(g.live(10)); got != "\n  Second" {
		t.Fatalf("live = %q", got)
	}
	g.send(`
{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" paragraph."}}}
{"type":"stream_event","event":{"type":"content_block_stop","index":0}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"First paragraph.\n\nSecond paragraph."}]}}
`)
	want := "\n> hi\n\n⏺ First paragraph.\n\n  Second paragraph."
	if got := join(g.printed()); got != want {
		t.Fatalf("after finish:\n%q\nwant\n%q", got, want)
	}
	if g.f.store.Committed() != 2 || len(g.live(10)) != 0 {
		t.Fatalf("committed %d, live %q", g.f.store.Committed(), g.live(10))
	}
	if len(g.f.texts) != 0 {
		t.Fatal("render cache of a committed item was kept")
	}
}

func TestCommitWaitsForRunningTool(t *testing.T) {
	g := newRig(t, 61)
	g.send(`
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 5"}}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m1","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"echo hi"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"hi"}]},"tool_use_result":{"stdout":"hi","stderr":""}}
`)
	if len(g.c.Printed) != 0 {
		t.Fatalf("committed past a running tool: %q", g.printed())
	}
	live := join(g.live(20))
	if !strings.Contains(live, "Bash(sleep 5)") || !strings.Contains(live, "Bash(echo hi)") {
		t.Fatalf("live area = %q", live)
	}
	g.send(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":""}]},"tool_use_result":{"stdout":"","stderr":""}}`)
	got := join(g.printed())
	if !strings.Contains(got, "Bash(sleep 5)") || !strings.Contains(got, "⎿  hi") || strings.Index(got, "sleep 5") > strings.Index(got, "echo hi") {
		t.Fatalf("printed = %q", got)
	}
}

func TestCommitGroupsMCPCalls(t *testing.T) {
	g := newRig(t, 61)
	g.send(`
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"mcp__slack__search","input":{"q":"x"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"assistant","uuid":"a2","message":{"id":"m","content":[{"type":"tool_use","id":"t2","name":"mcp__slack__post","input":{"q":"y"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}
`)
	if len(g.c.Printed) != 0 {
		t.Fatalf("an open MCP run was committed: %q", g.printed())
	}
	g.send(`{"type":"assistant","uuid":"a3","message":{"id":"m","content":[{"type":"text","text":"Done."}]}}`)
	got := join(g.printed())
	if !strings.Contains(got, "⏺ Called slack 2 times") || !strings.Contains(got, "search, post") {
		t.Fatalf("printed = %q", got)
	}
	if strings.Count(got, "(MCP)") != 0 {
		t.Fatalf("grouped calls also printed one by one: %q", got)
	}
}

func TestCommitSingleMCPCallAfterRun(t *testing.T) {
	g := newRig(t, 61)
	g.send(`
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"mcp__slack__search","input":{"q":"x"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":10,"is_error":false,"num_turns":1,"total_cost_usd":0}
`)
	if got := join(g.printed()); !strings.Contains(got, "slack - search (MCP)") {
		t.Fatalf("printed = %q", got)
	}
}

func TestCommitReprintAndReplace(t *testing.T) {
	g := newRig(t, 61)
	g.send(`
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"hi"}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"Old answer."}]}}
`)
	first := join(g.printed())
	g.send(`{"type":"assistant","uuid":"a2","supersedes":["a1"],"message":{"id":"m2","content":[{"type":"text","text":"New answer."}]}}`)
	got := join(g.printed())
	if !strings.HasPrefix(got, first) || !strings.Contains(got[len(first):], "(response replaced)") || !strings.Contains(got[len(first):], "New answer.") {
		t.Fatalf("after supersedes:\n%s", got)
	}
	g.c.Printed = nil
	g.deliver(ext.ScreenClearedMsg{})
	if got := join(g.printed()); got != "\n> hi\n\n⏺ New answer." {
		t.Fatalf("reprint = %q", got)
	}
}

func TestConversationResetReprints(t *testing.T) {
	g := newRig(t, 61)
	g.send(`{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"hi"}}`)
	g.send(`{"type":"conversation_reset","new_conversation_id":"n","trigger":"clear"}`)
	if g.c.Reprints != 1 || len(g.f.store.Items()) != 0 {
		t.Fatalf("reprints %d items %d", g.c.Reprints, len(g.f.store.Items()))
	}
}

func TestHistoryMsg(t *testing.T) {
	g := newRig(t, 61)
	g.send(`{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"live"}}`)
	g.c.Printed = nil
	g.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Reset: true, Items: []*ext.Item{
		{ID: "user:h1", Key: ext.KeyUserPrompt, Data: "from history"},
	}})
	if got := join(g.printed()); got != "\n> from history" || len(g.f.store.Items()) != 1 {
		t.Fatalf("printed %q items %d", got, len(g.f.store.Items()))
	}
	g.deliver(ext.TranscriptHistoryMsg{EngineID: "other", Items: []*ext.Item{{ID: "x", Key: ext.KeyUserPrompt, Data: "no"}}})
	if len(g.f.store.Items()) != 1 {
		t.Fatal("history for another engine was accepted")
	}
}

func TestCommitOnlyInline(t *testing.T) {
	g := newRig(t, 61)
	g.c.LayoutMode = ext.Fullscreen
	g.send(`{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"hi"}}`)
	if len(g.c.Printed) != 0 || g.f.store.Committed() != 0 {
		t.Fatal("fullscreen layout committed to scrollback")
	}
}

// TestCommitSampleSession replays the sample session: every item ends up in
// scrollback, nothing is wider than the print width, and nothing is printed
// twice.
func TestCommitSampleSession(t *testing.T) {
	for _, w := range []int{61, 101, 161} {
		g := newRig(t, w)
		g.send(sampleSession)
		if g.f.store.Committed() != len(g.f.store.Items()) {
			t.Fatalf("width %d: committed %d of %d", w, g.f.store.Committed(), len(g.f.store.Items()))
		}
		for _, b := range g.c.Printed {
			checkLines(t, strings.Split(b, "\n"), w-1)
		}
		got := join(g.printed())
		for _, want := range []string{"> The worker retries", "Read(internal/queue/worker.go)", "Called tracker 2 times", "Churned for 1m 6s", "Conversation compacted"} {
			if strings.Count(got, want) != 1 {
				t.Errorf("width %d: %q appears %d times", w, want, strings.Count(got, want))
			}
		}
	}
}

func TestTimestamps(t *testing.T) {
	g := newRig(t, 61)
	g.c.SettingsV = exttest.NewSettings(map[string]any{
		"showMessageTimestamps": true, "timeFormat": "24h", "timeZone": "UTC",
	})
	g.deliver(ext.SettingsMsg{})
	g.send(`
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"hi"}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":5000,"is_error":false,"num_turns":1,"total_cost_usd":0}
`)
	got := g.printed()
	if len(got) < 2 || !strings.HasPrefix(got[1], "> hi") || !strings.HasSuffix(got[1], "15:04") || render.Width(got[1]) != 60 {
		t.Fatalf("prompt line = %q", got)
	}
	if last := got[len(got)-1]; !strings.HasSuffix(last, "for 5s · done 15:04") {
		t.Fatalf("duration line = %q", last)
	}
}

func TestFitLive(t *testing.T) {
	more := func(n int, running bool) string {
		if running {
			return "+" + itoa(n) + " running"
		}
		return "+" + itoa(n)
	}
	chunks := [][]string{{"", "a1", "a2"}, {"", "b1"}, {"", "c1", "c2", "c3"}}
	running := []bool{true, false, true}
	if got := join(fitLive(chunks, running, 0, more)); got != "\na1\na2\n\nb1\n\nc1\nc2\nc3" {
		t.Fatalf("unbounded: %q", got)
	}
	if got := join(fitLive(chunks, running, 7, more)); got != "+1 running\n\nb1\n\nc1\nc2\nc3" {
		t.Fatalf("height 7: %q", got)
	}
	if got := join(fitLive(chunks, running, 3, more)); got != "+2 running\nc2\nc3" {
		t.Fatalf("height 3 (tail of the newest): %q", got)
	}
	if got := join(fitLive(chunks[2:], running[2:], 2, more)); got != "c2\nc3" {
		t.Fatalf("single tall item: %q", got)
	}
}
