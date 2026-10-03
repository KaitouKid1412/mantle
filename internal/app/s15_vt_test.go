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

// Spike S15 checks (docs/plans/01-contracts-shell.md, "Facts verified").

// commitBox is a live area whose finished lines move into scrollback: on commitMsg the
// first n live lines are printed and removed from the live area in the same Update,
// which is what the transcript's commit policy does.
type commitBox struct {
	box
	live []string
}

type commitMsg struct{ n int }
type growMsg struct{ lines []string }

func (b *commitBox) Update(c ext.Ctx, m tea.Msg) tea.Cmd {
	switch m := m.(type) {
	case commitMsg:
		n := min(m.n, len(b.live))
		done := b.live[:n]
		b.live = append([]string(nil), b.live[n:]...)
		c.Invalidate(b.id)
		return c.Print(strings.Join(done, "\n"))
	case growMsg:
		b.live = append(b.live, m.lines...)
		c.Invalidate(b.id)
	}
	return nil
}

func (b *commitBox) View(c ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: strings.Join(b.live, "\n")}
}

func startCommit(t *testing.T, w, h int) (*testkit.Harness, *commitBox) {
	t.Helper()
	cb := &commitBox{box: box{id: "test.live"}}
	f := ext.Feature{ID: "test.commit", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotLive, cb, ext.SlotOpts{})
		r.AddComponent(ext.SlotInput, focusBox{&box{id: "test.input", text: "> type here", ctx: ext.ContextChat}}, ext.SlotOpts{})
		return nil
	}}
	host := NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()})
	hs := testkit.New(t, New(Options{Host: host, NoBackgroundQuery: true}), testkit.WithSize(w, h))
	hs.WaitForText("> type here", 3*time.Second)
	return hs, cb
}

// TestCommitAndRemove streams items into the live area and commits them in batches:
// every item must end up in scrollback exactly once, in order, with no ghost copies of
// the live area (S15 "flicker on commit-and-remove").
func TestCommitAndRemove(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			hs, _ := startCommit(t, sz[0], sz[1])
			var want []string
			for round := range 6 {
				var batch []string
				for i := range 7 {
					batch = append(batch, fmt.Sprintf("P item %02d.%d", round, i))
				}
				want = append(want, batch...)
				hs.SendMsg(growMsg{lines: batch})
				hs.WaitForText(batch[len(batch)-1], 3*time.Second)
				hs.SendMsg(commitMsg{n: 5}) // leave 2 live, commit the rest later
			}
			hs.SendMsg(commitMsg{n: 1000})
			hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P item") == len(want) && countPrefix(hs.ScreenLines(), "P item") <= sz[1] }, 5*time.Second)
			hs.Settle(80*time.Millisecond, 2*time.Second)
			var got []string
			for _, l := range hs.All() {
				if strings.HasPrefix(l, "P item") {
					got = append(got, l)
				}
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("items %d want %d; first diff %s\n%s", len(got), len(want), firstDiff(got, want), strings.Join(hs.All(), "\n"))
			}
			for _, l := range hs.Scrollback() {
				if strings.HasPrefix(l, "> type here") {
					t.Fatalf("ghost input line in scrollback:\n%s", strings.Join(hs.Scrollback(), "\n"))
				}
			}
		})
	}
}

// TestResizeReflow prints, then narrows and widens the terminal: the live frame must
// be redrawn intact at each width and later prints must still land in order.
func TestResizeReflow(t *testing.T) {
	hs := startPrinter(t, 100, 20, 4)
	hs.SendMsg(printMsg{prefix: "P", n: 30})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") >= 30 }, 5*time.Second)
	for _, w := range []int{60, 140, 80} {
		hs.Resize(w, 20)
		hs.WaitFor(func(s string) bool { return strings.Contains(s, "LIVE 03") && strings.Contains(s, "> type here") }, 3*time.Second)
	}
	hs.SendMsg(printMsg{prefix: "Q", n: 10})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "Q ") >= 10 }, 5*time.Second)
	hs.Settle(80*time.Millisecond, 2*time.Second)
	var qs []string
	for _, l := range hs.All() {
		if strings.HasPrefix(l, "Q ") {
			qs = append(qs, l)
		}
	}
	if strings.Join(qs, ",") != strings.Join(lines("Q", 10), ",") {
		t.Fatalf("prints after resize: %v", qs)
	}
	screen := hs.ScreenLines()
	last := ""
	for _, l := range screen {
		if l != "" {
			last = l
		}
	}
	if last != "> type here" {
		t.Fatalf("live frame not at the bottom after resizes; last line %q\n%s", last, strings.Join(screen, "\n"))
	}
}

// TestModifiedEnterDecoding records how Bubble Tea decodes the byte sequences
// terminals send for shift+enter and ctrl+enter (S15 key detection).
func TestModifiedEnterDecoding(t *testing.T) {
	cases := []struct{ name, seq, want string }{
		{"plain enter (all terminals)", "\r", "enter"},
		{"kitty / Ghostty / WezTerm / iTerm2 CSI u shift+enter", "\x1b[13;2u", "shift+enter"},
		{"CSI u ctrl+enter", "\x1b[13;5u", "ctrl+enter"},
		{"xterm modifyOtherKeys=2 shift+enter", "\x1b[27;2;13~", "shift+enter"},
		{"alt+enter (Terminal.app option-as-meta, fallback)", "\x1b\r", "alt+enter"},
		{"ctrl+j (newline fallback everywhere)", "\n", "ctrl+j"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := make(chan string, 4)
			m := keyRecorder{got: got}
			hs := testkit.New(t, m, testkit.WithSize(40, 5))
			time.Sleep(50 * time.Millisecond)
			hs.Type(c.seq)
			select {
			case k := <-got:
				if k != c.want {
					t.Fatalf("%q decoded as %q, want %q", c.seq, k, c.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%q produced no key", c.seq)
			}
		})
	}
}

type keyRecorder struct{ got chan string }

func (k keyRecorder) Init() tea.Cmd { return nil }
func (k keyRecorder) Update(m tea.Msg) (tea.Model, tea.Cmd) {
	if kp, ok := m.(tea.KeyPressMsg); ok {
		select {
		case k.got <- kp.Keystroke():
		default:
		}
	}
	return k, nil
}
func (k keyRecorder) View() tea.View { return tea.NewView("keys") }
