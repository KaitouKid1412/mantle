package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func delta(parent string, index int, typ, text string) *proto.StreamEvent {
	d := &proto.Delta{Type: typ}
	switch typ {
	case proto.DeltaText:
		d.Text = text
	case proto.DeltaThinking:
		d.Thinking = text
	case proto.DeltaInputJSON:
		d.PartialJSON = text
	case proto.DeltaSignature:
		d.Signature = text
	}
	return &proto.StreamEvent{
		Envelope:        proto.Envelope{Type: proto.TypeStreamEvent, Raw: []byte("{}")},
		Event:           proto.StreamPayload{Type: proto.StreamContentBlockDelta, Index: index, Delta: d},
		ParentToolUseID: parent,
	}
}

type sink struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *sink) out(b []tea.Msg) { s.mu.Lock(); s.msgs = append(s.msgs, b...); s.mu.Unlock() }
func (s *sink) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.msgs {
		switch v := m.(type) {
		case ext.EngineEventMsg:
			if se, ok := v.Event.(*proto.StreamEvent); ok {
				d := se.Event.Delta
				out = append(out, fmt.Sprintf("%s%d:%s", se.ParentToolUseID, se.Event.Index, d.Text+d.Thinking+d.PartialJSON+d.Signature))
			} else {
				out = append(out, v.Event.Env().Type)
			}
		default:
			out = append(out, fmt.Sprintf("%T", m))
		}
	}
	return out
}

func TestCoalescerMergesAndKeepsOrder(t *testing.T) {
	var s sink
	c := newCoalescer("main", time.Hour, s.out)
	c.Push(delta("", 0, proto.DeltaText, "a"))
	c.Push(delta("p", 0, proto.DeltaText, "x")) // subagent stream
	c.Push(delta("", 0, proto.DeltaText, "b"))
	c.Push(delta("", 1, proto.DeltaInputJSON, `{"a"`))
	c.Push(delta("", 1, proto.DeltaInputJSON, `:1}`))
	c.Push(delta("", 1, proto.DeltaSignature, "sig")) // not merged: flushes like any non-delta
	c.Push(delta("p", 0, proto.DeltaText, "y"))
	c.Push(&proto.Assistant{Envelope: proto.Envelope{Type: proto.TypeAssistant}})
	c.PushMsg(ext.SessionChangedMsg{})
	c.Push(delta("", 0, proto.DeltaText, "c"))
	if got := strings.Join(s.list(), " "); got != `0:ab p0:x 1:{"a":1} 1:sig p0:y assistant ext.SessionChangedMsg` {
		t.Errorf("got %s", got)
	}
	c.Flush()
	if l := s.list(); l[len(l)-1] != "0:c" {
		t.Errorf("flush: %v", l)
	}
}

func TestCoalescerTimerFlushAndNoAliasing(t *testing.T) {
	var s sink
	c := newCoalescer("main", 5*time.Millisecond, s.out)
	orig := delta("", 0, proto.DeltaText, "he")
	c.Push(orig)
	c.Push(delta("", 0, proto.DeltaText, "llo"))
	time.Sleep(50 * time.Millisecond)
	if got := strings.Join(s.list(), " "); got != "0:hello" {
		t.Errorf("got %q", got)
	}
	if orig.Event.Delta.Text != "he" || orig.Raw == nil {
		t.Error("merge must not modify the pushed event")
	}
	c.Stop()
	c.Push(delta("", 0, proto.DeltaText, "late"))
	c.Flush()
	if len(s.list()) != 1 {
		t.Error("pushed after Stop")
	}
}

func TestCoalescerConcurrentOrder(t *testing.T) {
	// The flush timer and the reader race; non-deltas must never pass earlier deltas.
	for round := 0; round < 50; round++ {
		var s sink
		c := newCoalescer("main", time.Microsecond, s.out)
		for i := 0; i < 200; i++ {
			c.Push(delta("", 0, proto.DeltaText, "."))
			if i%10 == 9 {
				c.Push(&proto.Assistant{Envelope: proto.Envelope{Type: proto.TypeAssistant}})
			}
		}
		c.Stop()
		dots := 0
		for _, k := range s.list() {
			if k == "assistant" {
				if dots%10 != 0 {
					t.Fatalf("round %d: assistant after %d dots", round, dots)
				}
				continue
			}
			dots += strings.Count(k, ".")
		}
		if dots != 200 {
			t.Fatalf("lost deltas: %d", dots)
		}
	}
}

func TestCorrelator(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	c := newCorrelator(func(l []byte) error { mu.Lock(); sent = append(sent, string(l)); mu.Unlock(); return nil })
	last := func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var m map[string]any
		_ = json.Unmarshal([]byte(sent[len(sent)-1]), &m)
		return m
	}

	// Success.
	go func() {
		for {
			mu.Lock()
			n := len(sent)
			mu.Unlock()
			if n > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		id := last()["request_id"].(string)
		c.Complete(proto.ControlResponseBody{Subtype: "success", RequestID: id, Response: json.RawMessage(`{"ok":1}`)})
	}()
	_, resp, err := c.Request(context.Background(), proto.GetSettingsRequest{}, time.Second)
	if err != nil || string(resp) != `{"ok":1}` {
		t.Fatalf("resp=%s err=%v", resp, err)
	}

	// Timeout sends control_cancel_request.
	id, _, err := c.Request(context.Background(), proto.GetUsageRequest{}, 20*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err=%v", err)
	}
	if m := last(); m["type"] != "control_cancel_request" || m["request_id"] != id {
		t.Errorf("cancel not sent: %v", m)
	}
	if c.Complete(proto.ControlResponseBody{Subtype: "success", RequestID: id}) {
		t.Error("late response for a timed-out request was accepted")
	}

	// Close fails pending and future requests.
	done := make(chan error, 1)
	go func() {
		_, _, err := c.Request(context.Background(), proto.ListModelsRequest{}, time.Minute)
		done <- err
	}()
	for c.Len() == 0 {
		time.Sleep(time.Millisecond)
	}
	c.Close(ErrExited)
	if err := <-done; !errors.Is(err, ErrExited) {
		t.Errorf("pending: %v", err)
	}
	if _, _, err := c.Request(context.Background(), proto.ListModelsRequest{}, time.Second); !errors.Is(err, ErrExited) {
		t.Errorf("after close: %v", err)
	}
}

