package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// SE-04: a result the engine cut to a preview gets its saved output back, but only
// from the session's own tool-results directory.
func TestPersistedToolOutput(t *testing.T) {
	dir := t.TempDir()
	results := filepath.Join(dir, "tool-results")
	_ = os.MkdirAll(results, 0o755)
	full := filepath.Join(results, "abc.txt")
	_ = os.WriteFile(full, []byte("line 1\nline 2\nall of it\n"), 0o644)
	outside := filepath.Join(dir, "elsewhere.txt")
	_ = os.WriteFile(outside, []byte("secret"), 0o644)

	preview := func(p string) string {
		b, _ := json.Marshal("<persisted-output>\nOutput too large (44.4KB). Full output saved to: " + p + "\n\nPreview (first 2KB):\nline 1\n</persisted-output>")
		return string(b)
	}
	env := `"cwd":"/w","sessionId":"s","timestamp":"2026-09-01T10:00:00.000Z"`
	lines := []string{
		`{"parentUuid":null,"isSidechain":false,"type":"assistant","message":{"id":"m","role":"assistant","model":"x","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}},{"type":"tool_use","id":"t2","name":"Bash","input":{}}]},"uuid":"a",` + env + `}`,
		`{"parentUuid":"a","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":` + preview(full) + `},{"type":"tool_result","tool_use_id":"t2","content":` + preview(outside) + `}]},"uuid":"r",` + env + `}`,
	}
	tr, _ := sessions.Parse(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	items := Normalize(tr, NormalizeOptions{EngineID: "main", ToolResultsDir: results})
	if got := items[0].Result.Content.PlainText(); got != "line 1\nline 2\nall of it\n" {
		t.Fatalf("t1 = %q", got)
	}
	if got := items[1].Result.Content.PlainText(); !strings.Contains(got, "<persisted-output>") {
		t.Fatalf("t2 read a file outside tool-results: %q", got)
	}
	// Without the option nothing is read.
	items = Normalize(tr, NormalizeOptions{EngineID: "main"})
	if !strings.Contains(items[0].Result.Content.PlainText(), "Preview") {
		t.Fatal("read without ToolResultsDir")
	}
}

// SE-18: mantle titles its own sessions once, after the first turn, quietly.
func TestAutoTitle(t *testing.T) {
	h := newHarness(t)
	h.eng.supports[proto.SubGenerateSessionTitle] = true
	h.eng.reply[proto.SubGenerateSessionTitle] = json.RawMessage(`{"title":"Fix the parser"}`)
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p", "the parser drops quotes")}}
	h.run(ext.Msg(event(&proto.Result{})))
	if h.ctx.SessionValue.Title != "Fix the parser" || len(h.ctx.Notices) != 0 {
		t.Fatalf("title %q, notices %+v", h.ctx.SessionValue.Title, h.ctx.Notices)
	}
	req := h.eng.controls[0].req.(proto.GenerateSessionTitleRequest)
	if !req.Persist || !strings.Contains(req.Description, "parser drops quotes") {
		t.Fatalf("req = %+v", req)
	}
	h.run(ext.Msg(event(&proto.Result{})))
	if len(h.eng.controls) != 1 {
		t.Fatal("asked for a title twice")
	}
	// A session that already has a title is left alone.
	h2 := newHarness(t)
	h2.eng.supports[proto.SubGenerateSessionTitle] = true
	h2.ctx.SessionValue.Title = "Named"
	h2.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{promptItem("p", "x")}}
	h2.run(ext.Msg(event(&proto.Result{})))
	if len(h2.eng.controls) != 0 {
		t.Fatal("retitled a named session")
	}
}

// SE-21: /subtask opens in Claude Code.
func TestSubtaskHandsOff(t *testing.T) {
	h := newHarness(t)
	h.command("subtask", "audit the tests")
	if !slices.Equal(h.ctx.Opened, []string{DialogHandoff}) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
	d, _ := h.r.Dialogs[DialogHandoff](h.ctx, []string{"/subtask audit the tests"})
	if !strings.Contains(ansi.Strip(d.View(h.ctx, ext.Area{Width: 80}).Text), "/subtask audit the tests") {
		t.Fatal("hand-off dialog lacks the command")
	}
}

// SE-14: the picker groups sessions by date.
func TestPickerDateGroups(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-time.Hour), "Today"},
		{now.Add(-30 * time.Hour), "Yesterday"},
		{now.Add(-4 * 24 * time.Hour), "This week"},
		{now.Add(-20 * 24 * time.Hour), "This month"},
		{now.Add(-90 * 24 * time.Hour), "Older"},
	} {
		if got := dateGroup(now, c.t); got != c.want {
			t.Errorf("dateGroup(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	h := newHarness(t)
	p := &picker{f: h.f, list: storySessions(h.ctx.Clock().Now())}
	p.refilter()
	v := ansi.Strip(p.View(h.ctx, ext.Area{Width: 100, MaxHeight: 24}).Text)
	iToday, iYesterday, iMonth := strings.Index(v, "Today"), strings.Index(v, "Yesterday"), strings.Index(v, "This month")
	if iToday < 0 || iYesterday < iToday || iMonth < iYesterday {
		t.Fatalf("groups:\n%s", v)
	}
	// Selecting the last session keeps it visible in a short window.
	p.sel = 2
	v = ansi.Strip(p.View(h.ctx, ext.Area{Width: 100, MaxHeight: 9}).Text)
	if !strings.Contains(v, "My renamed session") {
		t.Fatalf("selection scrolled out:\n%s", v)
	}
}
