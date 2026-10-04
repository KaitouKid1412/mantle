package enginetest

// Protocol spikes against the real claude, offline (fakeapi + isolated config).
// They log facts rather than assert most of them; run with
//
//	MANTLE_SPIKES=1 go test -run Spike -v ./internal/engine/enginetest
//
// The facts are recorded in docs/plans/02-proto-engine.md ("Facts verified").

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func spike(t *testing.T) {
	t.Helper()
	if os.Getenv("MANTLE_SPIKES") == "" {
		t.Skip("set MANTLE_SPIKES=1")
	}
}

func text(s string) []proto.ContentBlock { return []proto.ContentBlock{proto.Text(s)} }

func short(v any) string {
	b, _ := json.Marshal(v)
	if len(b) > 400 {
		return string(b[:400]) + "..."
	}
	return string(b)
}

func isInitDone(m tea.Msg) bool {
	r, ok := m.(ext.ControlResultMsg)
	return ok && r.Subtype == proto.SubInitialize
}

// sessionFile finds <config>/projects/*/<sid>.jsonl.
func sessionFile(t *testing.T, config, sid string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(config, "projects", "*", sid+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

func jsonlRecords(path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func control(t *testing.T, e *engine.Engine, req proto.Request) json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := e.Request(ctx, req)
	if err != nil {
		t.Logf("%s: error %v", req.ControlSubtype(), err)
	}
	return resp
}

func TestSpikeS5Entrypoint(t *testing.T) {
	spike(t)
	for _, ep := range []string{"", "cli", "mantle", "sdk-ts"} {
		r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
		o := r.Opts()
		if ep != "" {
			o.Env["CLAUDE_CODE_ENTRYPOINT"] = ep
		}
		e, err := r.Manager.Start("", o)
		if err != nil {
			t.Fatal(err)
		}
		e.Send(ext.Prompt{UUID: "11111111-1111-4111-8111-111111111111", Blocks: text("hi")})()
		r.Rec.WaitFor(t, resultFor("11111111-1111-4111-8111-111111111111"))
		sid := e.Snapshot().Info.SessionID
		e.Stop(context.Background())
		vals := map[string]bool{}
		for _, rec := range jsonlRecords(sessionFile(t, r.Config, sid)) {
			if v, ok := rec["entrypoint"].(string); ok {
				vals[v] = true
			}
		}
		t.Logf("S5 CLAUDE_CODE_ENTRYPOINT=%q -> jsonl entrypoint values %v", ep, vals)
	}
}

func TestSpikeS7Resume(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
	r.AutoAllow = true
	e, err := r.Manager.Start("", r.Opts())
	if err != nil {
		t.Fatal(err)
	}
	u1, u2 := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	e.Send(ext.Prompt{UUID: u1, Blocks: text("first")})()
	r.Rec.WaitFor(t, resultFor(u1))
	e.Send(ext.Prompt{UUID: u2, Blocks: text("second")})()
	r.Rec.WaitFor(t, resultFor(u2))
	sid := e.Snapshot().Info.SessionID
	recs := jsonlRecords(sessionFile(t, r.Config, sid))
	var asst1 string
	for _, rec := range recs {
		if rec["type"] == "assistant" && asst1 == "" {
			asst1, _ = rec["uuid"].(string)
		}
	}
	t.Logf("S7 session %s: %d jsonl records; first assistant uuid %s; user uuids kept: %v", sid, len(recs), asst1,
		strings.Contains(short(recs), u1))

	run := func(name string, o ext.SpawnOpts, prompt string) {
		n := len(r.Rec.Msgs())
		reqs := len(r.API.Requests())
		if err := e.RestartNow(context.Background(), o); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		uid := "33333333-3333-4333-8333-33333333333" + string(rune('0'+len(name)%10))
		e.Send(ext.Prompt{UUID: uid, Blocks: text(prompt)})()
		var got tea.Msg
		done := make(chan struct{})
		go func() {
			defer close(done)
			got = r.Rec.WaitFor(t, func(m tea.Msg) bool {
				if x, ok := m.(ext.EngineExitedMsg); ok && x.Err != nil {
					return true
				}
				return resultFor(uid)(m)
			})
		}()
		<-done
		var replayed []string
		var initSID string
		for _, m := range r.Rec.Msgs()[n:] {
			ev, ok := m.(ext.EngineEventMsg)
			if !ok {
				continue
			}
			switch x := ev.Event.(type) {
			case *proto.SystemInit:
				initSID = x.SessionID
			case *proto.Assistant:
				replayed = append(replayed, "assistant")
			case *proto.User:
				if !x.IsReplay {
					replayed = append(replayed, "user")
				}
			}
		}
		msgs := 0
		if rs := r.API.RequestsOf(fakeapi.KindMain); len(rs) > 0 && len(r.API.Requests()) > reqs {
			if mr, err := rs[len(rs)-1].Messages(); err == nil {
				msgs = len(mr.Messages)
			}
		}
		t.Logf("S7 %s: init session_id=%s (orig %s) events-before/after=%v api messages=%d final=%s", name, initSID, sid, replayed, msgs, short(describe(got)))
	}
	o := r.Opts()
	o.Resume = sid
	run("resume", o, "third")
	o.ForkSession = true
	run("fork", o, "fourth")
	o.ForkSession = false
	o.ResumeSessionAt = asst1
	run("resume-at-assistant1", o, "fifth")
	o.ResumeSessionAt = u1
	run("resume-at-user1", o, "sixth")
	o.ResumeSessionAt = asst1
	o.ResumeDropsTurn = true
	run("resume-at+drops-turn(same uuid)", o, "seventh")

	// rewind_files dry run after a Write.
	r2 := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{
		{Match: &fakeapi.Match{Contains: "write"}, Reply: []fakeapi.Block{{Type: "tool_use", Name: "Write",
			Input: json.RawMessage(`{"file_path":"` + filepath.Join(t.TempDir(), "x") + `","content":"one\n"}`)}}},
		{Match: &fakeapi.Match{ToolResult: "Write"}, Reply: []fakeapi.Block{{Type: "text", Text: "written"}}},
	}})
	r2.AutoAllow = true
	e2, _ := r2.Manager.Start("", r2.Opts())
	e2.Send(ext.Prompt{UUID: u1, Blocks: text("write")})()
	r2.Rec.WaitFor(t, resultFor(u1))
	t.Logf("S7 rewind_files dry_run: %s", control(t, e2, proto.RewindFilesRequest{UserMessageID: u1, DryRun: true}))
}

