package sessions

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadPlain(t *testing.T) {
	tr := loadFixture(t, sidPlain)
	if tr.Malformed != 0 || tr.Partial {
		t.Fatalf("malformed=%d partial=%v", tr.Malformed, tr.Partial)
	}
	if len(tr.Records) != 16 || len(tr.Entries) != 8 {
		t.Fatalf("records=%d entries=%d", len(tr.Records), len(tr.Entries))
	}
	kinds := map[string]int{}
	for _, r := range tr.Records {
		kinds[r.Kind()]++
		if r.Pos().Line == 0 || !json.Valid(r.Raw()) {
			t.Fatalf("bad record %T pos=%+v", r, r.Pos())
		}
	}
	if kinds[KindAssistant] != 3 || kinds[KindQueueOperation] != 2 || kinds["atis-latch"] != 1 {
		t.Fatalf("kinds = %v", kinds)
	}
	if _, ok := tr.Records[len(tr.Records)-1].(*Unknown); !ok {
		t.Fatalf("atis-latch decoded as %T", tr.Records[len(tr.Records)-1])
	}

	m := tr.Meta
	if m.SessionID != sidPlain || m.AITitle != "Explain the build system" || m.LastPrompt != "And the tests?" ||
		m.PermissionMode != "default" || m.Mode != "normal" || m.LeafUUID != u("1s", 2) {
		t.Fatalf("meta = %+v", m)
	}
	c := m.Cost
	if c == nil || c.TotalCostUSD != 0.0123 || c.APIDuration != 2500*time.Millisecond ||
		c.Duration != 61*time.Second || c.LinesAdded != 3 || c.ModelUsage["claude-opus-5-5"].InputTokens != 300 ||
		c.StartTime.UnixMilli() != 1788256800000 {
		t.Fatalf("cost = %+v", c)
	}

	user := tr.Entries[1]
	if user.Kind() != KindUser || !user.IsPrompt() || user.Message.Content.Text() != "Explain the build" ||
		!user.Message.Content.WasString || user.Cwd != "/work/demo" || user.GitBranch != "main" ||
		user.Entrypoint != "cli" || user.PromptID != "p1" || user.ParentUUID != u("1a", 0) {
		t.Fatalf("user entry = %+v", user)
	}
	if !user.Timestamp.Equal(time.Date(2026, 9, 1, 10, 0, 2, 0, time.UTC)) {
		t.Fatalf("timestamp = %v", user.Timestamp)
	}
	if tr.Entries[3].ToolUseResult != nil || tr.Entries[3].Attachment != nil {
		t.Fatal("absent fields must stay nil")
	}
	think := tr.Entries[2]
	if b := think.Message.Content.Blocks[0]; b.Type != "thinking" || b.Thinking != "Look at the Makefile." ||
		think.Message.Usage.CacheReadInputTokens != 50 || think.RequestID != "req_1" {
		t.Fatalf("thinking entry = %+v", think.Message)
	}
	att := tr.Entries[0]
	if att.Attachment == nil || att.Attachment.Type != "environment" || att.ParentUUID != "" {
		t.Fatalf("attachment = %+v", att.Attachment)
	}
	sys := tr.Entries[4]
	if sys.Subtype != "turn_duration" || sys.Duration != 1234*time.Millisecond || sys.MessageCount != 3 {
		t.Fatalf("system = %+v", sys)
	}
	q := tr.Records[1].(*QueueOperation)
	if q.Operation != "enqueue" || q.Content != "Explain the build" || q.Timestamp.IsZero() {
		t.Fatalf("queue op = %+v", q)
	}
}

func TestReadToolsAndResults(t *testing.T) {
	tr := loadFixture(t, sidTools)
	var toolUse, result *Entry
	for _, e := range tr.Entries {
		if e.Kind() == KindAssistant && e.Message.Content.Has("tool_use") && toolUse == nil {
			toolUse = e
		}
		if e.IsToolResult() && result == nil {
			result = e
		}
	}
	b := toolUse.Message.Content.Blocks[0]
	var in struct{ Command string }
	if b.Name != "Bash" || b.ID != "toolu_bash1" || json.Unmarshal(b.Input, &in) != nil || in.Command != "ls" {
		t.Fatalf("tool_use = %+v", b)
	}
	rb := result.Message.Content.Blocks[0]
	if rb.ToolUseID != "toolu_bash1" || rb.ResultText() != "a.go\nb.go" || result.IsPrompt() ||
		result.SourceToolAssistantUUID != toolUse.UUID || !bytes.Contains(result.ToolUseResult, []byte(`"stdout"`)) {
		t.Fatalf("tool_result = %+v", result)
	}
	// Block-form tool_result content.
	agentRes := tr.Entries[4].Message.Content.Blocks[0]
	if agentRes.ResultText() != "Both files define main." {
		t.Fatalf("agent result = %q", agentRes.ResultText())
	}
	pr, ok := tr.Meta.PR, tr.Meta.PR != nil
	if !ok || pr.PRNumber != 42 || pr.PRRepository != "example/demo" {
		t.Fatalf("pr = %+v", pr)
	}
}

