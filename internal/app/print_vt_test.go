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

// printMsg asks the printer feature to print n lines (as several blocks).
type printMsg struct {
	prefix string
	n      int
}

type reprintMsg struct{}

// liveBox renders `height` live lines and an input line.
func printerFeature(live int) ext.Feature {
	return ext.Feature{ID: "test.printer", Order: 10, Setup: func(r ext.Registrar) error {
		var sb strings.Builder
		for i := range live {
			if i > 0 {
				sb.WriteByte('\n')
			}
			fmt.Fprintf(&sb, "LIVE %02d", i)
		}
		r.AddComponent(ext.SlotLive, &box{id: "test.live", text: sb.String()}, ext.SlotOpts{})
		r.AddComponent(ext.SlotInput, focusBox{&box{id: "test.input", text: "> type here", ctx: ext.ContextChat}}, ext.SlotOpts{})
		ext.Subscribe(r, "test.print", func(c ext.Ctx, m printMsg) tea.Cmd {
			ls := lines(m.prefix, m.n)
			// Several blocks in one call, plus a second call in the same Update: order
			// must be kept.
			half := len(ls) / 2
			return tea.Batch(c.Print(strings.Join(ls[:half], "\n")), c.Print(ls[half:]...))
		})
		ext.Subscribe(r, "test.reprint", func(c ext.Ctx, m reprintMsg) tea.Cmd { return c.Reprint() })
		ext.Subscribe(r, "test.long", func(c ext.Ctx, m printLong) tea.Cmd { return c.Print(m.s) })
		return nil
	}}
}

func startPrinter(t *testing.T, w, h, live int) *testkit.Harness {
	t.Helper()
	host := NewHost([]ext.Feature{printerFeature(live)}, HostOptions{Core: CoreFeatures()})
	root := New(Options{Host: host, NoBackgroundQuery: true})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	hs.WaitForText("> type here", 3*time.Second)
	return hs
}

// checkPrinted asserts the printed lines appear exactly once, in order, followed by the
// live frame, and that no live-frame line leaked into scrollback (a ghost).
func checkPrinted(t *testing.T, hs *testkit.Harness, want []string, live int) {
	t.Helper()
	all := hs.All()
	var printed []string
	for _, l := range all {
		if strings.HasPrefix(l, "P ") {
			printed = append(printed, l)
		}
	}
	if strings.Join(printed, "\n") != strings.Join(want, "\n") {
		t.Fatalf("printed %d lines, want %d\nfirst diff near: %s\n--- all ---\n%s", len(printed), len(want), firstDiff(printed, want), strings.Join(all, "\n"))
	}
	liveSeen := 0
	for _, l := range hs.Scrollback() {
		if strings.HasPrefix(l, "LIVE ") || strings.HasPrefix(l, "> type here") {
			liveSeen++
		}
	}
	if liveSeen > 0 {
		t.Fatalf("%d ghost live-area lines in scrollback\n--- scrollback ---\n%s", liveSeen, strings.Join(hs.Scrollback(), "\n"))
	}
	screen := hs.ScreenLines()
	tail := strings.Join(screen, "\n")
	if !strings.Contains(tail, fmt.Sprintf("LIVE %02d", live-1)) || !strings.Contains(tail, "> type here") {
		t.Fatalf("live frame missing from screen:\n%s", tail)
	}
}

