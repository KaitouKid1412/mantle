package editor

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ui/editor/vim"
)

func vimEditor(t *testing.T, start string) *Editor {
	t.Helper()
	e := fromCursor(t, start)
	e.SetVim(true, map[string]string{"jj": "<Esc>"})
	return e
}

func send(e *Editor, ks string) (bool, tea.Cmd) {
	var ok bool
	var cmd tea.Cmd
	for _, k := range keys(ks) {
		ok, cmd = e.HandleKey(k)
	}
	return ok, cmd
}

func TestVimOnEditor(t *testing.T) {
	tests := []struct{ start, keys, want string }{
		{"foo bar|", "esc b d w", "foo| "},
		{"foo bar|", "esc 0 c w x y z esc", "xy|z bar"},
		{"one\ntwo|", "esc k d d", "|two"},
		{"abc|", "esc x u", "ab|c"},
		{"ab|", "esc i x y esc u", "a|b"},
		{"ab|", "j j", "a|b"},
		{"x|", "esc y y p", "x\n|x"},
		{"hello world|", "esc 0 v e y $ p", "hello worldhell|o"},
	}
	for _, tt := range tests {
		t.Run(tt.start+"/"+tt.keys, func(t *testing.T) {
			e := vimEditor(t, tt.start)
			send(e, tt.keys)
			if got := withCursor(e); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVimChipsAreWords(t *testing.T) {
	e := vimEditor(t, "|")
	typeText(e, "see ")
	e.InsertChip(NewPasteChip(e.NextChipID(), "a\nb\nc\nd"))
	typeText(e, " now")
	send(e, "esc 0 w")
	if c, _, _ := e.at(e.cur); !c.isChip() {
		t.Fatalf("w should land on the chip, cursor %v", e.cur)
	}
	send(e, "d w")
	if got := e.Display(); got != "see now" {
		t.Fatalf("dw over chip: %q", got)
	}
	send(e, "u")
	if len(e.Chips()) != 1 {
		t.Fatal("undo restores the chip")
	}
}

func TestVimModesAndCursor(t *testing.T) {
	e := vimEditor(t, "abc|")
	if e.VimMode() != vim.Insert {
		t.Fatal("vim starts in INSERT")
	}
	send(e, "esc")
	if e.VimMode() != vim.Normal || !e.vim.blockCursor() {
		t.Fatal("esc enters NORMAL with a block cursor")
	}
	if ok, _ := send(e, "enter"); ok {
		t.Fatal("enter in NORMAL belongs to the host")
	}
	send(e, "v")
	if from, to, ok := e.selection(); !ok || from != (Pos{0, 2}) || to != (Pos{0, 3}) {
		t.Fatalf("visual selection %v %v %v", from, to, ok)
	}
	send(e, "esc")
	_, cmd := send(e, "/")
	if msg := cmd(); msg != (VimEventMsg{Event: vim.EventHistorySearch}) {
		t.Fatalf("/ event: %#v", msg)
	}
	e.SetVim(false, nil)
	if e.VimEnabled() || e.VimMode() != vim.Insert {
		t.Fatal("vim off")
	}
}

func TestVimRemapTimeout(t *testing.T) {
	e := vimEditor(t, "|")
	_, cmd := send(e, "j")
	if cmd == nil || e.Display() != "" {
		t.Fatal("j waits for the remap with a timeout")
	}
	e.vim.m.RemapTimeout = time.Millisecond
	msg := VimTimeoutMsg{ed: e, seq: e.vim.seq}
	e.Update(msg)
	if e.Display() != "j" {
		t.Fatalf("timeout types the key: %q", e.Display())
	}
	// A stale timeout is ignored.
	send(e, "j")
	e.Update(msg)
	if e.Display() != "j" {
		t.Fatalf("stale timeout: %q", e.Display())
	}
}

func TestTeaKeyRoundTrip(t *testing.T) {
	for _, k := range []string{"ctrl+a", "alt+b", "shift+enter", "ctrl+shift+-", "backspace", "x", "ctrl+_"} {
		if got := teaKey(vim.Key{Name: k}).Keystroke(); got != k {
			t.Errorf("%s -> %s", k, got)
		}
	}
}
