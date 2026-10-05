package sessions

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

const (
	sidPlain   = "11111111-1111-4111-8111-111111111111"
	sidTools   = "22222222-2222-4222-8222-222222222222"
	sidCompact = "33333333-3333-4333-8333-333333333333"
	sidBranch  = "44444444-4444-4444-8444-444444444444"
	sidMessy   = "55555555-5555-4555-8555-555555555555"
	sidOther   = "77777777-7777-4777-8777-777777777777"
)

func fixtureProject() string {
	return filepath.Join("..", "..", "testdata", "fixtures", "06", "claude", "projects", "-work-demo")
}

func u(prefix string, n int) string {
	s := "000000" + itoa(n)
	return prefix + s[len(s)-6:] + "-0000-4000-8000-000000000000"
}

func normalizeFixture(t *testing.T, id string, opts NormalizeOptions) []*ext.Item {
	t.Helper()
	tr, err := sessions.Load(sessions.SessionFile(fixtureProject(), id))
	if err != nil {
		t.Fatal(err)
	}
	if opts.EngineID == "" {
		opts.EngineID = ext.MainEngine
	}
	return Normalize(tr, opts)
}

type itemSig struct {
	ID, Parent string
	Key        ext.ContentKey
	State      ext.ItemState
}

func sigs(items []*ext.Item) []itemSig {
	out := make([]itemSig, len(items))
	for i, it := range items {
		out[i] = itemSig{it.ID, it.ParentID, it.Key, it.State}
	}
	return out
}

func TestNormalizePlain(t *testing.T) {
	items := normalizeFixture(t, sidPlain, NormalizeOptions{})
	want := []itemSig{
		{"user:" + u("1u", 1), "", ext.KeyUserPrompt, ext.Done},
		{"thk:" + u("1b", 1) + ":0", "", ext.KeyAssistantThinking, ext.Done},
		{"txt:" + u("1b", 2) + ":0", "", ext.KeyAssistantText, ext.Done},
		{"result:" + u("1s", 1), "", keyResult, ext.Done},
		{"user:" + u("1u", 2), "", ext.KeyUserPrompt, ext.Done},
		{"txt:" + u("1b", 3) + ":0", "", ext.KeyAssistantText, ext.Done},
		{"result:" + u("1s", 2), "", keyResult, ext.Done},
	}
	if got := sigs(items); !slices.Equal(got, want) {
		t.Fatalf("items:\n got %v\nwant %v", got, want)
	}
	for _, it := range items {
		if it.EngineID != ext.MainEngine || it.Start.IsZero() || it.End.IsZero() {
			t.Fatalf("item %s: engine=%q start=%v end=%v", it.ID, it.EngineID, it.Start, it.End)
		}
	}
	p := items[0].Data.(*proto.User)
	if p.Message.Content.PlainText() != "Explain the build" || !p.IsReplay || p.UUID != u("1u", 1) {
		t.Fatalf("prompt = %+v", p)
	}
	if b := items[2].Data.(*proto.ContentBlock); b.Text != "The build uses make." {
		t.Fatalf("text = %+v", b)
	}
	if r := items[3].Data.(*proto.Result); r.DurationMS != 1234 {
		t.Fatalf("result = %+v", r)
	}
	if !items[0].Start.Equal(time.Date(2026, 9, 1, 10, 0, 2, 0, time.UTC)) {
		t.Fatalf("start = %v", items[0].Start)
	}
}

func TestNormalizeToolsAndSubagent(t *testing.T) {
	sub := sessions.SubagentsDir(fixtureProject(), sidTools)
	items := normalizeFixture(t, sidTools, NormalizeOptions{SubagentsDir: sub})
	want := []itemSig{
		{"user:" + u("2u", 1), "", ext.KeyUserPrompt, ext.Done},
		{"toolu_bash1", "", ext.ToolKey("Bash"), ext.Done},
		{"toolu_agent1", "", ext.ToolKey("Agent"), ext.Done},
		{"toolu_read1", "toolu_agent1", ext.ToolKey("Read"), ext.Done},
		{"txt:" + u("2d", 2) + ":0", "toolu_agent1", ext.KeyAssistantText, ext.Done},
		{"txt:" + u("2b", 3) + ":0", "", ext.KeyAssistantText, ext.Done},
		{"result:" + u("2s", 1), "", keyResult, ext.Done},
	}
	if got := sigs(items); !slices.Equal(got, want) {
		t.Fatalf("items:\n got %v\nwant %v", got, want)
	}
	bash := items[1]
	tu := bash.Data.(*proto.ToolUse)
	if tu.Name != "Bash" || bash.Result == nil || bash.Result.Content.PlainText() != "a.go\nb.go" ||
		!strings.Contains(string(bash.Result.Structured), `"stdout"`) || !bash.End.After(bash.Start) {
		t.Fatalf("bash = %+v result %+v", tu, bash.Result)
	}
	if r := items[3].Result; r == nil || r.ParentToolUseID != "toolu_agent1" || r.Content.PlainText() != "package main" {
		t.Fatalf("nested result = %+v", r)
	}

	// Without the subagents dir, nothing is nested.
	flat := normalizeFixture(t, sidTools, NormalizeOptions{})
	for _, it := range flat {
		if it.ParentID != "" {
			t.Fatalf("unexpected nested item %s", it.ID)
		}
	}
}

