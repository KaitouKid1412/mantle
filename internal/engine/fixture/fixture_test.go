package fixture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/engine/enginetest"
	"github.com/KaitouKid1412/mantle/internal/engine/fixture"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func fixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "02")
}

func load(t *testing.T, name string) []fixture.Entry {
	t.Helper()
	es, err := fixture.Load(filepath.Join(fixtureDir(), name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// TestFixturesDecodeAndRoundTrip: every recorded engine line decodes to a known type
// with no shape drift and no unmodelled fields, and .ndjson matches the recording.
func TestFixturesDecodeAndRoundTrip(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(fixtureDir(), "*.jsonl"))
	if len(files) < 15 {
		t.Fatalf("only %d fixtures", len(files))
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		entries := load(t, name)
		outs := fixture.Outputs(entries)
		if len(outs) == 0 {
			t.Errorf("%s: no engine output", name)
		}
		for i, line := range outs {
			ev, err := proto.Decode(line)
			if err != nil {
				t.Errorf("%s out %d: %v", name, i, err)
				continue
			}
			env := ev.Env()
			if _, ok := ev.(*proto.Unknown); ok {
				t.Errorf("%s out %d: unknown %s/%s", name, i, env.Type, env.Subtype)
			}
			if env.Mismatch != nil {
				t.Errorf("%s out %d: %v", name, i, env.Mismatch)
			}
			if lost, _ := proto.Unmodelled(ev); len(lost) > 0 {
				t.Errorf("%s out %d (%s/%s): unmodelled %v", name, i, env.Type, env.Subtype, lost)
			}
		}
		nd, err := os.ReadFile(strings.TrimSuffix(f, ".jsonl") + ".ndjson")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var want bytes.Buffer
		for _, l := range outs {
			want.Write(l)
			want.WriteByte('\n')
		}
		if !bytes.Equal(nd, want.Bytes()) {
			t.Errorf("%s.ndjson differs from the recording's output", name)
		}
	}
}

func TestSanitizer(t *testing.T) {
	home, _ := os.UserHomeDir()
	s := fixture.NewSanitizer("/work/secret-project", "/home/user/project")
	line := `{"cwd":"` + home + `/x","p":"/work/secret-project/a.go","email":"someone@corp.example.com",` +
		`"key":"sk-ant-api03-abcdefghijklmnop","auth":"Bearer abcdefghijklmnopqrstuv",` +
		`"session_id":"1BD4A383-7C8A-49FB-A66B-D55320E41104","again":"1bd4a383-7c8a-49fb-a66b-d55320e41104",` +
		`"other":"4d35fdbc-bb4d-4250-b195-d9a33675bd69","tool":"toolu_01Ar94JJYokAbMMCXskf3axf","msg":"msg_011CffKGHbfXxX6jjY9U2muv"}`
	got := string(s.Line([]byte(line)))
	for _, bad := range []string{home, "secret-project", "corp.example", "abcdefghijklmnop", "abcdefghijklmnopqrstuv", "1bd4a383", "toolu_01Ar", "msg_011C"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q survived: %s", bad, got)
		}
	}
	for _, want := range []string{`"/home/user/x"`, `"/home/user/project/a.go"`, "user@example.com", "sk-ant-REDACTED", "Bearer REDACTED",
		`"session_id":"00000000-0000-4000-8000-000000000001","again":"00000000-0000-4000-8000-000000000001"`,
		`"other":"00000000-0000-4000-8000-000000000002"`, `"toolu_00000001"`, `"msg_00000001"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if !json.Valid([]byte(got)) {
		t.Error("invalid JSON after sanitising")
	}
	// Stable across lines.
	if got2 := string(s.Line([]byte(`{"x":"toolu_01Ar94JJYokAbMMCXskf3axf"}`))); got2 != `{"x":"toolu_00000001"}` {
		t.Errorf("not stable: %s", got2)
	}
	// Engine prose in command lists becomes placeholders; names stay.
	got3 := string(s.Line([]byte(`{"type":"control_response","response":{"response":{"commands":[{"name":"review","description":"Long built-in prose"}],"agents":[{"name":"Explore","description":"More prose"}]}}}`)))
	if strings.Contains(got3, "prose") || !strings.Contains(got3, "Description of review.") || !strings.Contains(got3, `"name":"Explore"`) {
		t.Errorf("descriptions: %s", got3)
	}
}

func TestRecorderRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	rec := fixture.NewRecorder(&buf, nil)
	rec.Record(fixture.In, []byte(`{"type":"user"}`))
	rec.Record(fixture.Out, []byte("not json"))
	rec.Record(fixture.Out, []byte(`{"type":"result"}`+"\n"))
	es, err := fixture.Read(&buf)
	if err != nil || len(es) != 2 || es[0].Dir != fixture.In || string(es[1].Msg) != `{"type":"result"}` {
		t.Fatalf("entries %+v err %v", es, err)
	}
}

// TestReplayThroughEngine converts recordings into fakeclaude scripts and replays
// them through the real engine.Manager, re-sending the recorded prompts and
// permission answers: the engine must deliver the recorded output, in order.
func TestReplayThroughEngine(t *testing.T) {
	for _, name := range []string{"plain-answer", "tool-allow", "tool-deny", "ask-user-question"} {
		t.Run(name, func(t *testing.T) {
			entries := load(t, name)
			script, err := fixture.Script(entries, fixture.ScriptOptions{})
			if err != nil {
				t.Fatal(err)
			}
			rec := enginetest.NewRecorder()
			m := engine.NewManager(rec.Send)
			m.Spawner = &enginetest.Spawner{Scripts: []*enginefake.Script{script}}
			m.RunDir, m.Binary = "-", "claude"
			defer m.Close(context.Background())
			e, err := m.Start("", ext.SpawnOpts{})
			if err != nil {
				t.Fatal(err)
			}

			// Replay the client side.
			var lastUser string
			for _, en := range entries {
				if en.Dir != fixture.In {
					continue
				}
				var in struct {
					Type     string            `json:"type"`
					UUID     string            `json:"uuid"`
					Message  proto.UserMessage `json:"message"`
					Response struct {
						RequestID string          `json:"request_id"`
						Response  json.RawMessage `json:"response"`
					} `json:"response"`
				}
				_ = json.Unmarshal(en.Msg, &in)
				switch in.Type {
				case "user":
					lastUser = in.UUID
					e.Send(ext.Prompt{UUID: in.UUID, Blocks: []proto.ContentBlock{proto.Text(in.Message.Content.PlainText())}})()
				case "control_response":
					id := in.Response.RequestID
					pm := rec.WaitFor(t, func(m tea.Msg) bool { p, ok := m.(ext.PermissionMsg); return ok && p.RequestID == id }).(ext.PermissionMsg)
					var res proto.PermissionResult
					_ = json.Unmarshal(in.Response.Response, &res)
					pm.Reply(res)()
				}
			}
			rec.WaitFor(t, func(m tea.Msg) bool {
				ev, ok := m.(ext.EngineEventMsg)
				if !ok {
					return false
				}
				r, ok := ev.Event.(*proto.Result)
				return ok && r.UserMessageUUID == lastUser
			})

			// Compare: non-delta event kinds in order, plus the streamed text.
			kinds := func(lines [][]byte) ([]string, string) {
				var ks []string
				var text strings.Builder
				for _, l := range lines {
					ev, err := proto.Decode(l)
					if err != nil {
						continue
					}
					ks = appendKind(ks, &text, ev)
				}
				return ks, text.String()
			}
			var want [][]byte
			for _, l := range fixture.Outputs(entries) {
				want = append(want, l)
			}
			wantKinds, wantText := kinds(want)
			var gotKinds []string
			var gotText strings.Builder
			for _, ev := range enginetest.Events(rec.Msgs()) {
				gotKinds = appendKind(gotKinds, &gotText, ev)
			}
			if strings.Join(gotKinds, " ") != strings.Join(wantKinds, " ") {
				t.Errorf("events:\n got %v\nwant %v", gotKinds, wantKinds)
			}
			if gotText.String() != wantText {
				t.Errorf("streamed text %q, want %q", gotText.String(), wantText)
			}
		})
	}
}

// appendKind records an engine event's kind; deltas only add to text (the
// coalescer may merge them), control traffic is not an event.
func appendKind(ks []string, text *strings.Builder, ev proto.Event) []string {
	switch x := ev.(type) {
	case *proto.ControlResponse, *proto.ControlRequest, *proto.ControlCancelRequest, *proto.KeepAlive:
		return ks
	case *proto.StreamEvent:
		if x.IsDelta() {
			if x.Event.Delta != nil {
				text.WriteString(x.Event.Delta.Text + x.Event.Delta.Thinking + x.Event.Delta.PartialJSON)
			}
			return ks
		}
		return append(ks, x.Event.Type)
	}
	env := ev.Env()
	k := env.Type
	if env.Subtype != "" {
		k += "/" + env.Subtype
	}
	return append(ks, k)
}