func firstDiff(a, b []string) string {
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return fmt.Sprintf("index %d: got %q want %q", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("length %d vs %d", len(a), len(b))
}

func TestPrintNoGhostLines(t *testing.T) {
	if testing.Short() {
		t.Skip("vt matrix")
	}
	sizes := [][2]int{{80, 24}, {120, 40}, {200, 60}}
	lives := []int{3, 8, 14, 20}
	for _, sz := range sizes {
		for _, live := range lives {
			if live > sz[1]-3 {
				continue
			}
			t.Run(fmt.Sprintf("%dx%d/live%d", sz[0], sz[1], live), func(t *testing.T) {
				t.Parallel()
				hs := startPrinter(t, sz[0], sz[1], live)
				n := sz[1]*2 + 7 // several chunks and more than a screenful
				hs.SendMsg(printMsg{prefix: "P", n: n})
				want := lines("P", n)
				hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") >= n }, 10*time.Second)
				hs.Settle(60*time.Millisecond, 2*time.Second)
				checkPrinted(t, hs, want, live)
			})
		}
	}
}

func countPrefix(ls []string, p string) int {
	n := 0
	for _, l := range ls {
		if strings.HasPrefix(l, p) {
			n++
		}
	}
	return n
}

func TestPrintWrapsLongLines(t *testing.T) {
	hs := startPrinter(t, 40, 12, 3)
	// 97 cells: wraps into 3 rows at PrintWidth(40) = 39 (a full 40-cell row would lose
	// its last cell to insertAbove's erase-line).
	long := "P " + strings.Repeat("x", 95)
	hs.SendMsg(printLong{long})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") >= 1 }, 5*time.Second)
	hs.Settle(60*time.Millisecond, 2*time.Second)
	all := strings.Join(hs.All(), "\n")
	if !strings.Contains(all, "P "+strings.Repeat("x", 37)+"\n"+strings.Repeat("x", 39)+"\n"+strings.Repeat("x", 19)) {
		t.Fatalf("long line not hard-wrapped:\n%s", all)
	}
	for _, l := range hs.Scrollback() {
		if strings.HasPrefix(l, "LIVE") {
			t.Fatalf("ghost line after wrapped print:\n%s", all)
		}
	}
}

type printLong struct{ s string }

type closeTallMsg struct{}

// TestReprintAfterTallFrameShrinks is plan 06's /resume: a tall live area (the picker,
// two rows short of the screen) closes in the same Update that asks for a Reprint, and
// the history is printed on ScreenClearedMsg. The clear must drop the shrink hold, or
// the frame stays padded to the picker's height and the history scrolls straight into
// scrollback; and blank rows between items must print even while the tall-frame window
// limits chunks to one row.
func TestReprintAfterTallFrameShrinks(t *testing.T) {
	const w, h = 60, 20
	live := &box{id: "test.live", text: strings.TrimSuffix(strings.Repeat("TALL\n", h-2), "\n")}
	printed := false
	f := ext.Feature{ID: "test.resume", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddComponent(ext.SlotLive, live, ext.SlotOpts{})
		r.AddComponent(ext.SlotInput, focusBox{&box{id: "test.input", text: "> type here", ctx: ext.ContextChat}}, ext.SlotOpts{})
		ext.Subscribe(r, "test.close", func(c ext.Ctx, _ closeTallMsg) tea.Cmd {
			live.text = "short"
			c.Invalidate("test.live")
			return c.Reprint()
		})
		ext.Subscribe(r, "test.cleared", func(c ext.Ctx, _ ext.ScreenClearedMsg) tea.Cmd {
			if printed {
				return nil
			}
			printed = true
			return c.Print("H 000\nH 001", "", "H 002")
		})
		return nil
	}}
	root := New(Options{Host: NewHost([]ext.Feature{f}, HostOptions{Core: CoreFeatures()}), NoBackgroundQuery: true})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	hs.WaitFor(func(s string) bool { return strings.Count(s, "TALL") == h-2 }, 3*time.Second)

	hs.SendMsg(closeTallMsg{})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "H ") == 3 }, 5*time.Second)
	hs.Settle(80*time.Millisecond, 3*time.Second)

	if sb := hs.Scrollback(); countPrefix(sb, "H ") > 0 {
		t.Fatalf("history scrolled into scrollback after the clear\n--- scrollback ---\n%s\n--- screen ---\n%s", strings.Join(sb, "\n"), hs.Screen())
	}
	screen := hs.ScreenLines()
	at := -1
	for i, l := range screen {
		if strings.HasPrefix(l, "H 001") {
			at = i
		}
	}
	if at < 1 || at+2 >= len(screen) || !strings.HasPrefix(screen[at-1], "H 000") ||
		strings.TrimSpace(screen[at+1]) != "" || !strings.HasPrefix(screen[at+2], "H 002") {
		t.Fatalf("want H 000, H 001, a blank row, H 002 on screen\n%s", strings.Join(screen, "|\n"))
	}
	if !strings.Contains(hs.Screen(), "> type here") || strings.Contains(hs.Screen(), "TALL") {
		t.Fatalf("live frame after the shrink:\n%s", hs.Screen())
	}
}

func TestReprintClearsScrollback(t *testing.T) {
	hs := startPrinter(t, 60, 16, 3)
	hs.SendMsg(printMsg{prefix: "P", n: 40})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") >= 40 }, 5*time.Second)
	hs.SendMsg(reprintMsg{})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") == 0 && len(hs.Scrollback()) == 0 }, 5*time.Second)
	hs.WaitForText("> type here", 2*time.Second)
	// Printing after a clear works normally.
	hs.SendMsg(printMsg{prefix: "P", n: 5})
	hs.WaitFor(func(string) bool { return countPrefix(hs.All(), "P ") == 5 }, 5*time.Second)
	hs.Settle(60*time.Millisecond, 2*time.Second)
	checkPrinted(t, hs, lines("P", 5), 3)
}
