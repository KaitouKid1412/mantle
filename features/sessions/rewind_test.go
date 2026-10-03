package sessions

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func uuidPrompt(uuid, text string) *ext.Item {
	return &ext.Item{ID: "user:" + uuid, Key: ext.KeyUserPrompt, State: ext.Done,
		Data: &proto.User{Envelope: proto.Envelope{UUID: uuid}, Message: proto.UserMessage{Role: "user", Content: proto.TextContent(text)}}}
}

// rewindHarness is a harness on the plain fixture session with its two prompts.
func rewindHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.ctx.SessionValue.SessionID = sidPlain
	h.ctx.TranscriptV = &fakeTranscript{items: []*ext.Item{
		uuidPrompt(u("1u", 1), "Explain the build"),
		textItem("a", "The build uses make."),
		uuidPrompt(u("1u", 2), "And the tests?"),
	}}
	h.eng.supports[proto.SubRewindFiles] = true
	h.eng.reply[proto.SubRewindFiles] = json.RawMessage(`{"canRewind":true,"filesChanged":["a.go","b.go"],"insertions":10,"deletions":4}`)
	return h
}

func TestRewindSelector(t *testing.T) {
	h := rewindHarness(t)
	if _, ok := h.r.Command("rewind"); !ok || len(h.r.Actions) == 0 || h.r.Actions[0].ID != ActRewind {
		t.Fatalf("registration: %+v", h.r.Actions)
	}
	if c, _ := h.r.Command("rewind"); !slices.Equal(c.Aliases, []string{"checkpoint", "undo"}) {
		t.Fatalf("aliases = %v", c.Aliases)
	}
	d := h.openDialog(DialogRewind, nil).(*rewindSelector)
	if len(d.entries) != 2 || d.sel != 1 {
		t.Fatalf("entries = %+v sel %d", d.entries, d.sel)
	}
	if cs := d.KeyContexts(); !slices.Equal(cs, []string{ext.ContextMessageSelector, ext.ContextSelect}) {
		t.Fatalf("contexts = %v", cs)
	}
	v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 80, MaxHeight: 20}).Text)
	if !strings.Contains(v, "› And the tests?") || !strings.Contains(v, "  Explain the build") {
		t.Fatalf("view:\n%s", v)
	}

	// Choosing the latest prompt runs a dry run and opens the options.
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if c := h.eng.controls[0]; c.subtype != proto.SubRewindFiles || !c.req.(proto.RewindFilesRequest).DryRun ||
		c.req.(proto.RewindFilesRequest).UserMessageID != u("1u", 2) {
		t.Fatalf("dry run = %+v", h.eng.controls)
	}
	if !slices.Contains(h.ctx.Opened, DialogRewindOptions) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
}

func openOptions(t *testing.T, h *harness, uuid, text string) *choiceDialog {
	t.Helper()
	h.run(h.f.rewindDryRun(h.ctx, rewindTarget{uuid: uuid, text: text}))
	var target rewindTarget
	for _, m := range h.out {
		if dr, ok := m.(rewindDryRunMsg); ok {
			target = dr.t
		}
	}
	return h.openDialog(DialogRewindOptions, target).(*choiceDialog)
}

func TestRewindConversation(t *testing.T) {
	h := rewindHarness(t)
	d := openOptions(t, h, u("1u", 2), "And the tests?")
	v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 80}).Text)
	if !strings.Contains(v, "2 files changed since then (+10 −4)") || !strings.Contains(v, "Summarize from here") {
		t.Fatalf("options:\n%s", v)
	}
	h.reset()
	h.key(tea.KeyPressMsg{Code: '2', Text: "2"}) // Restore conversation
	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 || start[0].Opts.Resume != sidPlain || start[0].Opts.ResumeSessionAt != u("1s", 1) || start[0].Opts.Model != "opus" {
		t.Fatalf("start = %+v", start)
	}
	hist := find[ext.TranscriptHistoryMsg](h)
	if len(hist) != 1 || !hist[0].Reset || len(hist[0].Items) != 4 || hist[0].Items[3].ID != "result:"+u("1s", 1) {
		t.Fatalf("history = %d items", len(hist[0].Items))
	}
	if set := find[ext.EditorSetTextMsg](h); len(set) != 1 || set[0].Text != "And the tests?" {
		t.Fatalf("prompt not put back: %+v", set)
	}
}

func TestRewindCodeAndConversation(t *testing.T) {
	h := rewindHarness(t)
	openOptions(t, h, u("1u", 2), "And the tests?")
	h.eng.controls = nil
	h.reset()
	h.key(tea.KeyPressMsg{Code: '1', Text: "1"})
	if len(h.eng.controls) != 1 || h.eng.controls[0].req.(proto.RewindFilesRequest).DryRun {
		t.Fatalf("controls = %+v", h.eng.controls)
	}
	if len(find[ext.EngineStartMsg](h)) != 1 {
		t.Fatal("conversation not restored after the code")
	}
	if !strings.Contains(h.ctx.Notices[0].Text, "Code restored") {
		t.Fatalf("notices = %+v", h.ctx.Notices)
	}

	// Code only.
	h2 := rewindHarness(t)
	openOptions(t, h2, u("1u", 2), "x")
	h2.reset()
	h2.key(tea.KeyPressMsg{Code: '3', Text: "3"})
	if len(find[ext.EngineStartMsg](h2)) != 0 || !strings.Contains(h2.ctx.Notices[0].Text, "Code restored") {
		t.Fatalf("code only: %v %+v", h2.out, h2.ctx.Notices)
	}
}

func TestRewindFirstPromptAndCheckpointsOff(t *testing.T) {
	h := newHarness(t)
	h.ctx.SessionValue.SessionID = sidCompact
	h.run(h.f.restoreConversation(h.ctx, rewindTarget{uuid: u("3u", 1), text: "Start a long task"}))
	if !slices.Equal(h.eng.sent, []string{"/clear"}) {
		t.Fatalf("first prompt: sent %q", h.eng.sent)
	}

	h2 := rewindHarness(t)
	h2.ctx.SettingsV = exttest.NewSettings(map[string]any{"fileCheckpointingEnabled": false})
	d := openOptions(t, h2, u("1u", 2), "x")
	if !d.choices[0].disabled || !d.choices[2].disabled || d.choices[1].disabled {
		t.Fatalf("choices = %+v", d.choices)
	}
	if !strings.Contains(d.subtitle, "fileCheckpointingEnabled") {
		t.Fatalf("subtitle = %q", d.subtitle)
	}
}

func TestRewindRestoresPreClearSession(t *testing.T) {
	h := rewindHarness(t)
	h.f.cleared = []string{sidTools}
	d := h.openDialog(DialogRewind, nil).(*rewindSelector)
	if len(d.entries) != 3 || d.entries[0].cleared != sidTools {
		t.Fatalf("entries = %+v", d.entries)
	}
	h.key(tea.KeyPressMsg{Code: tea.KeyHome})
	d.sel = 0
	h.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	start := find[ext.EngineStartMsg](h)
	if len(start) != 1 || start[0].Opts.Resume != sidTools || start[0].Opts.ResumeSessionAt != "" {
		t.Fatalf("start = %+v", start)
	}
}