func TestNormalizeCompaction(t *testing.T) {
	items := normalizeFixture(t, sidCompact, NormalizeOptions{})
	want := []string{"sys:" + u("3c", 1), "user:" + u("3u", 4), "txt:" + u("3b", 3) + ":0"}
	if got := ids(items); !slices.Equal(got, want) {
		t.Fatalf("items = %v", got)
	}
	cb := items[0].Data.(*proto.CompactBoundary)
	if items[0].Key != ext.KeySystemCompactBoundary || cb.CompactMetadata.PreTokens != 12345 || cb.CompactMetadata.Trigger != "manual" {
		t.Fatalf("boundary = %+v", cb)
	}
	full := normalizeFixture(t, sidCompact, NormalizeOptions{AcrossCompaction: true})
	want = append([]string{"user:" + u("3u", 1), "txt:" + u("3b", 1) + ":0", "user:" + u("3u", 2), "txt:" + u("3b", 2) + ":0"}, want...)
	if got := ids(full); !slices.Equal(got, want) {
		t.Fatalf("full = %v", got)
	}
}

func TestNormalizeBranchAndMessy(t *testing.T) {
	items := normalizeFixture(t, sidBranch, NormalizeOptions{})
	want := []string{"user:" + u("4u", 1), "txt:" + u("4b", 1) + ":0", "user:" + u("4u", 22), "txt:" + u("4b", 22) + ":0"}
	if got := ids(items); !slices.Equal(got, want) {
		t.Fatalf("branch items = %v", got)
	}

	items = normalizeFixture(t, sidMessy, NormalizeOptions{})
	got := sigs(items)
	wantSigs := []itemSig{
		{"user:" + u("5u", 1), "", ext.KeyUserPrompt, ext.Done},
		{"sys:" + u("5u", 2), "", ext.KeySystemLocalCommand, ext.Done},
		{"user:" + u("5u", 3), "", ext.KeyUserBash, ext.Done},
		{"sys:" + u("5u", 4), "", ext.KeySystemInformational, ext.Done},
		{"sys:" + u("5s", 1), "", ext.KeySystemInformational, ext.Done},
		{"txt:" + u("5b", 1) + ":0", "", ext.KeyAssistantText, ext.Done},
		{"srv_1", "", ext.ToolKey("web_search"), ext.Interrupted},
	}
	if !slices.Equal(got, wantSigs) {
		t.Fatalf("messy items:\n got %v\nwant %v", got, wantSigs)
	}
	if txt := items[0].Data.(*proto.User).Message.Content.PlainText(); txt != "/model" {
		t.Fatalf("command shown as %q", txt)
	}
	if out := items[1].Data.(*proto.LocalCommandOutput); out.Content != "Model set" {
		t.Fatalf("local output = %q", out.Content)
	}
}

