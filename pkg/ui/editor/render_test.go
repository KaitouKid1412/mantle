package editor

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

var goldenWidths = []int{40, 80, 120}

// renderPlain draws the editor without styles, padded to its width with a
// "|" right edge, plus the cursor position.
func renderPlain(e *Editor) string {
	rows, _ := e.Render()
	x, y, ok := e.CursorPos()
	var b strings.Builder
	fmt.Fprintf(&b, "width=%d rows=%d cursor=%d,%d focused=%v\n", e.Width(), len(rows), x, y, ok)
	for _, r := range rows {
		p := ansi.Strip(r)
		b.WriteString(p)
		b.WriteString(strings.Repeat(" ", max(e.Width()-ansi.StringWidth(p), 0)))
		b.WriteString("|\n")
	}
	return b.String()
}

func checkRows(t *testing.T, e *Editor) {
	t.Helper()
	rows, _ := e.Render()
	if len(rows) != e.Height() {
		t.Errorf("Height()=%d but %d rows", e.Height(), len(rows))
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w > e.Width() {
			t.Errorf("row %d is %d wide (> %d): %q", i, w, e.Width(), ansi.Strip(r))
		}
	}
}

func TestRenderGolden(t *testing.T) {
	scenarios := []struct {
		name  string
		setup func(e *Editor)
	}{
		{"wrap", func(e *Editor) {
			e.SetValue("Refactor the session loader so that resumed sessions replay their earlier turns from the JSONL file, then add tests that cover sidechains, compact boundaries and malformed lines.\nSecond paragraph after a newline.")
			e.SetCursor(Pos{0, 60})
		}},
		{"chips", func(e *Editor) {
			typeText(e, "Compare ")
			e.InsertImage(&Image{MediaType: "image/png"})
			typeText(e, "with the log ")
			e.InsertPaste("a\nb\nc\nd\ne")
			typeText(e, " — 日本語のテキスト and emoji 👍🏽 done")
		}},
		{"ghost", func(e *Editor) {
			typeText(e, "/he")
			e.SetGhost("lp")
		}},
		{"suggestion", func(e *Editor) {
			e.SetPlaceholder("Try \"fix lint errors\"")
			e.SetGhost("run the tests and fix any failures you find in the editor package")
		}},
		{"placeholder", func(e *Editor) {
			e.SetPlaceholder("Try \"refactor <filepath>\"")
		}},
		{"exact-fill", func(e *Editor) {
			e.SetValue(strings.Repeat("x", e.Width()) + "\n" + strings.Repeat("y", e.Width()))
			e.SetCursor(Pos{0, e.Width()})
		}},
		{"long-word", func(e *Editor) {
			e.SetValue("see " + strings.Repeat("abcdefghij", 15) + " end")
		}},
		{"scroll", func(e *Editor) {
			var lines []string
			for i := 1; i <= 30; i++ {
				lines = append(lines, "line "+strconv.Itoa(i))
			}
			e.SetValue(strings.Join(lines, "\n"))
			e.SetMaxHeight(5)
			e.SetCursor(Pos{19, 2})
		}},
		{"blurred", func(e *Editor) {
			e.SetValue("not focused")
			e.Blur()
		}},
		{"tabs", func(e *Editor) {
			e.SetValue("a\tb\n\tindented")
		}},
	}
	for _, sc := range scenarios {
		for _, w := range goldenWidths {
			t.Run(sc.name+"/"+strconv.Itoa(w), func(t *testing.T) {
				e := newTestEditor()
				e.SetWidth(w)
				sc.setup(e)
				checkRows(t, e)
				golden.RequireEqual(t, renderPlain(e))
			})
		}
	}
}

// The styled render, escapes visible: cursor, chips, attachment selection,
// ghost text and a vim visual selection.
func TestRenderStyledGolden(t *testing.T) {
	e := newTestEditor()
	e.SetWidth(40)
	typeText(e, "see ")
	e.InsertImage(&Image{})
	typeText(e, "and ")
	e.InsertPaste("1\n2\n3\n4")
	e.EnterAttachments()
	out := e.View() + "\n---\n"

	e.ExitAttachments()
	e.SetGhost(" next")
	out += e.View() + "\n---\n"

	e.SetVim(true, nil)
	send(e, "esc 0 v e")
	out += e.View() + "\n---\n"

	e2 := newTestEditor()
	e2.SetWidth(40)
	e2.SetValue("think hard: ultrathink about it")
	e2.Decorate = func(row int, gs []string) []Span {
		s := strings.Join(gs, "")
		i := strings.Index(s, "ultrathink")
		if i < 0 {
			return nil
		}
		st := e2.Styles.Cursor // any visible style
		return []Span{{Start: len([]rune(s[:i])), End: len([]rune(s[:i])) + 10, Style: st.Bold(true)}}
	}
	out += e2.View()
	golden.RequireEqualEscape(t, []byte(out), true)
}

func TestRealCursor(t *testing.T) {
	e := newTestEditor()
	e.SetWidth(10)
	e.VirtualCursor = false
	e.SetValue("hello world again")
	rows, cur := e.Render()
	if cur == nil {
		t.Fatal("real cursor expected")
	}
	if len(rows) != 3 || cur.Y != 2 || cur.X != 5 {
		t.Fatalf("rows %d cursor %+v", len(rows), cur.Position)
	}
	if strings.Contains(strings.Join(rows, ""), "\x1b[7m") {
		t.Fatal("no virtual cursor when the real one is used")
	}
	e.Blur()
	if _, cur := e.Render(); cur != nil {
		t.Fatal("no cursor when blurred")
	}
}

func TestVerticalMovesOnWrappedLines(t *testing.T) {
	e := newTestEditor()
	e.SetWidth(10)
	e.SetValue("aaaa bbbb cccc dddd")
	// rows: "aaaa bbbb " / "cccc dddd" + cursor reserve
	e.SetCursor(Pos{0, 17})
	if !e.CursorUp() {
		t.Fatal("up within a wrapped line")
	}
	if e.Cursor() != (Pos{0, 7}) {
		t.Fatalf("cursor %v", e.Cursor())
	}
	if e.CursorUp() {
		t.Fatal("first visual row: up belongs to the host")
	}
	if !e.CursorDown() || e.Cursor() != (Pos{0, 17}) {
		t.Fatalf("down: %v", e.Cursor())
	}
	if !e.OnLastRow() || e.CursorDown() {
		t.Fatal("last row")
	}
}
