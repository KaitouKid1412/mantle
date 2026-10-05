package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// cursorInput is a focused prompt that shows the real cursor, as plan 04's editor does.
type cursorInput struct{ focusBox }

func (c cursorInput) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: c.text, Cursor: tea.NewCursor(2, 0)}
}

type setLiveMsg struct{ rows int }

// shrinkFeature: a live area of variable height above a prompt with a footer below it.
func shrinkFeature(live *box) ext.Feature {
	return ext.Feature{ID: "test.shrink", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotLive, live, ext.SlotOpts{})
		r.AddComponent(ext.SlotInput, cursorInput{focusBox{&box{id: "test.input", text: "> type here", ctx: ext.ContextChat}}}, ext.SlotOpts{})
		r.AddComponent(ext.SlotBelowInput, &box{id: "test.footer", text: "footer one\nfooter two"}, ext.SlotOpts{})
		ext.Subscribe(r, "test.live", func(c ext.Ctx, m setLiveMsg) tea.Cmd {
			var rows []string
			for i := range m.rows {
				rows = append(rows, fmt.Sprintf("TALL %02d", i))
			}
			if m.rows == 0 {
				rows = []string{"short"}
			}
			live.text = strings.Join(rows, "\n")
			c.Invalidate("test.live")
			return nil
		})
		ext.Subscribe(r, "test.print", func(c ext.Ctx, m printMsg) tea.Cmd { return c.Print(lines(m.prefix, m.n)...) })
		// A tall inline dialog without a cursor (the cursor is hidden while it is open).
		r.AddDialog("dialog.tall", func(c ext.Ctx, args any) (ext.Dialog, error) {
			return &testDialog{focusBox: focusBox{&box{id: "dialog.tall#1", text: strings.TrimSuffix(strings.Repeat("DIALOG ROW\n", 12), "\n"), ctx: ext.ContextConfirmation}}}, nil
		})
		ext.Subscribe(r, "test.dialog", func(c ext.Ctx, m tallDialogMsg) tea.Cmd {
			if m.open {
				return c.OpenDialog("dialog.tall", nil)
			}
			return c.CloseDialog("dialog.tall")
		})
		return nil
	}}
}

type tallDialogMsg struct{ open bool }

// contiguous returns the lines of all from the first one starting with first through
// the first one starting with last.
func contiguous(all []string, first, last string) []string {
	start := -1
	for i, l := range all {
		if start < 0 && strings.HasPrefix(l, first) {
			start = i
		}
		if start >= 0 && strings.HasPrefix(l, last) {
			return all[start : i+1]
		}
	}
	return nil
}

// TestShrinkLeavesNoGap: when the live area shrinks (a turn ends, a dialog closes),
// the frame moves up over the freed rows. Nothing stale and no blank rows stay
// between what was printed and the frame, on screen or in scrollback.
func TestShrinkLeavesNoGap(t *testing.T) {
	for _, tc := range []struct {
		name       string
		w, h, tall int
	}{
		{"small", 60, 20, 6},
		{"tall", 60, 20, 14},
		{"wide", 120, 40, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := &box{id: "test.live", text: "short"}
			root := New(Options{Host: NewHost([]ext.Feature{shrinkFeature(live)}, HostOptions{Core: CoreFeatures()}), NoBackgroundQuery: true})
			hs := testkit.New(t, root, testkit.WithSize(tc.w, tc.h))
			hs.WaitForText("footer two", 3*time.Second)

			hs.SendMsg(printMsg{prefix: "P", n: 3})
			hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") == 3 }, 3*time.Second)
			for round := range 2 { // twice: the second shrink starts from a released frame
				hs.SendMsg(setLiveMsg{rows: tc.tall})
				hs.WaitForText(fmt.Sprintf("TALL %02d", tc.tall-1), 3*time.Second)
				hs.Settle(50*time.Millisecond, 2*time.Second)
				hs.SendMsg(setLiveMsg{})
				hs.WaitFor(func(s string) bool { return !strings.Contains(s, "TALL") }, 3*time.Second)
				hs.Settle(120*time.Millisecond, 3*time.Second) // past the hold's release
				hs.SendMsg(printMsg{prefix: fmt.Sprintf("Q%d", round), n: 2})
				hs.WaitFor(func(string) bool { return countPrefix(hs.All(), fmt.Sprintf("Q%d ", round)) == 2 }, 3*time.Second)
				hs.Settle(120*time.Millisecond, 3*time.Second)
			}

			all := hs.All()
			for _, l := range all {
				if strings.Contains(l, "TALL") {
					t.Fatalf("stale live rows left behind\n%s", strings.Join(all, "|\n"))
				}
			}
			want := []string{"P 000", "P 001", "P 002", "Q0 000", "Q0 001", "Q1 000", "Q1 001", "short", "> type here", "footer one", "footer two"}
			got := contiguous(all, "P 000", "footer two")
			if len(got) != len(want) {
				t.Fatalf("printed lines and the frame should be contiguous, no gaps\nwant %q\n--- all ---\n%s", want, strings.Join(all, "|\n"))
			}
			for i := range want {
				if !strings.HasPrefix(got[i], want[i]) {
					t.Fatalf("row %d = %q, want %q\n--- all ---\n%s", i, got[i], want[i], strings.Join(all, "|\n"))
				}
			}
			// Only the tall frame's own growth may scroll (its peak: 5 printed rows,
			// tall live rows and 3 prompt rows). A frame still held at the tall height
			// when the prints came would have scrolled them away too.
			wantSB := max(0, 5+tc.tall+3-tc.h)
			if sb := hs.Scrollback(); len(sb) != wantSB {
				t.Fatalf("%d rows scrolled away, want %d (was the held height released?)\n%s", len(sb), wantSB, strings.Join(all, "|\n"))
			}
		})
	}
}