func TestCorrelatorConcurrent(t *testing.T) {
	var c *correlator
	c = newCorrelator(func(l []byte) error {
		var m struct {
			RequestID string `json:"request_id"`
			Type      string `json:"type"`
		}
		_ = json.Unmarshal(l, &m)
		if m.Type == proto.TypeControlRequest {
			go c.Complete(proto.ControlResponseBody{Subtype: "success", RequestID: m.RequestID, Response: json.RawMessage(`"` + m.RequestID + `"`)})
		}
		return nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, resp, err := c.Request(context.Background(), proto.GetSettingsRequest{}, 5*time.Second)
			if err != nil || string(resp) != `"`+id+`"` {
				t.Errorf("id=%s resp=%s err=%v", id, resp, err)
			}
		}()
	}
	wg.Wait()
	if c.Len() != 0 {
		t.Errorf("leaked %d", c.Len())
	}
}

func TestBuildArgs(t *testing.T) {
	got := strings.Join(BuildArgs(ext.SpawnOpts{
		Name: "n", Model: "opus", PermissionMode: "plan", Resume: "-weird-id", ForkSession: true,
		ResumeSessionAt: "u1", ResumeDropsTurn: true, AddDirs: []string{"/a", "/b"},
		Settings: `{"disabledMcpjsonServers":["x"]}`, ExtraArgs: []string{"--effort", "high"},
	}), " ")
	want := strings.Join(BaseArgs, " ") + ` -n n --model opus --permission-mode plan --resume=-weird-id --fork-session --resume-session-at=u1 --resume-drops-turn=u1 --add-dir /a --add-dir /b --settings {"disabledMcpjsonServers":["x"]} --effort high`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := strings.Join(BuildArgs(ext.SpawnOpts{Continue: true, ForkSession: true}), " "); !strings.HasSuffix(got, "--continue --fork-session") {
		t.Errorf("continue: %s", got)
	}
	if got := strings.Join(BuildArgs(ext.SpawnOpts{ForkSession: true}), " "); strings.Contains(got, "fork") {
		t.Errorf("fork without resume: %s", got)
	}
}

func TestBuildEnv(t *testing.T) {
	base := []string{"ANTHROPIC_API_KEY=secret", "PATH=/bin", "CLAUDECODE=1", "NODE_OPTIONS=--x", "DEBUG=1", "CLAUDE_CODE_SIMPLE=1", "CLAUDE_CODE_SAFE_MODE=1", "HOME=/h", "PWD=/old"}
	env := map[string]string{}
	for _, kv := range BuildEnv(base, ext.SpawnOpts{Cwd: "/w", Env: map[string]string{"FOO": "bar"}, UnsetEnv: []string{"ANTHROPIC_API_KEY"}}) {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := env[k]; dup {
			t.Errorf("duplicate %s", k)
		}
		env[k] = v
	}
	for _, k := range []string{"CLAUDECODE", "NODE_OPTIONS", "DEBUG", "CLAUDE_CODE_SIMPLE", "CLAUDE_CODE_SAFE_MODE", "ANTHROPIC_API_KEY"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s not removed", k)
		}
	}
	if env["CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS"] != "1" || env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] != "true" ||
		env["PATH"] != "/bin" || env["FOO"] != "bar" || env["PWD"] != "/w" {
		t.Errorf("env %v", env)
	}
	safe := strings.Join(BuildEnv(base, ext.SpawnOpts{SafeMode: true}), " ")
	if !strings.Contains(safe, "CLAUDE_CODE_SAFE_MODE=1") {
		t.Errorf("safe mode: %s", safe)
	}
}

func TestMantleHomePaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MANTLE_HOME", dir)
	if got := DefaultEngineStatePath(); got != dir+"/state/engines.json" {
		t.Errorf("state path %s", got)
	}
	if got := NewManager(func(tea.Msg) {}).runDir(); got != dir+"/run" {
		t.Errorf("run dir %s", got)
	}
	t.Setenv("MANTLE_HOME", "")
	home, _ := os.UserHomeDir()
	if got := DefaultEngineStatePath(); got != home+"/.mantle/state/engines.json" {
		t.Errorf("default state path %s", got)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		v, min string
		want   bool
	}{
		{"2.1.288", "2.1.288", true},
		{"2.1.289", "2.1.288", true},
		{"2.2.0", "2.1.288", true},
		{"2.1.287", "2.1.288", false},
		{"2.1.288 (Claude Code)", "2.1.288", true},
		{"", "2.1.288", false},
		{"3", "2.1.288", true},
	}
	for _, c := range cases {
		if got := versionAtLeast(c.v, c.min); got != c.want {
			t.Errorf("%q >= %q: %v", c.v, c.min, got)
		}
	}
}
