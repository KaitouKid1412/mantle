package editor

import (
	"regexp"
	"strconv"
	"strings"
)

// ChipKind says what a chip stands for.
type ChipKind int

const (
	// ChipPaste is a collapsed paste: "[Pasted text #N +L lines]".
	ChipPaste ChipKind = iota
	// ChipImage is an attached image: "[Image #N]".
	ChipImage
)

// Chip is an atomic unit in the buffer: the cursor steps over it in one move
// and deletion removes it whole. Chips are immutable once inserted, so
// snapshots and kill-ring entries may share them.
type Chip struct {
	Kind ChipKind
	// ID is the N in the label. Pastes and images share one counter per
	// session, like Claude Code.
	ID int

	// Paste chips: the full text, its newline count (the "+L lines"), and
	// the paste-cache key once stored (see PasteStore).
	Text  string
	Lines int
	Hash  string

	// Image chips.
	Image *Image

	label string
}

// Label is the chip's text in the editor and in history's display field.
func (c *Chip) Label() string {
	if c.label != "" {
		return c.label
	}
	switch c.Kind {
	case ChipImage:
		c.label = "[Image #" + strconv.Itoa(c.ID) + "]"
	default:
		if c.Lines > 0 {
			c.label = "[Pasted text #" + strconv.Itoa(c.ID) + " +" + strconv.Itoa(c.Lines) + " lines]"
		} else {
			c.label = "[Pasted text #" + strconv.Itoa(c.ID) + "]"
		}
	}
	return c.label
}

// Expanded is the chip's content on submit: a paste's full text, or an
// image's label (the image itself travels as a separate content block).
func (c *Chip) Expanded() string {
	if c.Kind == ChipPaste {
		return c.Text
	}
	return c.Label()
}

// NewPasteChip returns a paste chip for text.
func NewPasteChip(id int, text string) *Chip {
	return &Chip{Kind: ChipPaste, ID: id, Text: text, Lines: strings.Count(text, "\n")}
}

// NewImageChip returns an image chip.
func NewImageChip(id int, img *Image) *Chip {
	return &Chip{Kind: ChipImage, ID: id, Image: img}
}

// NextChipID reserves and returns the next chip number.
func (e *Editor) NextChipID() int {
	e.chipSeq++
	return e.chipSeq
}

// SetChipSeq sets the last chip number handed out (for example after a
// restart, so numbering continues).
func (e *Editor) SetChipSeq(n int) { e.chipSeq = n }

// ChipSeq is the last chip number handed out.
func (e *Editor) ChipSeq() int { return e.chipSeq }

// Chips returns the chips in the buffer in reading order.
func (e *Editor) Chips() []*Chip {
	var out []*Chip
	for _, l := range e.lines {
		for _, c := range l.cells {
			if c.chip != nil {
				out = append(out, c.chip)
			}
		}
	}
	return out
}

// InsertChip inserts a chip at the cursor as one undoable edit.
func (e *Editor) InsertChip(ch *Chip) {
	if ch.ID > e.chipSeq {
		e.chipSeq = ch.ID
	}
	e.checkpoint(editOther)
	e.insertFragment(fragment{{chipCell(ch)}})
	e.afterEdit()
}

var chipLabelRE = regexp.MustCompile(`\[(?:Pasted text #(\d+)(?: \+\d+ lines)?|Image #(\d+))\]`)