// TestShrinkWithHiddenCursor: while a dialog without a cursor is open the renderer's
// cursor row is unknown, so a shrink is held (blank rows below the frame) until the
// prompt and its cursor are back; prints meanwhile still land in order with no gaps,
// and nothing of the dialog or the tall live area is left behind.
func TestShrinkWithHiddenCursor(t *testing.T) {
	live := &box{id: "test.live", text: "short"}
	root := New(Options{Host: NewHost([]ext.Feature{shrinkFeature(live)}, HostOptions{Core: CoreFeatures()}), NoBackgroundQuery: true})
	hs := testkit.New(t, root, testkit.WithSize(60, 24))
	hs.WaitForText("footer two", 3*time.Second)
	step := func(m tea.Msg, until func(string) bool) {
		t.Helper()
		hs.SendMsg(m)
		hs.WaitFor(until, 3*time.Second)
		hs.Settle(120*time.Millisecond, 3*time.Second)
	}
	has := func(sub string) func(string) bool { return func(s string) bool { return strings.Contains(s, sub) } }
	printed := func(p string, n int) func(string) bool {
		return func(string) bool { return countPrefix(hs.All(), p+" ") == n }
	}

	step(printMsg{prefix: "P", n: 2}, printed("P", 2))
	// A tall dialog closes back to the prompt.
	step(tallDialogMsg{open: true}, has("DIALOG ROW"))
	step(tallDialogMsg{}, func(s string) bool { return !strings.Contains(s, "DIALOG") && strings.Contains(s, "> type here") })
	step(printMsg{prefix: "Q", n: 2}, printed("Q", 2))
	// The live area shrinks while the dialog (no cursor) is open, with a print in
	// between; then the dialog closes.
	step(setLiveMsg{rows: 8}, has("TALL 07"))
	step(tallDialogMsg{open: true}, has("DIALOG ROW"))
	step(setLiveMsg{}, func(s string) bool { return !strings.Contains(s, "TALL") })
	step(printMsg{prefix: "R", n: 2}, printed("R", 2))
	step(tallDialogMsg{}, func(s string) bool { return !strings.Contains(s, "DIALOG") && strings.Contains(s, "> type here") })
	step(printMsg{prefix: "S", n: 2}, printed("S", 2))

	all := hs.All()
	for _, l := range all {
		if strings.Contains(l, "TALL") || strings.Contains(l, "DIALOG") {
			t.Fatalf("stale rows left behind\n%s", strings.Join(all, "|\n"))
		}
	}
	want := []string{"P 000", "P 001", "Q 000", "Q 001", "R 000", "R 001", "S 000", "S 001", "short", "> type here", "footer one", "footer two"}
	got := contiguous(all, "P 000", "footer two")
	if len(got) != len(want) {
		t.Fatalf("want %q contiguous\n--- all ---\n%s", want, strings.Join(all, "|\n"))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("row %d = %q, want %q\n--- all ---\n%s", i, got[i], want[i], strings.Join(all, "|\n"))
		}
	}
}
