package enginetest

// Fixture recorder: runs every flow against the real claude (offline, through
// fakeapi) and writes testdata/fixtures/02/<flow>.jsonl (both directions, sanitised)
// plus <flow>.ndjson (engine stdout only). Re-record on a new engine version:
//
//	MANTLE_RECORD_FIXTURES=1 go test -run RecordFixtures ./internal/engine/enginetest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/engine/fixture"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// FixtureDir is testdata/fixtures/02 at the repository root.
func FixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "02")
}

type flow struct {
	name   string
	script *fakeapi.Script
	opts   func(o *ext.SpawnOpts)
	drive  func(t *testing.T, d *driver)
}

// driver is a flow's handle on the running engine.
type driver struct {
	r *Real
	e *engine.Engine
	n int
}

func (d *driver) uuid() string {
	d.n++
	return []string{
		"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555", "66666666-6666-4666-8666-666666666666",
	}[d.n-1]
}

// send writes a prompt and returns its uuid.
func (d *driver) send(t *testing.T, text, priority string) string {
	t.Helper()
	u := d.uuid()
	if msg := d.e.Send(ext.Prompt{UUID: u, Priority: priority, Blocks: []proto.ContentBlock{proto.Text(text)}})(); msg != nil {
		t.Fatalf("send: %v", msg)
	}
	return u
}

// prompt sends text and waits for its result.
func (d *driver) prompt(t *testing.T, text string) *proto.Result {
	t.Helper()
	u := d.send(t, text, "")
	return d.result(t, u)
}

func (d *driver) result(t *testing.T, u string) *proto.Result {
	t.Helper()
	return d.r.Rec.WaitFor(t, resultFor(u)).(ext.EngineEventMsg).Event.(*proto.Result)
}

// permission waits for the next PermissionMsg after mark.
func (d *driver) permission(t *testing.T, mark int) ext.PermissionMsg {
	t.Helper()
	return d.r.Rec.WaitAfter(t, mark, func(m tea.Msg) bool { _, ok := m.(ext.PermissionMsg); return ok }).(ext.PermissionMsg)
}

func (d *driver) waitEvent(t *testing.T, mark int, match func(proto.Event) bool) proto.Event {
	t.Helper()
	return d.r.Rec.WaitAfter(t, mark, func(m tea.Msg) bool {
		ev, ok := m.(ext.EngineEventMsg)
		return ok && match(ev.Event)
	}).(ext.EngineEventMsg).Event
}

func (d *driver) interrupt(t *testing.T) {
	t.Helper()
	if res := d.e.Interrupt(false)().(ext.ControlResultMsg); res.Err != nil {
		t.Fatalf("interrupt: %v", res.Err)
	}
}

func (d *driver) idle(t *testing.T, mark int) {
	t.Helper()
	d.waitEvent(t, mark, func(ev proto.Event) bool {
		s, ok := ev.(*proto.SessionStateChanged)
		return ok && s.State == proto.StateIdle
	})
}

func bash(cmd string) fakeapi.Block {
	in, _ := json.Marshal(map[string]any{"command": cmd, "description": "Run " + cmd})
	return fakeapi.Block{Type: "tool_use", Name: "Bash", Input: in}
}

func textTurn(match, text string) fakeapi.Turn {
	t := fakeapi.TextTurn(text)
	if match != "" {
		t.Match = &fakeapi.Match{Contains: match}
	}
	return t
}

func afterTool(tool, text string) fakeapi.Turn {
	return fakeapi.Turn{Match: &fakeapi.Match{ToolResult: tool}, Reply: []fakeapi.Block{{Type: "text", Text: text}}}
}

var askInput = json.RawMessage(`{"questions":[{"question":"Which color should the button be?","header":"Color","options":[{"label":"Red","description":"Warm and loud"},{"label":"Blue","description":"Calm"}],"multiSelect":false}]}`)