func ids(items []*ext.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

// Rarer records: queued prompts, hooks, API errors, synthetic output, bash output,
// inline sidechains matched through the tool result's agentId.
func TestNormalizeRareRecords(t *testing.T) {
	env := `"cwd":"/w","sessionId":"s","timestamp":"2026-09-01T10:00:00.000Z"`
	lines := []string{
		`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"<command-name>/review</command-name>\n<command-message>review</command-message>\n<command-args>42</command-args>"},"uuid":"u1",` + env + `}`,
		`{"parentUuid":"u1","isSidechain":false,"type":"attachment","attachment":{"type":"queued_command","prompt":"also check tests","commandMode":"prompt"},"uuid":"q1",` + env + `}`,
		`{"parentUuid":"q1","isSidechain":false,"type":"attachment","attachment":{"type":"queued_command","prompt":"hidden","isMeta":true},"uuid":"q2",` + env + `}`,
		`{"parentUuid":"q2","isSidechain":false,"type":"attachment","attachment":{"type":"hook_blocking_error","hookName":"PreToolUse:Bash","blockingError":{"blockingError":"no rm","command":"x"}},"uuid":"h1",` + env + `}`,
		`{"parentUuid":"h1","isSidechain":false,"type":"attachment","attachment":{"type":"skill_listing","content":"..."},"uuid":"h2",` + env + `}`,
		`{"parentUuid":"h2","isSidechain":false,"type":"assistant","message":{"id":"m1","role":"assistant","model":"<synthetic>","content":[{"type":"text","text":"local out"}]},"uuid":"a1",` + env + `}`,
		`{"parentUuid":"a1","isSidechain":false,"type":"assistant","message":{"id":"m2","role":"assistant","model":"x","content":[{"type":"text","text":"API Error: overloaded"}]},"isApiErrorMessage":true,"error":"overloaded","uuid":"a2",` + env + `}`,
		`{"parentUuid":"a2","isSidechain":false,"type":"user","message":{"role":"user","content":"<bash-input>ls</bash-input>"},"uuid":"b1",` + env + `}`,
		`{"parentUuid":"b1","isSidechain":false,"type":"user","message":{"role":"user","content":"<bash-stdout>a.go</bash-stdout><bash-stderr></bash-stderr>"},"uuid":"b2",` + env + `}`,
		`{"parentUuid":"b2","isSidechain":false,"type":"assistant","message":{"id":"m3","role":"assistant","model":"x","content":[{"type":"tool_use","id":"toolu_t","name":"Task","input":{}}]},"uuid":"a3",` + env + `}`,
		`{"parentUuid":null,"isSidechain":true,"agentId":"inl","type":"user","message":{"role":"user","content":"sub prompt"},"uuid":"s1",` + env + `}`,
		`{"parentUuid":"s1","isSidechain":true,"agentId":"inl","type":"assistant","message":{"id":"m4","role":"assistant","model":"x","content":[{"type":"text","text":"sub answer"}]},"uuid":"s2",` + env + `}`,
		`{"parentUuid":"a3","isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_t","content":"done","is_error":true}]},"toolUseResult":{"agentId":"inl"},"uuid":"r3",` + env + `}`,
		`{"parentUuid":"r3","isSidechain":false,"type":"system","subtype":"api_error","level":"error","uuid":"e1",` + env + `}`,
		`{"parentUuid":"e1","isSidechain":false,"type":"system","subtype":"local_command","content":"<local-command-stdout>Set effort</local-command-stdout>","uuid":"lc",` + env + `}`,
	}
	tr, err := sessions.Parse(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	items := Normalize(tr, NormalizeOptions{EngineID: "main"})
	want := []itemSig{
		{"user:u1", "", ext.KeyUserPrompt, ext.Done},
		{"user:q1", "", ext.KeyUserPrompt, ext.Done},
		{"sys:h1", "", ext.KeySystemInformational, ext.Done},
		{"local:a1", "", ext.KeySystemLocalCommand, ext.Done},
		{"err:a2", "", ext.KeySystemError, ext.Failed},
		{"user:b1", "", ext.KeyUserBash, ext.Done},
		{"toolu_t", "", ext.ToolKey("Task"), ext.Failed},
		{"txt:s2:0", "toolu_t", ext.KeyAssistantText, ext.Done},
		{"sys:lc", "", ext.KeySystemLocalCommand, ext.Done},
	}
	if got := sigs(items); !slices.Equal(got, want) {
		t.Fatalf("items:\n got %v\nwant %v", got, want)
	}
	if txt := items[0].Data.(*proto.User).Message.Content.PlainText(); txt != "/review 42" {
		t.Fatalf("command = %q", txt)
	}
	if info := items[2].Data.(*proto.Informational); info.Content != "PreToolUse:Bash: no rm" || info.Level != "warning" {
		t.Fatalf("hook = %+v", info)
	}
	if a := items[4].Data.(*proto.Assistant); a.Error != "overloaded" {
		t.Fatalf("error = %+v", a)
	}
	if txt := items[5].Data.(*proto.User).Message.Content.PlainText(); !strings.Contains(txt, "<bash-stdout>a.go") {
		t.Fatalf("bash output not merged: %q", txt)
	}
	if out := items[8].Data.(*proto.LocalCommandOutput); out.Content != "Set effort" {
		t.Fatalf("local_command = %q", out.Content)
	}
}
