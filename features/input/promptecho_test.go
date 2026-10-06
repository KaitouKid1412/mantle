package input

import (
	"errors"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// stubTranscript stands in for plan 03's store: echoes need a transcript.
type stubTranscript struct{}

func (stubTranscript) Items() []*ext.Item   { return nil }
func (stubTranscript) Get(string) *ext.Item { return nil }
func (stubTranscript) Committed() int       { return 0 }
func echoRig(t *testing.T, claude map[string]any) *rig {
	r := newRig(t, claude)
	r.c.TranscriptV = stubTranscript{}
	return r
}

// echoes returns the transcript items the feature sent, in order.
func echoes(r *rig) []*ext.Item {
	var out []*ext.Item
	for _, m := range r.msgs {
		if h, ok := m.(ext.TranscriptHistoryMsg); ok && !h.Reset {
			out = append(out, h.Items...)
		}
	}
	return out
}

func echoText(it *ext.Item) string {
	u, _ := it.Data.(*proto.User)
	if u == nil {
		return ""
	}
	if u.Message.Content.Blocks == nil {
		return u.Message.Content.Text
	}
	s := ""
	for _, b := range u.Message.Content.Blocks {
		s += b.Text
	}
	return s
}

// TestPromptEchoOnEnter: a prompt sent to an idle engine goes into the
// transcript at once, as "user:<uuid>" with the uuid it is sent with; one
// typed during a turn waits in the queue; commands are not echoed.
func TestPromptEchoOnEnter(t *testing.T) {
	r := echoRig(t, nil)
	r.keys("'first question'", "enter")
	ps, es := r.eng.prompts(), echoes(r)
	if len(ps) != 1 || len(es) != 1 {
		t.Fatalf("%d prompts, %d echoes", len(ps), len(es))
	}
	if e := es[0]; e.ID != "user:"+ps[0].UUID || e.Key != ext.KeyUserPrompt || e.State != ext.Running || echoText(e) != "first question" {
		t.Fatalf("echo %+v %q", e, echoText(e))
	}
	r.keys("'during the turn'", "enter")
	if len(echoes(r)) != 1 || len(r.s.queue) != 1 {
		t.Fatalf("a queued prompt was echoed: %d echoes, %d queued", len(echoes(r)), len(r.s.queue))
	}

	r2 := echoRig(t, nil)
	r2.keys("'/compact'", "esc", "enter")
	if len(r2.eng.prompts()) != 1 || len(echoes(r2)) != 0 {
		t.Fatalf("a command was echoed: %d", len(echoes(r2)))
	}
}

// TestPromptEchoWhileStarting: the first prompt typed while the engine starts
// shows in the transcript (it runs first), not in the queue; later ones queue.
func TestPromptEchoWhileStarting(t *testing.T) {
	r := echoRig(t, nil)
	eng := r.eng
	delete(r.c.Engines, ext.MainEngine)
	r.keys("'early one'", "enter")
	r.keys("'early two'", "enter")
	if es := echoes(r); len(es) != 1 || echoText(es[0]) != "early one" {
		t.Fatalf("echoes %v", es)
	}
	if len(r.s.queue) != 1 || r.s.queue[0].text != "early two" {
		t.Fatalf("queue %+v", r.s.queue)
	}
	r.c.Engines[ext.MainEngine] = eng
	r.run(r.s.update(r.c, ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: eng}))
	if ps := eng.prompts(); len(ps) != 2 || "user:"+ps[0].UUID != echoes(r)[0].ID {
		t.Fatalf("sent %+v", ps)
	}
}

// TestPromptEchoSendFailed: a prompt the engine never got goes back into the
// box (the transcript drops its echo), as when the engine is not running.
func TestPromptEchoSendFailed(t *testing.T) {
	r := echoRig(t, nil)
	r.keys("'lost words'", "enter")
	u := r.eng.prompts()[0].UUID
	r.run(r.s.update(r.c, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.TypeUser, RequestID: u, Err: errors.New("broken pipe")}))
	if r.text() != "lost words" || r.s.busy {
		t.Fatalf("box %q busy %v", r.text(), r.s.busy)
	}

	// Something new typed meanwhile stays; the prompt is in history.
	r1 := echoRig(t, nil)
	r1.keys("'first try'", "enter")
	u = r1.eng.prompts()[0].UUID
	r1.keys("'new text'")
	r1.run(r1.s.update(r1.c, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.TypeUser, RequestID: u, Err: errors.New("engine down")}))
	if r1.text() != "new text" || len(r1.s.histAll) == 0 || r1.s.histAll[len(r1.s.histAll)-1].Display != "first try" {
		t.Fatalf("box %q history %+v", r1.text(), r1.s.histAll)
	}

	// A queued prompt the engine rejects leaves the queue and comes back too.
	rq := echoRig(t, nil)
	rq.keys("'running'", "enter", "'queued one'", "enter")
	u = rq.eng.prompts()[1].UUID
	rq.run(rq.s.update(rq.c, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.TypeUser, RequestID: u, Err: errors.New("engine down")}))
	if rq.text() != "queued one" || len(rq.s.queue) != 0 {
		t.Fatalf("box %q queue %+v", rq.text(), rq.s.queue)
	}

	// Once the engine took it up (its replay), a later failure leaves the box alone.
	r2 := echoRig(t, nil)
	r2.keys("'kept going'", "enter")
	u = r2.eng.prompts()[0].UUID
	r2.run(r2.s.update(r2.c, ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.User{
		Envelope: proto.Envelope{Type: proto.TypeUser, UUID: u}, IsReplay: true,
		Message: proto.UserMessage{Role: "user", Content: proto.TextContent("kept going")},
	}}))
	r2.run(r2.s.update(r2.c, ext.EngineExitedMsg{EngineID: ext.MainEngine, Err: errors.New("exit 1")}))
	if r2.text() != "" {
		t.Fatalf("box %q", r2.text())
	}

	// The engine died before taking it up: back in the box.
	r3 := echoRig(t, nil)
	r3.keys("'never taken'", "enter")
	r3.run(r3.s.update(r3.c, ext.EngineExitedMsg{EngineID: ext.MainEngine, Err: errors.New("exit 1")}))
	if r3.text() != "never taken" {
		t.Fatalf("box %q", r3.text())
	}
}

// TestBashEchoOnEnter: a "!" command shows at once (running), then with its
// output, under the uuid its output is sent with; no notices.
func TestBashEchoOnEnter(t *testing.T) {
	r := echoRig(t, nil)
	r.s.cwd = t.TempDir()
	r.keys("'!'", "'echo hi'", "enter")
	ps, es := r.eng.prompts(), echoes(r)
	if len(ps) != 1 || len(es) != 2 {
		t.Fatalf("%d prompts, %d echoes", len(ps), len(es))
	}
	id := "user:" + ps[0].UUID
	if es[0].ID != id || es[0].Key != ext.KeyUserBash || es[0].State != ext.Running || echoText(es[0]) != "<bash-input>echo hi</bash-input>" {
		t.Fatalf("first echo %+v %q", es[0], echoText(es[0]))
	}
	if es[1].ID != id || es[1].State != ext.Done || echoText(es[1]) != "<bash-input>echo hi</bash-input><bash-stdout>hi\n</bash-stdout><bash-stderr></bash-stderr>" {
		t.Fatalf("second echo %+v %q", es[1], echoText(es[1]))
	}
	for _, n := range r.c.Notices {
		if n.Key == "input.bash" {
			t.Fatalf("notice %+v", n)
		}
	}
}