var flows = []flow{
	{name: "plain-answer", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "thinking", Thinking: "The user said hello; answer briefly."}, {Type: "text", Text: "Hello! How can I help you today?"}}},
	}}, drive: func(t *testing.T, d *driver) { d.prompt(t, "hello") }},

	{name: "tool-allow", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "text", Text: "I'll create the file."}, bash("echo hi > out.txt")}},
		afterTool("Bash", "Created out.txt."),
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "create out.txt", "")
		pm := d.permission(t, m)
		pm.Reply(pm.Req.Allow(nil))()
		d.result(t, u)
	}},

	{name: "tool-deny", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{bash("rm -rf build")}},
		afterTool("Bash", "OK, I won't delete it."),
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "clean the build", "")
		pm := d.permission(t, m)
		pm.Reply(pm.Req.Deny("Don't delete build/", false))()
		d.result(t, u)
	}},

	{name: "tool-always", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{bash("touch a.txt")}},
		afterTool("Bash", "Touched."),
		{Match: &fakeapi.Match{Contains: "again"}, Reply: []fakeapi.Block{bash("touch a.txt")}},
		afterTool("Bash", "Touched again, no prompt."),
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "touch a file", "")
		pm := d.permission(t, m)
		pm.Reply(pm.Req.AllowAlways(pm.Req.PermissionSuggestions...))()
		d.result(t, u)
		d.prompt(t, "touch it again")
	}},

	{name: "ask-user-question", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "tool_use", Name: "AskUserQuestion", Input: askInput}}},
		afterTool("AskUserQuestion", "Blue it is."),
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "style the button", "")
		pm := d.permission(t, m)
		var in map[string]any
		_ = json.Unmarshal(pm.Req.Input, &in)
		in["answers"] = map[string]string{"Which color should the button be?": "Blue"}
		upd, _ := json.Marshal(in)
		pm.Reply(pm.Req.Allow(upd))()
		d.result(t, u)
	}},

	{name: "exit-plan-mode", opts: func(o *ext.SpawnOpts) { o.PermissionMode = proto.ModePlan },
		script: &fakeapi.Script{Turns: []fakeapi.Turn{
			{Reply: []fakeapi.Block{{Type: "tool_use", Name: "ExitPlanMode", Input: json.RawMessage(`{"plan":"1. Add a test\n2. Fix the bug"}`)}}},
			afterTool("ExitPlanMode", "Plan approved; starting."),
		}}, drive: func(t *testing.T, d *driver) {
			m := d.r.Rec.Len()
			u := d.send(t, "plan the fix", "")
			pm := d.permission(t, m)
			pm.Reply(pm.Req.Allow(nil))()
			d.result(t, u)
		}},

	{name: "interrupt-text", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{ChunkDelayMS: 40, Reply: []fakeapi.Block{{Type: "text", Text: "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty"}}},
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "count in words", "")
		d.waitEvent(t, m, func(ev proto.Event) bool { se, ok := ev.(*proto.StreamEvent); return ok && se.IsDelta() })
		d.interrupt(t)
		d.result(t, u)
	}},

	{name: "interrupt-tool", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{bash("sleep 5 && touch done.txt")}},
		afterTool("Bash", "unreachable"),
	}}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "wait a bit", "")
		pm := d.permission(t, m)
		pm.Reply(pm.Req.Allow(nil))()
		time.Sleep(1500 * time.Millisecond)
		d.interrupt(t)
		d.result(t, u)
	}},

	{name: "queued-priority", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{ChunkDelayMS: 60, Reply: []fakeapi.Block{{Type: "text", Text: "Working on the first request, slowly, piece by piece."}}},
	}, DefaultReply: "Handled a queued message."}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u1 := d.send(t, "first", "")
		d.waitEvent(t, m, func(ev proto.Event) bool { se, ok := ev.(*proto.StreamEvent); return ok && se.IsDelta() })
		u2 := d.send(t, "also mention tests", proto.PriorityNext)
		u3 := d.send(t, "then summarize", proto.PriorityLater)
		d.result(t, u1)
		d.result(t, u3)
		_ = u2
		d.idle(t, m)
	}},

	{name: "local-commands", script: &fakeapi.Script{}, drive: func(t *testing.T, d *driver) {
		d.prompt(t, "/context")
		d.prompt(t, "/cost")
		d.prompt(t, "/model")
		d.prompt(t, "/help")
	}},

	{name: "compact", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		textTurn("first", "First answer."),
		textTurn("second", "Second answer."),
	}, DefaultReply: "Summary: the user asked two questions and got two answers."}, drive: func(t *testing.T, d *driver) {
		d.prompt(t, "first question")
		d.prompt(t, "second question")
		d.prompt(t, "/compact")
	}},

	{name: "clear", script: &fakeapi.Script{DefaultReply: "Sure."}, drive: func(t *testing.T, d *driver) {
		d.prompt(t, "remember the number 7")
		d.prompt(t, "/clear")
		d.prompt(t, "what number?")
	}},

	{name: "resume", script: &fakeapi.Script{DefaultReply: "Noted."}, drive: func(t *testing.T, d *driver) {
		d.prompt(t, "my name is Ada")
		sid := d.e.Snapshot().Info.SessionID
		o := d.e.Options()
		o.Resume = sid
		if err := d.e.RestartNow(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		d.prompt(t, "what is my name?")
	}},

	{name: "subagent", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "tool_use", Name: "Task", Input: json.RawMessage(`{"description":"Find TODOs","prompt":"SUBAGENT: find TODO comments","subagent_type":"general-purpose"}`)}}},
		{Match: &fakeapi.Match{Contains: "SUBAGENT"}, Reply: []fakeapi.Block{{Type: "thinking", Thinking: "Search the files."}, {Type: "text", Text: "Found 2 TODOs in main.go."}}},
		{Match: &fakeapi.Match{Contains: "Async agent"}, Reply: []fakeapi.Block{{Type: "text", Text: "A subagent is searching; I'll report back."}}},
	}, DefaultReply: "The subagent found 2 TODOs."}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		d.prompt(t, "delegate the TODO search")
		d.waitEvent(t, m, func(ev proto.Event) bool { _, ok := ev.(*proto.TaskNotification); return ok })
		d.idle(t, m)
	}},

	{name: "background-task", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "tool_use", Name: "Bash", Input: json.RawMessage(`{"command":"sleep 1 && touch built.txt","description":"Build in the background","run_in_background":true}`)}}},
		afterTool("Bash", "The build runs in the background."),
	}, DefaultReply: "The build finished."}, drive: func(t *testing.T, d *driver) {
		m := d.r.Rec.Len()
		u := d.send(t, "build in the background", "")
		pm := d.permission(t, m)
		pm.Reply(pm.Req.Allow(nil))()
		d.result(t, u)
		d.waitEvent(t, m, func(ev proto.Event) bool { _, ok := ev.(*proto.TaskNotification); return ok })
		d.idle(t, m)
	}},

	{name: "rate-limit", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Reply: []fakeapi.Block{{Type: "text", Text: "Answer while close to the limit."}}, Headers: map[string]string{
			"anthropic-ratelimit-unified-status":               "allowed_warning",
			"anthropic-ratelimit-unified-reset":                "1791042600",
			"anthropic-ratelimit-unified-5h-utilization":       "0.92",
			"anthropic-ratelimit-unified-5h-reset":             "1791042600",
			"anthropic-ratelimit-unified-representative-claim": "five_hour",
		}},
	}}, drive: func(t *testing.T, d *driver) { d.prompt(t, "hi") }},

	{name: "api-retry", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Error: &fakeapi.APIError{Status: 529, Type: "overloaded_error", Message: "Overloaded"}},
		textTurn("", "Answer after a retry."),
	}}, drive: func(t *testing.T, d *driver) { d.prompt(t, "hi") }},

	{name: "api-error", script: &fakeapi.Script{Turns: []fakeapi.Turn{
		{Error: &fakeapi.APIError{Status: 400, Type: "invalid_request_error", Message: "prompt is too long"}},
	}}, drive: func(t *testing.T, d *driver) { d.prompt(t, "hi") }},
}

