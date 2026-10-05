package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// recEngine records prompts sent to it.
type recEngine struct{ sent []string }

type sentMsg struct{}

func (e *recEngine) Send(p ext.Prompt) tea.Cmd {
	e.sent = append(e.sent, p.Blocks[0].Text)
	return ext.Msg(sentMsg{})
}
func (e *recEngine) Interrupt(bool) tea.Cmd        { return nil }
func (e *recEngine) Control(string, any) tea.Cmd   { return nil }
func (e *recEngine) Supports(string) bool          { return true }
func (e *recEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }

func TestCoreSendQueuesUntilAttach(t *testing.T) {
	r, _ := newRoot(t)
	drive(t, r, cmdMsgs(r.Ctx().Submit(ext.Draft{Text: "typed early", Mode: "prompt"}))...)
	drive(t, r, cmdMsgs(r.Ctx().Submit(ext.Draft{Text: "and again", Mode: "prompt"}))...)
	if len(r.pendingPrompts) != 2 {
		t.Fatalf("pending = %d, want 2 queued before attach", len(r.pendingPrompts))
	}
	e := &recEngine{}
	drive(t, r, ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: e})
	if len(e.sent) != 2 || e.sent[0] != "typed early" || e.sent[1] != "and again" {
		t.Fatalf("sent on attach = %v", e.sent)
	}
	if len(r.pendingPrompts) != 0 || r.noticeSeq("core.send") != 0 {
		t.Fatal("queue and waiting notice must clear after the flush")
	}
	// After attach, prompts go straight out.
	drive(t, r, cmdMsgs(r.Ctx().Submit(ext.Draft{Text: "later", Mode: "prompt"}))...)
	if len(e.sent) != 3 || e.sent[2] != "later" {
		t.Fatalf("sent = %v", e.sent)
	}
	// A builder engine attaching must not flush the main queue.
	r.engines = map[string]ext.Engine{}
	drive(t, r, cmdMsgs(r.Ctx().Submit(ext.Draft{Text: "queued", Mode: "prompt"}))...)
	drive(t, r, ext.EngineAttachMsg{EngineID: "builder", Engine: &recEngine{}})
	if len(r.pendingPrompts) != 1 {
		t.Fatal("only the main engine's attach flushes the queue")
	}
}
