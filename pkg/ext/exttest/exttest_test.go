package exttest

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

type pingMsg struct{ n int }

func TestRegistrarAndDispatch(t *testing.T) {
	f := ext.Feature{ID: "demo.ping", Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "ping", Run: func(c ext.Ctx, args string) tea.Cmd {
			return c.Print("pong " + args)
		}})
		ext.Subscribe(r, "demo.ping/sub", func(c ext.Ctx, m pingMsg) tea.Cmd {
			c.Invalidate("demo.ping")
			return nil
		})
		return nil
	}}
	r, err := Setup(f)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCtx()
	cmd, ok := r.Command("ping")
	if !ok {
		t.Fatal("command not recorded")
	}
	msgs := Exec(cmd.Run(c, "x"))
	if len(msgs) != 1 || msgs[0].(PrintMsg).Blocks[0] != "pong x" {
		t.Fatalf("got %#v", msgs)
	}
	Exec(r.Dispatch(c, pingMsg{1}))
	Exec(r.Dispatch(c, "not a ping"))
	if len(c.Invalidated) != 1 {
		t.Fatalf("invalidated %v", c.Invalidated)
	}
}

func TestExecFlattens(t *testing.T) {
	c := NewCtx()
	msgs := Exec(tea.Sequence(c.Print("a"), tea.Batch(c.Print("b"), nil), c.Reprint()))
	if len(msgs) != 3 {
		t.Fatalf("got %d msgs: %#v", len(msgs), msgs)
	}
	if got := c.KeysFor(ext.ContextGlobal, ext.ActAppToggleTranscript); len(got) != 1 || got[0] != "ctrl+o" {
		t.Fatalf("KeysFor = %v", got)
	}
}