func TestReadMessy(t *testing.T) {
	tr := loadFixture(t, sidMessy)
	if tr.Malformed != 2 {
		t.Fatalf("malformed = %d, want 2", tr.Malformed)
	}
	if !tr.Partial {
		t.Fatal("partial final line not detected")
	}
	var unknown []string
	for _, r := range tr.Records {
		if u, ok := r.(*Unknown); ok {
			unknown = append(unknown, u.Kind())
		}
	}
	if len(unknown) != 1 || unknown[0] != "future-record" || KnownKinds["future-record"] {
		t.Fatalf("unknown = %v", unknown)
	}
	// NUL-prefixed first entry decodes; raw has the NULs stripped.
	first := tr.Entries[0]
	if first.UUID != u("5u", 1) || first.Raw()[0] != '{' {
		t.Fatalf("first = %+v", first)
	}
	// A field of the wrong type is dropped, not the record.
	var sys *Entry
	for _, e := range tr.Entries {
		if e.Kind() == KindSystem {
			sys = e
		}
	}
	if sys == nil || sys.Subtype != "informational" || sys.Content != "Heads up" || sys.Duration != 0 {
		t.Fatalf("system with bad field = %+v", sys)
	}
	if tr.Meta.Summary != "Legacy summary title" || tr.Meta.CustomTitle != "My renamed session" {
		t.Fatalf("meta = %+v", tr.Meta)
	}
	if got := tr.Title(); got != "My renamed session" {
		t.Fatalf("title = %q", got)
	}
	if got := tr.FirstPrompt(); got != "! git status" {
		t.Fatalf("first prompt = %q", got)
	}
	// Unknown content blocks keep their type.
	last := tr.Entries[len(tr.Entries)-1]
	if bl := last.Message.Content.Blocks; len(bl) != 2 || bl[1].Type != "server_tool_use" {
		t.Fatalf("blocks = %+v", bl)
	}
}

func TestReaderPartialAndResume(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, sidPlain+".jsonl")
	l1 := `{"type":"ai-title","aiTitle":"one","sessionId":"s"}` + "\n"
	half := `{"type":"ai-title","aiTi`
	if err := os.WriteFile(p, []byte(l1+half), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, off, err := ReadFrom(p, 0)
	if err != nil || len(recs) != 1 || off != int64(len(l1)) {
		t.Fatalf("first read: %d recs off=%d err=%v", len(recs), off, err)
	}
	// The writer finishes the line and adds another.
	rest := `tle":"two","sessionId":"s"}` + "\n" + `{"type":"tag","tag":"x"}` + "\n"
	if err := os.WriteFile(p, []byte(l1+half+rest), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, off2, err := ReadFrom(p, off)
	if err != nil || len(recs) != 2 || off2 != int64(len(l1+half+rest)) {
		t.Fatalf("second read: %d recs off=%d err=%v", len(recs), off2, err)
	}
	if recs[0].(*AITitle).Title != "two" || recs[0].Pos().Offset != off || recs[1].Kind() != KindTag {
		t.Fatalf("resumed records = %+v %+v", recs[0], recs[1])
	}
}

func TestReaderLongLine(t *testing.T) {
	big := strings.Repeat("x", 5<<20)
	line := `{"parentUuid":null,"type":"user","uuid":"u1","message":{"role":"user","content":"` + big + `"}}`
	rd := NewReader(strings.NewReader(line + "\n" + `{"type":"tag","tag":"t"}` + "\n"))
	r, err := rd.Next()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(r.(*Entry).Message.Content.Text()); got != len(big) {
		t.Fatalf("text len = %d", got)
	}
	if r, err = rd.Next(); err != nil || r.Kind() != KindTag {
		t.Fatalf("second = %v, %v", r, err)
	}
	if _, err = rd.Next(); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeBlankAndCRLF(t *testing.T) {
	recs, rd, err := ReadAll(strings.NewReader("\n\r\n{\"type\":\"tag\",\"tag\":\"a\"}\r\n\n"))
	if err != nil || len(recs) != 1 || rd.Malformed() != 0 || recs[0].Pos().Line != 3 || recs[0].Pos().Offset != 3 {
		t.Fatalf("recs=%v malformed=%d err=%v", recs, rd.Malformed(), err)
	}
}

func TestContentRoundTrip(t *testing.T) {
	for _, in := range []string{`"hello"`, `[{"type":"text","text":"a"},{"type":"image","source":{"type":"base64"}}]`} {
		var c Content
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		_ = json.Unmarshal([]byte(in), &a)
		_ = json.Unmarshal(out, &b)
		if !jsonEqual(a, b) {
			t.Fatalf("round trip %s -> %s", in, out)
		}
	}
	var c Content
	if err := json.Unmarshal([]byte(`5`), &c); err != nil || len(c.Blocks) != 0 {
		t.Fatalf("number content: %v %v", c, err)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestPromptPreview(t *testing.T) {
	mk := func(content string, meta bool) *Entry {
		e := &Entry{rec: rec{kind: KindUser}, IsMeta: meta, Message: &Message{}}
		_ = json.Unmarshal([]byte(content), &e.Message.Content)
		return e
	}
	cases := []struct {
		content   string
		meta      bool
		want, cmd string
		ok        bool
	}{
		{`"fix the bug\nplease"`, false, "fix the bug please", "", true},
		{`"<command-name>/review</command-name><command-args>1</command-args>"`, false, "", "/review", false},
		{`"<bash-input>ls -la</bash-input>"`, false, "! ls -la", "", true},
		{`"[Request interrupted by user for tool use]"`, false, "", "", false},
		{`"<system-reminder>x</system-reminder>"`, false, "", "", false},
		{`"real"`, true, "", "", false},
		{`[{"type":"image","source":{}},{"type":"text","text":"what is this"}]`, false, "what is this", "", true},
		{`"` + strings.Repeat("é", 250) + `"`, false, strings.Repeat("é", 200) + "…", "", true},
	}
	for _, c := range cases {
		got, cmd, ok := PromptPreview(mk(c.content, c.meta))
		if got != c.want || cmd != c.cmd || ok != c.ok {
			t.Errorf("PromptPreview(%.30s) = %q, %q, %v; want %q, %q, %v", c.content, got, cmd, ok, c.want, c.cmd, c.ok)
		}
	}
}