func TestRecordFixtures(t *testing.T) {
	if os.Getenv("MANTLE_RECORD_FIXTURES") == "" {
		t.Skip("set MANTLE_RECORD_FIXTURES=1 to re-record testdata/fixtures/02")
	}
	only := os.Getenv("MANTLE_RECORD_ONLY")
	dir := FixtureDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range flows {
		if only != "" && only != f.name {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			r := NewReal(t, f.script)
			var buf bytes.Buffer
			san := fixture.NewSanitizer(r.Work, "/home/user/project", r.Config, "/home/user/.claude-test")
			rec := fixture.NewRecorder(&buf, san)
			r.Manager.Tap = func(dir engine.Direction, line []byte) {
				d := fixture.In
				if dir == engine.FromEngine {
					d = fixture.Out
				}
				rec.Record(d, line)
			}
			o := r.Opts()
			if f.opts != nil {
				f.opts(&o)
			}
			e, err := r.Manager.Start("", o)
			if err != nil {
				t.Fatal(err)
			}
			r.Rec.WaitFor(t, isInitDone)
			f.drive(t, &driver{r: r, e: e})
			e.Stop(context.Background())
			if err := rec.Err(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, f.name+".jsonl"), buf.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			entries, err := fixture.Read(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			var nd bytes.Buffer
			for _, l := range fixture.Outputs(entries) {
				nd.Write(l)
				nd.WriteByte('\n')
			}
			if err := os.WriteFile(filepath.Join(dir, f.name+".ndjson"), nd.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d entries, %d API requests, unmatched %v", f.name, len(entries), len(r.API.Requests()), r.API.Unmatched())
			if f.name == "rate-limit" {
				writeRateLimitEvent(t, dir, entries)
			}
		})
	}
}

// writeRateLimitEvent derives rate-limit-event.{jsonl,ndjson}: the engine emits
// rate_limit_event only for subscription (OAuth) accounts, which fakeapi can't fake,
// so this inserts one event, shaped as seen live on 2.1.288, after message_stop.
func writeRateLimitEvent(t *testing.T, dir string, entries []fixture.Entry) {
	ev := json.RawMessage(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","resetsAt":1791042600,"rateLimitType":"five_hour","utilization":0.92,"overageStatus":"rejected","overageDisabledReason":"out_of_credits","isUsingOverage":false,"unifiedWindows":{"five_hour":{"utilization":0.92,"resetsAt":1791042600},"seven_day":{"utilization":0.12,"resetsAt":1791529200}}},"uuid":"00000000-0000-4000-8000-000000009999","session_id":"00000000-0000-4000-8000-000000000001"}`)
	var out []fixture.Entry
	for _, e := range entries {
		out = append(out, e)
		if e.Dir == fixture.Out && bytes.Contains(e.Msg, []byte(`"type":"message_stop"`)) {
			out = append(out, fixture.Entry{T: e.T, Dir: fixture.Out, Msg: ev})
		}
	}
	var js, nd bytes.Buffer
	for _, e := range out {
		b, _ := json.Marshal(e)
		js.Write(append(b, '\n'))
		if e.Dir == fixture.Out {
			nd.Write(append(append([]byte(nil), e.Msg...), '\n'))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "rate-limit-event.jsonl"), js.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rate-limit-event.ndjson"), nd.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
