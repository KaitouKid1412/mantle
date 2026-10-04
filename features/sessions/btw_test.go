package sessions

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func btwView(h *harness) string {
	return ansi.Strip((&btwOverlay{f: h.f}).View(h.ctx, ext.Area{Width: 80, MaxHeight: 20}).Text)
}

func TestBtwThroughEngine(t *testing.T) {
	h := newHarness(t)
	h.eng.supports[subSideQ] = true
	h.eng.reply[subSideQ] = json.RawMessage(`{"response":"It's 42.","synthetic":false}`)
	h.command("btw", "")
	if len(h.ctx.Notices) != 1 || !strings.Contains(h.ctx.Notices[0].Text, "Usage") {
		t.Fatalf("empty: %+v", h.ctx.Notices)
	}
	h.command("btw", "what is six times seven?")
	if !slices.Equal(h.ctx.Opened, []string{DialogBtw}) {
		t.Fatalf("opened = %v", h.ctx.Opened)
	}
	c := h.eng.controls[0]
	fields := string(c.req.(proto.RawRequest).Fields)
	if c.subtype != subSideQ || !strings.Contains(fields, `"question":"what is six times seven?"`) || !strings.Contains(fields, `"history":[]`) {
		t.Fatalf("request = %s %s", c.subtype, fields)
	}
	if v := btwView(h); !strings.Contains(v, "> what is six times seven?") || !strings.Contains(v, "It's 42.") {
		t.Fatalf("overlay:\n%s", v)
	}

	// A follow-up sends the earlier exchange; the overlay is not opened twice.
	h.eng.reply[subSideQ] = json.RawMessage(`{"response":"Fourteen."}`)
	h.command("btw", "and seven times two?")
	fields = string(h.eng.controls[1].req.(proto.RawRequest).Fields)
	if !strings.Contains(fields, `"response":"It's 42."`) || len(h.ctx.Opened) != 1 {
		t.Fatalf("follow-up = %s, opened %v", fields, h.ctx.Opened)
	}
	h.dialog = &btwOverlay{f: h.f}
	h.key(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift})
	if v := btwView(h); !strings.Contains(v, "1/2") || !strings.Contains(v, "It's 42.") {
		t.Fatalf("history:\n%s", v)
	}
	got := fakeClipboard(t)
	h.key(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if *got != "It's 42." {
		t.Fatalf("copied %q", *got)
	}
	h.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if len(h.f.btw) != 0 || h.f.btwOpen {
		t.Fatal("x did not clear")
	}
}

func TestBtwOneShotFallback(t *testing.T) {
	h := newHarness(t)
	var argv []string
	h.f.runSide = func(bin, cwd string, a []string) (string, error) {
		argv = a
		return "A forked answer.\n", nil
	}
	sid := h.ctx.SessionValue.SessionID
	h.command("btw", "why?")
	want := []string{"-p", "--resume", sid, "--fork-session", "--no-session-persistence", "--tools", "", "why?"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %q", argv)
	}
	if v := btwView(h); !strings.Contains(v, "A forked answer.") {
		t.Fatalf("overlay:\n%s", v)
	}

	// The engine refuses side_question: the one-shot answers instead.
	h2 := newHarness(t)
	h2.eng.supports[subSideQ] = true
	h2.eng.replyErr = errors.New("side_question is not supported in this context")
	h2.f.runSide = func(string, string, []string) (string, error) { return "fallback", nil }
	h2.command("btw", "q")
	if v := btwView(h2); !strings.Contains(v, "fallback") {
		t.Fatalf("fallback overlay:\n%s", v)
	}

	// An answer arriving after the overlay closed is announced.
	h3 := newHarness(t)
	h3.f.runSide = func(string, string, []string) (string, error) { return "late", nil }
	h3.f.btwOpen = true
	h3.f.btw = []*btwExchange{{id: 9, question: "q", loading: true}}
	h3.f.btwOpen = false
	h3.run(ext.Msg(btwAnswerMsg{id: 9, answer: "late"}))
	if len(h3.ctx.Notices) != 1 || !strings.Contains(h3.ctx.Notices[0].Text, "/btw shows it") {
		t.Fatalf("notices = %+v", h3.ctx.Notices)
	}
}
