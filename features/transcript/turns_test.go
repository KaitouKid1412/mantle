package transcript

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// TestTurnLedgerRestoresResumedTurnLines: the headless engine writes no
// turn-duration records, so the transcript remembers each turn and puts its
// line back when the session's history is loaded again.
func TestTurnLedgerRestoresResumedTurnLines(t *testing.T) {
	g := newRig(t, 81)
	g.c.SettingsV = exttest.NewSettings(map[string]any{"timeZone": "UTC", "timeFormat": "24h"})
	g.deliver(ext.SettingsMsg{})
	g.send(`
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"Remember a word"}}
{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":"Remember kumquat."}]}}
{"type":"result","subtype":"success","uuid":"r1","duration_ms":3000,"is_error":false,"num_turns":1,"total_cost_usd":0}
`)
	// A new process: the same state store, a fresh feature, history from
	// plan 06's reader (no turn-duration record for this session).
	g2 := newRig(t, 81)
	g2.c = g.c // shares the KV store
	g2.f.ledger = nil
	g2.c.Printed = nil
	g2.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Reset: true, Items: []*ext.Item{
		{ID: "user:u1", Key: ext.KeyUserPrompt, Data: "Remember a word"},
		{ID: "txt:a1:0", Key: ext.KeyAssistantText, Data: &proto.ContentBlock{Type: proto.BlockText, Text: "Remember kumquat."}},
	}})
	got := join(g2.printed())
	if !strings.Contains(got, "⏺ Remember kumquat.\n\n✻ ") || !strings.Contains(got, " for 3s · done ") {
		t.Fatalf("resumed transcript lacks the turn line:\n%s", got)
	}

	// A session file that has its own turn line (interactive sessions) keeps
	// just that one.
	g3 := newRig(t, 81)
	g3.c = g.c
	g3.f.ledger = nil
	g3.c.Printed = nil
	own := &ext.Item{ID: "result:t1", Key: KeyResult, Data: &proto.Result{DurationMS: 3000}}
	g3.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Reset: true, Items: []*ext.Item{
		{ID: "txt:a1:0", Key: ext.KeyAssistantText, Data: &proto.ContentBlock{Type: proto.BlockText, Text: "Remember kumquat."}},
		own,
	}})
	if n := strings.Count(join(g3.printed()), " for 3s"); n != 1 {
		t.Fatalf("%d turn lines, want 1:\n%s", n, join(g3.printed()))
	}
}

func TestAnswerUUID(t *testing.T) {
	for _, tc := range []struct {
		it   *ext.Item
		want string
		ok   bool
	}{
		{&ext.Item{ID: "txt:abc-1:0", Key: ext.KeyAssistantText}, "abc-1", true},
		{&ext.Item{ID: "txt:abc-1:0", Key: ext.KeyAssistantText, ParentID: "ag"}, "", false},
		{&ext.Item{ID: "blk::m:0", Key: ext.KeyAssistantText}, "", false},
		{&ext.Item{ID: "user:x", Key: ext.KeyUserPrompt}, "", false},
	} {
		if got, ok := answerUUID(tc.it); got != tc.want || ok != tc.ok {
			t.Errorf("answerUUID(%s) = %q %v", tc.it.ID, got, ok)
		}
	}
}
