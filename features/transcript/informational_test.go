package transcript

import (
	"strings"
	"testing"
)

// TestClassifierBillingNoticeHidden: the engine sends the auto-mode classifier
// billing notice as a system/informational warning, which interactive claude
// shows as a dialog, never as a transcript line. Other informational messages
// still show.
func TestClassifierBillingNoticeHidden(t *testing.T) {
	g := newRig(t, 81)
	g.send(`
{"type":"user","uuid":"u1","isReplay":true,"message":{"role":"user","content":"plan the fix"}}
{"type":"system","subtype":"informational","uuid":"i1","level":"warning","isMeta":false,"content":"We're changing auto mode billing. However, this session isn't eligible. To fix it, see: https://code.claude.com/docs/en/auto-mode-classifier-billing"}
{"type":"system","subtype":"informational","uuid":"i2","level":"warning","content":"Context is 80% full; consider /compact."}
`)
	got := join(g.printed())
	if strings.Contains(got, "auto mode billing") {
		t.Fatalf("billing notice in the transcript:\n%s", got)
	}
	if !strings.Contains(got, "Context is 80% full") {
		t.Fatalf("other informational messages must still show:\n%s", got)
	}
	// The ctrl+o viewer keeps it.
	v := g.viewer()
	viewText(g, v)
	if !strings.Contains(strings.Join(v.plain, "\n"), "auto mode billing") {
		t.Fatal("the viewer lost the notice")
	}
}
