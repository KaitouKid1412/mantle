package clipcmd

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/clipboard"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

func withCopier(t *testing.T, c clipboard.Copier) {
	t.Helper()
	old := Copier
	Copier = c
	t.Cleanup(func() { Copier = old })
}

func TestCopyLocal(t *testing.T) {
	var got string
	withCopier(t, clipboard.Copier{
		Env: terminal.Map{}, GOOS: "darwin",
		LookPath: func(string) (string, error) { return "/usr/bin/pbcopy", nil },
		Run: func(_ context.Context, _ string, _ []string, stdin []byte) error {
			got = string(stdin)
			return nil
		},
	})
	msg := Copy("copy", "hello")()
	m, ok := msg.(CopiedMsg)
	if !ok || m.Tag != "copy" || m.Method != clipboard.Pbcopy || m.Err != nil || m.Bytes != 5 || got != "hello" {
		t.Fatalf("msg = %#v, got %q", msg, got)
	}
	if m.Notice() != "Copied to clipboard" {
		t.Errorf("Notice = %q", m.Notice())
	}
}

func TestCopyOSC52(t *testing.T) {
	withCopier(t, clipboard.Copier{Env: terminal.Map{"SSH_TTY": "/dev/x"}, GOOS: "linux"})
	batch, ok := Copy("t", "hi")().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("want a batch, got %#v", batch)
	}
	raw, ok := batch[0]().(tea.RawMsg)
	if !ok || raw.Msg != "\x1b]52;c;aGk=\a" {
		t.Errorf("raw = %#v", batch[0]())
	}
	m := batch[1]().(CopiedMsg)
	if m.Method != clipboard.OSC52 || m.Notice() != "Sent to the terminal's clipboard" {
		t.Errorf("msg = %#v", m)
	}
}

func TestCopyError(t *testing.T) {
	withCopier(t, clipboard.Copier{Env: terminal.Map{"SSH_TTY": "x"}, GOOS: "linux"})
	big := make([]byte, clipboard.MaxOSC52)
	m := Copy("t", string(big))().(CopiedMsg)
	if !errors.Is(m.Err, clipboard.ErrTooLarge) || m.Notice() == "" {
		t.Errorf("msg = %#v", m)
	}
}
