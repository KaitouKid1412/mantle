package input

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// TestNativeCommandRunsMidTurn: native commands (/model, /effort, /fast) run at once
// while a turn runs instead of queueing, so their control requests reach the engine
// mid-turn and apply from its next request (PARITY TC-13).
func TestNativeCommandRunsMidTurn(t *testing.T) {
	r := newRig(t, nil)
	ran := ""
	r.c.CommandList = append(r.c.CommandList, ext.Command{Name: "native", Source: ext.SourceBuiltin,
		Run: func(_ ext.Ctx, args string) tea.Cmd { ran = args; return nil }})
	r.keys("'first'", "enter")
	if !r.s.busy {
		t.Fatal("a sent prompt makes the engine busy")
	}
	r.keys("'/native opus'", "enter")
	if ran != "opus" || len(r.eng.prompts()) != 1 || len(r.s.queue) != 0 {
		t.Fatalf("ran %q, prompts %d, queued %d", ran, len(r.eng.prompts()), len(r.s.queue))
	}
}

// TestBusyPromptJoinsTheTurn: a message typed during a turn goes out as "next", so the
// engine folds it in at the next tool boundary; commands still wait for the turn to
// end (PARITY TC-06).
func TestBusyPromptJoinsTheTurn(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'first'", "enter")
	r.keys("'also this'", "enter")
	r.keys("'/compact'", "esc", "enter")
	ps := r.eng.prompts()
	if len(ps) != 3 || ps[0].Priority != "" || ps[1].Priority != proto.PriorityNext || ps[2].Priority != proto.PriorityLater {
		t.Fatalf("priorities: %+v", ps)
	}
	if len(r.s.queue) != 2 {
		t.Fatalf("both wait in the queue until the engine starts them: %d", len(r.s.queue))
	}
}

// TestSendNowSendsQueue: chat:sendNow on an empty prompt while messages are queued
// takes them back and sends them again as one "now" prompt, as Claude Code 2.1.289
// does (the engine then backgrounds a running shell command and runs them at once).
// Plain Enter on an empty prompt leaves the queue alone (PARITY TC-10).
func TestSendNowSendsQueue(t *testing.T) {
	r := newRig(t, nil)
	refuse := ""
	r.eng.reply = func(sub string, req any) (json.RawMessage, error) {
		if c, ok := req.(proto.CancelAsyncMessageRequest); ok && c.MessageUUID != refuse {
			return json.RawMessage(`{"cancelled":true}`), nil
		}
		return json.RawMessage(`{"cancelled":false}`), nil
	}
	r.action(ext.ActChatSendNow) // nothing running: nothing to do
	r.keys("'first'", "enter")
	r.action(ext.ActChatSendNow) // running, nothing queued
	r.keys("'second'", "enter")
	r.keys("'third'", "enter")
	r.keys("enter") // plain Enter on the empty prompt: the queue stays
	if n := len(r.eng.controlsOf(proto.SubCancelAsyncMessage)); n != 0 || len(r.s.queue) != 2 {
		t.Fatalf("enter touched the queue: %d cancels, %d queued", n, len(r.s.queue))
	}
	ps := r.eng.prompts()
	r.action(ext.ActChatSendNow)
	cs := r.eng.controlsOf(proto.SubCancelAsyncMessage)
	if len(cs) != 2 || cs[0].req.(proto.CancelAsyncMessageRequest).MessageUUID != ps[1].UUID ||
		cs[1].req.(proto.CancelAsyncMessageRequest).MessageUUID != ps[2].UUID {
		t.Fatalf("take back %+v", cs)
	}
	all := r.eng.prompts()
	if len(all) != 4 || len(r.s.queue) != 0 {
		t.Fatalf("prompts %d, queued %d", len(all), len(r.s.queue))
	}
	now := all[3]
	if now.Priority != proto.PriorityNow || len(now.Blocks) != 2 || now.Blocks[0].Text != "second" || now.Blocks[1].Text != "third" {
		t.Fatalf("send-now prompt %+v", now)
	}

	// A message the engine already started is not sent twice.
	r.keys("'fourth'", "enter")
	r.keys("'fifth'", "enter")
	all = r.eng.prompts()
	refuse = all[4].UUID
	r.action(ext.ActChatSendNow)
	all2 := r.eng.prompts()
	if len(all2) != 7 || len(all2[6].Blocks) != 1 || all2[6].Blocks[0].Text != "fifth" {
		t.Fatalf("after a refused take-back: %+v", all2[len(all2)-1])
	}
}
