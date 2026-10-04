package sessions

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestResumeSummaryOnSwitch(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	h.f.now = func() time.Time { return now }
	meta := sessions.SessionMeta{ID: sidPlain, LastActive: now.Add(-3 * time.Hour), ContextTokens: 150_000}

	h.run(h.f.switchTo(h.ctx, meta, switchOpts{}))
	if !slices.Equal(h.ctx.Opened, []string{DialogResumeSummary}) || len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatalf("opened %v, out %v", h.ctx.Opened, h.out)
	}
	d := h.openDialog(DialogResumeSummary, summaryArgs{meta: meta, idle: 3 * time.Hour, tokens: 150_000})
	if v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 100}).Text); !strings.Contains(v, "3 hours ago") || !strings.Contains(v, "150k tokens") {
		t.Fatalf("dialog:\n%s", v)
	}
	h.key(tea.KeyPressMsg{Code: '1', Text: "1"}) // compact first
	if s := find[ext.EngineStartMsg](h); len(s) != 1 || s[0].Opts.Resume != sidPlain {
		t.Fatalf("start = %+v", s)
	}
	if !h.f.compactOnAttach {
		t.Fatal("compaction not scheduled")
	}
	h.run(ext.Msg(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: h.eng}))
	if !slices.Equal(h.eng.sent, []string{"/compact"}) || h.f.compactOnAttach {
		t.Fatalf("sent = %q", h.eng.sent)
	}

	// "Don't ask again" resumes in full and remembers.
	h2 := newHarness(t)
	h2.f.now = func() time.Time { return now }
	h2.openDialog(DialogResumeSummary, summaryArgs{meta: meta, idle: 3 * time.Hour, tokens: 150_000})
	h2.key(tea.KeyPressMsg{Code: '3', Text: "3"})
	if len(find[ext.EngineStartMsg](h2)) != 1 || h2.f.compactOnAttach {
		t.Fatal("full resume")
	}
	h2.reset()
	h2.run(h2.f.switchTo(h2.ctx, meta, switchOpts{}))
	if len(find[ext.EngineStartMsg](h2)) != 1 {
		t.Fatal("asked again after don't-ask-again")
	}

	// Small or recent conversations resume straight away.
	h3 := newHarness(t)
	h3.f.now = func() time.Time { return now }
	h3.run(h3.f.switchTo(h3.ctx, sessions.SessionMeta{ID: sidPlain, LastActive: now.Add(-10 * time.Minute), ContextTokens: 150_000}, switchOpts{}))
	if len(h3.ctx.Opened) != 0 || len(find[ext.EngineStartMsg](h3)) != 1 {
		t.Fatalf("recent: opened %v", h3.ctx.Opened)
	}
}

func TestResumeSummaryAtStartup(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	h.f.now = func() time.Time { return now }
	id := "13131313-1313-4313-8313-131313131313"
	env := `"cwd":"/work/demo","sessionId":"` + id + `","timestamp":"2026-09-01T10:00:00.000Z"`
	lines := `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"big"},"uuid":"u1",` + env + `}` + "\n" +
		`{"parentUuid":"u1","isSidechain":false,"type":"assistant","message":{"id":"m","role":"assistant","model":"opus","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":5000,"cache_read_input_tokens":120000,"output_tokens":100}},"uuid":"a1",` + env + `}` + "\n"
	if err := os.WriteFile(sessions.SessionFile(h.f.layout.ProjectDir("/work/demo"), id), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	h.ctx.SessionValue.SessionID = id
	h.run(h.r.Starts[0].Value.(func(ext.Ctx) tea.Cmd)(h.ctx))
	if !slices.Contains(h.ctx.Opened, DialogResumeSummary) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
	h.openDialog(DialogResumeSummary, summaryArgs{startup: true, idle: 26 * time.Hour, tokens: 125_100})
	h.key(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !slices.Equal(h.eng.sent, []string{"/compact"}) {
		t.Fatalf("sent = %q", h.eng.sent)
	}
	if len(find[ext.EngineStartMsg](h)) != 0 {
		t.Fatal("startup offer restarted the engine")
	}
}
