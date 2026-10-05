package transcript

import (
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// TestAnswerKeepsLineBreaks: a single newline in an answer is a line break,
// as claude shows it, not a space.
func TestAnswerKeepsLineBreaks(t *testing.T) {
	g := newRig(t, 81)
	g.send(`{"type":"assistant","uuid":"a1","message":{"id":"m","content":[{"type":"text","text":"Here is a list.\n\nLine 01 of it.\nLine 02 of it.\nLine 03 of it."}]}}`)
	want := "\n⏺ Here is a list.\n\n  Line 01 of it.\n  Line 02 of it.\n  Line 03 of it."
	if got := join(g.printed()); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestHistoryTurnLine: results rebuilt from a session file's turn-duration
// records (plan 06) get the turn line, timed by the record.
func TestHistoryTurnLine(t *testing.T) {
	g := newRig(t, 81)
	g.c.SettingsV = exttest.NewSettings(map[string]any{"timeZone": "UTC", "timeFormat": "24h"})
	g.deliver(ext.SettingsMsg{})
	at := time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC)
	g.deliver(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{
		{ID: "user:h1", Key: ext.KeyUserPrompt, Data: "an old question"},
		{ID: "result:h2", Key: KeyResult, Data: &proto.Result{Envelope: proto.Envelope{Type: proto.TypeResult, Subtype: proto.ResultSuccess, UUID: "h2"}, DurationMS: 4000}, Start: at},
	}})
	got := join(g.printed())
	if !strings.Contains(got, " for 4s · done 09:30") {
		t.Fatalf("no turn line for the resumed turn:\n%s", got)
	}
	// A local command's result still gets none.
	g.send(`{"type":"result","subtype":"success","uuid":"r3","duration_ms":30,"is_error":false,"num_turns":0,"local_command":"context","total_cost_usd":0}`)
	if strings.Count(join(g.printed()), "done ") != 1 {
		t.Fatalf("local command got a turn line:\n%s", join(g.printed()))
	}
}