// SetValueWithChips replaces the content with text in which chip labels
// (as produced by Display) are turned back into the given chips, matched by
// kind and ID. Labels without a matching chip stay plain text. Used to
// restore history entries and stashes.
func (e *Editor) SetValueWithChips(text string, chips []*Chip) {
	byKey := map[string]*Chip{}
	for _, ch := range chips {
		byKey[chipKey(ch.Kind, ch.ID)] = ch
		if ch.ID > e.chipSeq {
			e.chipSeq = ch.ID
		}
	}
	e.checkpoint(editOther)
	e.lines = nil
	for _, src := range strings.Split(normalizeNewlines(text), "\n") {
		var cells []cell
		for {
			loc := chipLabelRE.FindStringSubmatchIndex(src)
			if loc == nil {
				break
			}
			var key string
			if loc[2] >= 0 {
				id, _ := strconv.Atoi(src[loc[2]:loc[3]])
				key = chipKey(ChipPaste, id)
			} else {
				id, _ := strconv.Atoi(src[loc[4]:loc[5]])
				key = chipKey(ChipImage, id)
			}
			cells = append(cells, toCells(src[:loc[0]])...)
			if ch, ok := byKey[key]; ok {
				cells = append(cells, chipCell(ch))
			} else {
				cells = append(cells, toCells(src[loc[0]:loc[1]])...)
			}
			src = src[loc[1]:]
		}
		cells = append(cells, toCells(src)...)
		e.lines = append(e.lines, line{cells: cells})
	}
	e.cur = e.endPos()
	e.afterEdit()
}

func chipKey(k ChipKind, id int) string { return strconv.Itoa(int(k)) + ":" + strconv.Itoa(id) }

// ExpandChip replaces the paste chip just before the cursor (or under it)
// with its full text. It reports whether a chip was expanded.
func (e *Editor) ExpandChip() bool {
	p, ok := e.chipNearCursor(ChipPaste)
	if !ok {
		return false
	}
	ch := e.lines[p.Row].cells[p.Col].chip
	e.checkpoint(editOther)
	e.deleteRange(p, Pos{p.Row, p.Col + 1})
	e.cur = p
	e.insertFragment(fragment(splitLines(ch.Text)))
	e.afterEdit()
	return true
}

func (e *Editor) chipNearCursor(kind ChipKind) (Pos, bool) {
	l := e.lines[e.cur.Row].cells
	for _, c := range []int{e.cur.Col - 1, e.cur.Col} {
		if c >= 0 && c < len(l) && l[c].chip != nil && l[c].chip.Kind == kind {
			return Pos{e.cur.Row, c}, true
		}
	}
	return Pos{}, false
}

// ---- attachment navigation (Claude Code context "Attachments") ----

// EnterAttachments selects the last chip in the buffer. It reports false
// when there are no chips.
func (e *Editor) EnterAttachments() bool {
	n := len(e.Chips())
	if n == 0 {
		return false
	}
	e.attach = n - 1
	e.touch()
	return true
}

// InAttachments reports whether attachment navigation is active.
func (e *Editor) InAttachments() bool { return e.attach >= 0 }

// SelectedAttachment returns the selected chip, or nil.
func (e *Editor) SelectedAttachment() *Chip {
	if e.attach < 0 {
		return nil
	}
	chips := e.Chips()
	if e.attach >= len(chips) {
		return nil
	}
	return chips[e.attach]
}

// AttachmentPrev and AttachmentNext move the selection.
func (e *Editor) AttachmentPrev() {
	if e.attach > 0 {
		e.attach--
		e.touch()
	}
}

func (e *Editor) AttachmentNext() {
	if e.attach >= 0 && e.attach < len(e.Chips())-1 {
		e.attach++
		e.touch()
	}
}

// ExitAttachments leaves attachment navigation.
func (e *Editor) ExitAttachments() {
	if e.attach >= 0 {
		e.attach = -1
		e.touch()
	}
}

// RemoveAttachment deletes the selected chip. The selection moves to the
// neighbouring chip, and navigation ends when none are left.
func (e *Editor) RemoveAttachment() bool {
	ch := e.SelectedAttachment()
	if ch == nil {
		return false
	}
	for r, l := range e.lines {
		for c, cl := range l.cells {
			if cl.chip != ch {
				continue
			}
			e.checkpoint(editOther)
			e.deleteRange(Pos{r, c}, Pos{r, c + 1})
			if e.cur.Row == r && e.cur.Col > c {
				e.cur.Col--
			}
			e.afterEdit()
			n := len(e.Chips())
			switch {
			case n == 0:
				e.attach = -1
			case e.attach >= n:
				e.attach = n - 1
			}
			return true
		}
	}
	return false
}
