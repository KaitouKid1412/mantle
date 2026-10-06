package transcript

import (
	"errors"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// echoItem is what the input feature sends on Enter (features/input echoPrompt).
func echoItem(uuid string, key ext.ContentKey, text string, st ext.ItemState) ext.TranscriptHistoryMsg {
	in := proto.NewUserInput(uuid, proto.Text(text))
	return ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{{
		ID: "user:" + uuid, Key: key, State: st,
		Data: &proto.User{Envelope: proto.Envelope{Type: proto.TypeUser, UUID: uuid}, Message: in.Message},
	}}}
}

func count(lines []string, s string) int {
	return strings.Count(join(lines), s)
}

// TestPromptEchoSettledByReplay: the prompt shows on Enter (live area) and the
// replay with the same uuid settles it in place: one line, printed once.
func TestPromptEchoSettledByReplay(t *testing.T) {
	g := newRig(t, 81)
	g.deliver(echoItem("p1", ext.KeyUserPrompt, "Explain the plan", ext.Running))
	if got := join(g.live(10)); !strings.Contains(got, "❯ Explain the plan") {
		t.Fatalf("live after Enter = %q", got)
	}
	if len(g.printed()) != 0 {
		t.Fatalf("printed before the replay: %q", g.printed())
	}
	// Hooks and the like arrive before the replay: they wait behind the prompt.
	g.send(`{"type":"system","subtype":"informational","uuid":"n1","content":"A note before the turn"}`)
	g.send(`{"type":"user","uuid":"p1","isReplay":true,"message":{"role":"user","content":"Explain the plan"}}
{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"text","text":"Here it is."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":1500,"is_error":false,"num_turns":1,"total_cost_usd":0}`)
	got := g.printed()
	if n := count(got, "Explain the plan"); n != 1 {
		t.Fatalf("prompt printed %d times:\n%s", n, join(got))
	}
	all := join(got)
	if p, a := strings.Index(all, "❯ Explain the plan"), strings.Index(all, "⏺ Here it is."); p < 0 || a < p {
		t.Fatalf("order:\n%s", all)
	}
	if n := len(g.f.store.Items()); n != 4 { // prompt, note, answer, result
		t.Fatalf("%d items", n)
	}
	if len(g.f.store.echoes) != 0 {
		t.Fatalf("echo still pending: %v", g.f.store.echoes)
	}
}

