package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// The committer writes finished output into the terminal's native scrollback with
// tea.Println. Bubble Tea's inline insertAbove writes immediately (not on the frame
// ticker) and is only correct when the printed rows fit between the top of the screen
// and the live frame; a taller print leaves a ghost copy of the live area in
// scrollback. So the printer:
//
//   - hard-wraps every line to the terminal width (Bubble Tea's own wrap estimate is
//     approximate);
//   - prints at most H − liveHeight − 1 rows per tea.Println, where liveHeight is the
//     tallest frame of the last 250 ms (the renderer may still show an older frame);
//   - prints one chunk per event-loop round trip, so the next chunk is sized with the
//     frame that is current then, and so prints from different Updates never
//     interleave;
//   - pauses while an alt-screen view is up (Println is a no-op there) and resumes
//     when it closes.
//
// Reprint (Ctx.Reprint) goes through the same queue: it writes ESC[2J ESC[3J ESC[H,
// forces a full redraw, waits for the next frame flush and then delivers
// ext.ScreenClearedMsg.

type printItem struct {
	lines []string // already wrapped
	clear bool
}

type printer struct {
	queue  []printItem
	busy   bool // a chunk or clear is in flight
	paused bool
}

type (
	printStepMsg struct{}
	printDoneMsg struct{}
	clearedMsg   struct{}
)

// clearSeq erases the screen and the scrollback and homes the cursor.
const clearSeq = "\x1b[2J\x1b[3J\x1b[H"

// PrintWidth is the widest line Print keeps intact: one less than the terminal width.
// insertAbove follows each line with EL (erase to end of line); on a line that fills
// the last column the cursor sits there in the pending-wrap state, and EL erases that
// last character. Renderers should render committed items at PrintWidth.
func PrintWidth(termWidth int) int { return max(1, termWidth-1) }

func (r *Root) wrapBlock(b string) []string {
	w := r.w
	if w <= 0 {
		w = 80
	}
	w = PrintWidth(w)
	b = strings.ReplaceAll(b, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(b, "\n") {
		line = strings.ReplaceAll(line, "\t", "    ")
		if ansi.StringWidth(line) <= w {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(line, w, true), "\n")...)
	}
	return out
}

func (r *Root) enqueuePrint(blocks []string) tea.Cmd {
	if r.opts.Layout == ext.Fullscreen {
		// No scrollback in fullscreen: hand the blocks to features (plan 12's
		// transcript shows them in place).
		if len(blocks) == 0 {
			return nil
		}
		return ext.Msg(ext.PrintedMsg{Blocks: append([]string(nil), blocks...)})
	}
	var lines []string
	for _, b := range blocks {
		lines = append(lines, r.wrapBlock(b)...)
	}
	if len(lines) == 0 {
		return nil
	}
	r.printer.queue = append(r.printer.queue, printItem{lines: lines})
	return r.kickPrinter()
}

func (r *Root) enqueueClear() tea.Cmd {
	r.printer.queue = append(r.printer.queue, printItem{clear: true})
	return r.kickPrinter()
}

func (r *Root) kickPrinter() tea.Cmd {
	if r.printer.busy {
		return nil
	}
	return ext.Msg(printStepMsg{})
}

// chunkRows is how many rows one Println may print now.
func (r *Root) chunkRows() int {
	return max(1, r.h-r.liveHeight()-1)
}

func (r *Root) printerUpdate(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case printDoneMsg:
		r.printer.busy = false
		return r.printNext()
	case clearedMsg:
		r.printer.busy = false
		return tea.Batch(r.broadcast(ext.ScreenClearedMsg{}), r.printNext())
	case printStepMsg:
		if r.printer.busy {
			return nil
		}
		return r.printNext()
	}
	return nil
}

func (r *Root) printNext() tea.Cmd {
	if len(r.printer.queue) == 0 {
		return nil
	}
	if r.layoutMode() != ext.Inline {
		r.printer.paused = true
		return nil
	}
	r.printer.paused = false
	head := &r.printer.queue[0]
	r.printer.busy = true
	if head.clear {
		r.printer.queue = r.printer.queue[1:]
		r.invalidateAll()
		wait := 3 * r.opts.FrameInterval
		return tea.Sequence(
			tea.Raw(clearSeq),
			tea.ClearScreen,
			tea.Tick(wait, func(time.Time) tea.Msg { return clearedMsg{} }),
		)
	}
	n := min(r.chunkRows(), len(head.lines))
	chunk := head.lines[:n]
	head.lines = head.lines[n:]
	if len(head.lines) == 0 {
		r.printer.queue = r.printer.queue[1:]
	}
	return tea.Sequence(tea.Println(strings.Join(chunk, "\n")), ext.Msg(printDoneMsg{}))
}

// resumePrinter restarts printing after an alt-screen view closed.
func (r *Root) resumePrinter() tea.Cmd {
	if r.printer.paused && !r.printer.busy {
		return ext.Msg(printStepMsg{})
	}
	return nil
}
