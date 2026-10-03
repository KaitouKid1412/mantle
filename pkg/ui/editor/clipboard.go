package editor

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ErrNoClipboardImage means the clipboard holds no image.
var ErrNoClipboardImage = errors.New("no image found in clipboard")

// ErrClipboardUnsupported means no clipboard tool is available.
var ErrClipboardUnsupported = errors.New("image paste is not supported here")

// clipboardTimeout bounds the external clipboard tools.
const clipboardTimeout = 5 * time.Second

// ImagePastedMsg carries the result of PasteImageCmd.
type ImagePastedMsg struct {
	Image *Image
	Err   error
}

// PasteImageCmd reads an image from the system clipboard off the UI
// goroutine (chat:imagePaste). Feed the resulting ImagePastedMsg to
// InsertImage, or show Err as a notice.
func PasteImageCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
		defer cancel()
		im, err := ReadClipboardImage(ctx)
		return ImagePastedMsg{Image: im, Err: err}
	}
}

// InsertImage inserts an image chip at the cursor and returns it. Like
// Claude Code, the chip is set off from surrounding text by spaces.
func (e *Editor) InsertImage(im *Image) *Chip {
	ch := NewImageChip(e.NextChipID(), im)
	e.checkpoint(editOther)
	e.insertImageChip(ch)
	e.afterEdit()
	return ch
}

// insertImageChip inserts ch with a space before it (after text) and after
// it (unless one follows already). It does not checkpoint.
func (e *Editor) insertImageChip(ch *Chip) {
	l := e.lines[e.cur.Row].cells
	if e.cur.Col > 0 && !l[e.cur.Col-1].isSpace() {
		e.insertFragment(fragment{toCells(" ")})
	}
	e.insertFragment(fragment{{chipCell(ch)}})
	l = e.lines[e.cur.Row].cells
	if e.cur.Col >= len(l) || !l[e.cur.Col].isSpace() {
		e.insertFragment(fragment{toCells(" ")})
	}
}
