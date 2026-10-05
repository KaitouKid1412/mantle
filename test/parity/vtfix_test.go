package parity

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func emuRow(emu *vt.Emulator, y int) string {
	var b strings.Builder
	for x := range emu.Width() {
		if c := emu.CellAt(x, y); c != nil && c.Content != "" {
			b.WriteString(c.Content)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func TestStringSanitizerTitles(t *testing.T) {
	// A title whose UTF-8 holds 0x9C (✳) and 0x90 (◐), split across writes, then text.
	stream := []byte("\x1b]0;✳ Claude Code\x07\x1b]2;◐ busy\x1b\\\x1b[1;1Hhi ✳ there")
	for split := 1; split < len(stream); split++ {
		emu := vt.NewEmulator(30, 2)
		var s stringSanitizer
		_, _ = emu.Write(s.filter(stream[:split]))
		_, _ = emu.Write(s.filter(stream[split:]))
		if got := emuRow(emu, 0); got != "hi ✳ there" {
			t.Fatalf("split %d: row = %q", split, got)
		}
		_ = emu.Close()
	}
}

func TestStringSanitizerLeavesTextAlone(t *testing.T) {
	var s stringSanitizer
	in := []byte("plain ✳ text \x1b[31mred\x1b[0m \x1b(B\x1b]8;;https://x.example/é\x1b\\link\x1b]8;;\x1b\\ ◐")
	out := s.filter(in)
	if len(out) != len(in) {
		t.Fatalf("length changed: %d → %d", len(in), len(out))
	}
	want := bytes.Replace(in, []byte("é"), []byte("??"), 1)
	if !bytes.Equal(out, want) {
		t.Errorf("out = %q", out)
	}
	if !bytes.Contains(in, []byte("é")) {
		t.Error("filter must not modify its input")
	}
	// An ESC that is not ST aborts the string and starts a new sequence.
	s = stringSanitizer{}
	if got := s.filter([]byte("\x1b]0;a\x1b[1mé")); !bytes.HasSuffix(got, []byte("é")) {
		t.Errorf("abort: %q", got)
	}
}