func TestSpikeS9S10S11ProjectConfig(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
	// Untrusted dir with project hooks, env and .mcp.json.
	proj, _ := filepath.EvalSymlinks(t.TempDir())
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(proj, "sub", "deep"), 0o755)
	for _, f := range []string{"alpha.go", "beta.txt", "sub/apple.md", "sub/deep/avocado.go"} {
		os.WriteFile(filepath.Join(proj, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte(`{"env":{"SPIKE_PROJECT_ENV":"1"},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"touch `+filepath.Join(proj, "hook.marker")+`"}]}]}}`), 0o644)
	os.WriteFile(filepath.Join(proj, ".mcp.json"), []byte(`{"mcpServers":{"x":{"command":"cat"},"y":{"command":"cat"}}}`), 0o644)
	os.WriteFile(filepath.Join(proj, "CLAUDE.md"), []byte("project memory"), 0o644)

	for _, c := range []struct {
		name     string
		settings string
	}{{"untrusted", ""}, {"untrusted+disabledMcpjsonServers", `{"disabledMcpjsonServers":["x"]}`}} {
		os.Remove(filepath.Join(proj, "hook.marker"))
		o := r.Opts()
		o.Cwd = proj
		o.Settings = c.settings
		id := "s9-" + strings.ReplaceAll(c.name, "+", "-")
		e, err := r.Manager.Start(id, o)
		if err != nil {
			t.Fatal(err)
		}
		r.Rec.WaitFor(t, func(m tea.Msg) bool {
			x, ok := m.(ext.ControlResultMsg)
			return ok && x.EngineID == id && x.Subtype == proto.SubInitialize
		})
		time.Sleep(500 * time.Millisecond)
		var st proto.MCPStatusResponse
		_ = json.Unmarshal(control(t, e, proto.MCPStatusRequest{}), &st)
		var servers []string
		for _, s := range st.MCPServers {
			servers = append(servers, s.Name+"="+s.Status+"/"+s.Scope)
		}
		var set proto.SettingsResponse
		_ = json.Unmarshal(control(t, e, proto.GetSettingsRequest{}), &set)
		var sources []string
		for _, s := range set.Sources {
			sources = append(sources, s.Source)
		}
		var cu proto.ContextUsage
		_ = json.Unmarshal(control(t, e, proto.GetContextUsageRequest{}), &cu)
		_, markerErr := os.Stat(filepath.Join(proj, "hook.marker"))
		t.Logf("S9/S10 %s: mcp=%v settings sources=%v project hook ran=%v memoryFiles=%s", c.name, servers, sources, markerErr == nil, short(cu.MemoryFiles))

		if c.settings == "" {
			for _, q := range []string{"", "a", "sub/", "avo"} {
				t0 := time.Now()
				resp := control(t, e, proto.FileSuggestionsRequest{Query: q})
				t.Logf("S11 file_suggestions %q (%v): %s", q, time.Since(t0).Round(time.Millisecond), short(json.RawMessage(resp)))
			}
		}
		e.Stop(context.Background())
	}
}

func TestSpikeS7DropsTurn(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
	e, _ := r.Manager.Start("", r.Opts())
	for i, u := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		e.Send(ext.Prompt{UUID: u, Blocks: text([]string{"first", "second"}[i])})()
		r.Rec.WaitFor(t, resultFor(u))
	}
	sid := e.Snapshot().Info.SessionID
	type rec struct{ typ, uuid, parent string }
	var recs []rec
	for _, m := range jsonlRecords(sessionFile(t, r.Config, sid)) {
		if m["type"] == "user" || m["type"] == "assistant" {
			u, _ := m["uuid"].(string)
			p, _ := m["parentUuid"].(string)
			recs = append(recs, rec{m["type"].(string), u, p})
		}
	}
	t.Logf("S7b chain: %+v", recs)
	if len(recs) < 4 {
		t.Fatal("short chain")
	}
	at := recs[1].uuid // first assistant: keep turn 1, drop turn 2
	for _, drop := range []rec{recs[1], recs[2], recs[3]} {
		o := r.Opts()
		o.Resume, o.ResumeSessionAt = sid, at
		o.ExtraArgs = []string{"--resume-drops-turn=" + drop.uuid}
		n := len(r.Rec.Msgs())
		if err := e.RestartNow(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		u := "33333333-3333-4333-8333-333333333333"
		e.Send(ext.Prompt{UUID: u, Blocks: text("third")})()
		got := r.Rec.WaitFor(t, func(m tea.Msg) bool {
			_, ok := m.(ext.EngineExitedMsg)
			return (ok && len(r.Rec.Msgs()) > n) || resultFor(u)(m)
		})
		detail := describe(got)
		if x, ok := got.(ext.EngineExitedMsg); ok {
			detail += " stderr=" + x.Stderr
		}
		for _, m := range r.Rec.Msgs()[n:] {
			if ev, ok := m.(ext.EngineEventMsg); ok {
				if res, ok := ev.Event.(*proto.Result); ok && res.IsError {
					detail += " result.errors=" + short(res.Errors)
				}
			}
		}
		t.Logf("S7b resume-at=%s(assistant1) drops-turn=%s(%s): %s", at[:8], drop.uuid[:8], drop.typ, detail)
	}
}

func TestSpikeS11FileSuggestions(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
	for _, f := range []string{"alpha.go", "beta.txt", "sub/apple.md", "sub/deep/avocado.go"} {
		os.MkdirAll(filepath.Dir(filepath.Join(r.Work, f)), 0o755)
		os.WriteFile(filepath.Join(r.Work, f), []byte("x"), 0o644)
	}
	e, _ := r.Manager.Start("", r.Opts())
	r.Rec.WaitFor(t, isInitDone)
	for round := 0; round < 2; round++ {
		for _, q := range []string{"a", "sub/", "avo", "alpha"} {
			t0 := time.Now()
			resp := control(t, e, proto.FileSuggestionsRequest{Query: q})
			t.Logf("S11 round %d %q (%v): %s", round, q, time.Since(t0).Round(time.Millisecond), short(json.RawMessage(resp)))
		}
		time.Sleep(2 * time.Second)
	}
}

// TestSpikeS14Interactive runs interactive claude (no -p) in a pty against fakeapi
// with a seeded isolated config, types a prompt and logs the screen text.
func TestSpikeS14Interactive(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{fakeapi.TextTurn("Interactive hello from fakeapi.")}})
	cmd := exec.Command("claude", "--model", "claude-sonnet-4-5")
	cmd.Dir = r.Work
	cmd.Env = append(fakeapi.Env(os.Environ(), r.URL, r.Config, fakeapi.FakeAPIKey), "TERM=xterm-256color")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait(); f.Close() }()
	var mu sync.Mutex
	var screen bytes.Buffer
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := f.Read(buf)
			mu.Lock()
			screen.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return ansiRe.ReplaceAllString(screen.String(), "")
	}
	time.Sleep(4 * time.Second)
	first := snapshot()
	t.Logf("S14 interactive first screen (tail):\n%s", tail(first, 1500))
	f.Write([]byte("say hello"))
	time.Sleep(500 * time.Millisecond)
	f.Write([]byte("\r"))
	time.Sleep(4 * time.Second)
	after := snapshot()
	t.Logf("S14 interactive answered=%v main requests=%d", strings.Contains(after, "Interactive hello from fakeapi"), len(r.API.RequestsOf(fakeapi.KindMain)))
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][0-9A-Za-z]|\x1b[=>78]`)

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// TestSpikeS17DeltaRate measures the engine's peak stream_event rate (fakeapi sends
// one rune per delta, as fast as possible) and how many UI messages the bridge makes.
func TestSpikeS17DeltaRate(t *testing.T) {
	spike(t)
	long := strings.Repeat("abcdefghij ", 600) // 6600 runes -> 6600 deltas
	r := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{fakeapi.TextTurn(long)}}, fakeapi.WithChunkRunes(1))
	var mu sync.Mutex
	var first, last time.Time
	deltas := 0
	r.Manager.Tap = func(dir engine.Direction, line []byte) {
		if dir == engine.FromEngine && bytes.Contains(line, []byte(`"content_block_delta"`)) {
			mu.Lock()
			if deltas == 0 {
				first = time.Now()
			}
			last = time.Now()
			deltas++
			mu.Unlock()
		}
	}
	e, _ := r.Manager.Start("", r.Opts())
	u := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u, Blocks: text("go")})()
	r.Rec.WaitFor(t, resultFor(u))
	ui := 0
	for _, ev := range Events(r.Rec.Msgs()) {
		if se, ok := ev.(*proto.StreamEvent); ok && se.IsDelta() {
			ui++
		}
	}
	mu.Lock()
	defer mu.Unlock()
	span := last.Sub(first)
	t.Logf("S17 engine printed %d deltas in %v (%.0f/s); bridge delivered %d delta messages (16 ms coalescing)",
		deltas, span.Round(time.Millisecond), float64(deltas)/span.Seconds(), ui)
}

// TestSpikeUnstable tries the undocumented subtypes B8 wraps.
func TestSpikeUnstable(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{DefaultReply: "ok", SideReply: "side answer"})
	exec.Command("git", "-C", r.Work, "init", "-q").Run()
	os.WriteFile(filepath.Join(r.Work, "a.txt"), []byte("one\n"), 0o644)
	exec.Command("git", "-C", r.Work, "add", ".").Run()
	exec.Command("git", "-C", r.Work, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "init").Run()
	os.WriteFile(filepath.Join(r.Work, "a.txt"), []byte("two\n"), 0o644)
	e, _ := r.Manager.Start("", r.Opts())
	u1, u2 := "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"
	e.Send(ext.Prompt{UUID: u1, Blocks: text("first")})()
	r.Rec.WaitFor(t, resultFor(u1))
	e.Send(ext.Prompt{UUID: u2, Blocks: text("second")})()
	r.Rec.WaitFor(t, resultFor(u2))
	for _, req := range []proto.RawRequest{
		{Subtype: engine.SubGetWorkspaceDiff},
		{Subtype: engine.SubSideQuestion, Fields: json.RawMessage(`{"question":"what is 2+2?","history":[]}`)},
		{Subtype: engine.SubRewindConversation, Fields: json.RawMessage(`{"target_message_uuid":"` + u2 + `"}`)},
	} {
		n := len(r.Rec.Msgs())
		resp, err := e.Request(context.Background(), req)
		t.Logf("B8 %s -> err=%v resp=%s", req.Subtype, err, short(json.RawMessage(resp)))
		time.Sleep(300 * time.Millisecond)
		for _, m := range r.Rec.Msgs()[n:] {
			if ev, ok := m.(ext.EngineEventMsg); ok {
				t.Logf("   event %s/%s %s", ev.Event.Env().Type, ev.Event.Env().Subtype, short(json.RawMessage(ev.Event.Env().Raw)))
			}
		}
	}
}

func TestSpikeS12Subagent(t *testing.T) {
	spike(t)
	r := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{
		{Match: &fakeapi.Match{Contains: "delegate"}, Reply: []fakeapi.Block{{Type: "tool_use", Name: "Task",
			Input: json.RawMessage(`{"description":"Say hi","prompt":"SUBAGENT: say hi","subagent_type":"general-purpose"}`)}}},
		{Match: &fakeapi.Match{Contains: "SUBAGENT"}, Reply: []fakeapi.Block{{Type: "thinking", Thinking: "sub thinks"}, {Type: "text", Text: "hi from the subagent"}}},
		{Match: &fakeapi.Match{ToolResult: "Task"}, Reply: []fakeapi.Block{{Type: "text", Text: "parent done"}}},
	}})
	r.AutoAllow = true
	e, _ := r.Manager.Start("", r.Opts())
	u := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u, Blocks: text("delegate")})()
	got := r.Rec.WaitFor(t, func(m tea.Msg) bool {
		if resultFor(u)(m) {
			return true
		}
		x, ok := m.(ext.EngineExitedMsg)
		return ok && x.Err != nil
	})
	t.Logf("S12 end: %s; unmatched=%v", describe(got), r.API.Unmatched())
	for _, ev := range Events(r.Rec.Msgs()) {
		env := ev.Env()
		switch x := ev.(type) {
		case *proto.Assistant:
			t.Logf("S12 assistant parent=%q blocks=%s", x.ParentToolUseID, short(x.Message.Content))
		case *proto.User:
			t.Logf("S12 user parent=%q replay=%v results=%d", x.ParentToolUseID, x.IsReplay, len(x.ToolResults()))
		case *proto.StreamEvent:
			if x.ParentToolUseID != "" && x.Event.Type == proto.StreamContentBlockStart {
				t.Logf("S12 stream_event with parent %q: %s", x.ParentToolUseID, x.Event.ContentBlock.Type)
			}
		case *proto.TaskStarted, *proto.TaskProgress, *proto.TaskNotification, *proto.TaskUpdated:
			t.Logf("S12 %s: %s", env.Subtype, env.Raw)
		}
	}
}

func TestSpikeS13Thinking(t *testing.T) {
	spike(t)
	for _, args := range [][]string{nil, {"--thinking-display", "summarized"}, {"--max-thinking-tokens", "2048"}} {
		r := NewReal(t, &fakeapi.Script{Turns: []fakeapi.Turn{
			{Reply: []fakeapi.Block{{Type: "thinking", Thinking: "Let me think about this carefully."}, {Type: "text", Text: "answer"}}},
		}})
		o := r.Opts()
		o.ExtraArgs = args
		e, _ := r.Manager.Start("", o)
		u := "11111111-1111-4111-8111-111111111111"
		e.Send(ext.Prompt{UUID: u, Blocks: text("think")})()
		r.Rec.WaitFor(t, resultFor(u))
		var deltas []string
		var block string
		for _, ev := range Events(r.Rec.Msgs()) {
			if se, ok := ev.(*proto.StreamEvent); ok && se.Event.Delta != nil && se.Event.Delta.Type != proto.DeltaText {
				deltas = append(deltas, se.Event.Delta.Type)
			}
			if a, ok := ev.(*proto.Assistant); ok && a.Message.Content[0].Type == proto.BlockThinking {
				block = short(a.Message.Content[0])
			}
		}
		var thinkingReq json.RawMessage
		if rs := r.API.RequestsOf(fakeapi.KindMain); len(rs) > 0 {
			if mr, err := rs[0].Messages(); err == nil {
				thinkingReq = mr.Thinking
			}
		}
		t.Logf("S13 args=%v request.thinking=%s deltas=%v block=%s", args, thinkingReq, deltas, block)
	}
}