// TestPromptEchoWithoutReplay: a turn that starts without replaying the prompt
// (or an engine that exits) keeps the echo and does not hold the output behind it.
func TestPromptEchoWithoutReplay(t *testing.T) {
	g := newRig(t, 81)
	g.deliver(echoItem("p1", ext.KeyUserPrompt, "No replay", ext.Running))
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"text","text":"Answer."}]}}`)
	if got := join(g.printed()); !strings.Contains(got, "❯ No replay") || !strings.Contains(got, "⏺ Answer.") {
		t.Fatalf("printed = %q", got)
	}
	// A late replay still settles it in place.
	g.send(`{"type":"user","uuid":"p1","isReplay":true,"message":{"role":"user","content":"No replay"}}`)
	if n := count(g.printed(), "No replay"); n != 1 {
		t.Fatalf("prompt printed %d times", n)
	}

	// An engine stopped before the replay keeps it.
	g2 := newRig(t, 81)
	g2.deliver(echoItem("p2", ext.KeyUserPrompt, "Engine stopped", ext.Running))
	g2.deliver(ext.EngineExitedMsg{EngineID: ext.MainEngine})
	if got := join(g2.printed()); !strings.Contains(got, "❯ Engine stopped") {
		t.Fatalf("printed = %q", got)
	}
}

// TestPromptEchoSendFailed: the engine never got the prompt: the echo goes (the
// input feature puts the text back in the box).
func TestPromptEchoSendFailed(t *testing.T) {
	g := newRig(t, 81)
	g.deliver(echoItem("p1", ext.KeyUserPrompt, "Lost prompt", ext.Running))
	g.deliver(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.TypeUser, RequestID: "other", Err: errors.New("broken pipe")})
	if got := join(g.live(10)); !strings.Contains(got, "Lost prompt") {
		t.Fatalf("another prompt's failure dropped the echo: %q", got)
	}
	g.deliver(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.TypeUser, RequestID: "p1", Err: errors.New("broken pipe")})
	if live, printed := g.live(10), g.printed(); len(live) != 0 || len(printed) != 0 || len(g.f.store.Items()) != 0 {
		t.Fatalf("echo kept: live %q printed %q", live, printed)
	}

	// The engine failed to start, or died, before taking the prompt up.
	g2 := newRig(t, 81)
	g2.deliver(echoItem("p2", ext.KeyUserPrompt, "Never taken", ext.Running))
	g2.deliver(echoItem("b2", ext.KeyUserBash, "<bash-input>make</bash-input>", ext.Running))
	g2.deliver(ext.EngineExitedMsg{EngineID: ext.MainEngine, Err: errors.New("exit 1")})
	if got := join(g2.live(10)); strings.Contains(got, "Never taken") || !strings.Contains(got, "! make") {
		t.Fatalf("live = %q", got)
	}
}

// TestBashEcho: a "!" command shows at once with "Running…", gets its output
// from the input feature when it finishes, and the replay changes nothing.
func TestBashEcho(t *testing.T) {
	g := newRig(t, 81)
	g.deliver(echoItem("b1", ext.KeyUserBash, "<bash-input>sleep 1; echo hi</bash-input>", ext.Running))
	if got := join(g.live(10)); !strings.Contains(got, "! sleep 1; echo hi\n  ⎿  Running…") {
		t.Fatalf("live while running = %q", got)
	}
	// A turn ending meanwhile does not finish a running command.
	g.send(`{"type":"result","subtype":"success","uuid":"r0","duration_ms":10,"is_error":false,"num_turns":0,"total_cost_usd":0}`)
	if len(g.printed()) != 0 {
		t.Fatalf("printed while running: %q", g.printed())
	}
	in := proto.NewUserInput("b1", proto.Text("<bash-input>sleep 1; echo hi</bash-input>"), proto.Text("<bash-stdout>hi</bash-stdout><bash-stderr></bash-stderr>"))
	g.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{{
		ID: "user:b1", Key: ext.KeyUserBash, State: ext.Done,
		Data: &proto.User{Envelope: proto.Envelope{Type: proto.TypeUser, UUID: "b1"}, Message: in.Message},
	}}})
	want := "! sleep 1; echo hi\n  ⎿  hi"
	if got := join(g.printed()); !strings.Contains(got, want) || strings.Contains(got, "Running") {
		t.Fatalf("printed = %q", got)
	}
	g.send(`{"type":"user","uuid":"b1","isReplay":true,"message":{"role":"user","content":[{"type":"text","text":"<bash-input>sleep 1; echo hi</bash-input>"},{"type":"text","text":"<bash-stdout>hi</bash-stdout><bash-stderr></bash-stderr>"}]}}`)
	if n := count(g.printed(), "echo hi"); n != 1 || len(g.f.store.Items()) != 2 {
		t.Fatalf("command printed %d times, %d items", n, len(g.f.store.Items()))
	}
}

// TestHistoryStillFinished: history items (plan 06) are never echoes, whatever
// their state, and a duplicate ID is still skipped.
func TestHistoryStillFinished(t *testing.T) {
	g := newRig(t, 81)
	g.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{
		{ID: "user:h1", Key: ext.KeyUserPrompt, Data: "From the file"},
		{ID: "txt:a:0", Key: ext.KeyAssistantText, Data: &proto.ContentBlock{Type: proto.BlockText, Text: "Old answer."}, State: ext.Running},
	}})
	g.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{{ID: "user:h1", Key: ext.KeyUserPrompt, Data: "Changed"}}})
	got := join(g.printed())
	if !strings.Contains(got, "❯ From the file") || !strings.Contains(got, "⏺ Old answer.") || strings.Contains(got, "Changed") {
		t.Fatalf("printed = %q", got)
	}
}
