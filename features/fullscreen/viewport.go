package fullscreen

// Block is one transcript item rendered at the viewport width.
type Block struct {
	ID    string
	Lines []string // styled, at most Width cells each
}

// Viewport is a virtual scroll window over the transcript's rendered lines. It knows
// line counts per block, so only the visible window (plus a margin) has to be
// materialized by the caller. Offset counts lines from the top of the document.
type Viewport struct {
	Height int
	offset int
	follow bool // stick to the bottom as content grows
	total  int
	starts []int // first line of each block
	ids    []string
	unseen int // lines added below the window while not following
	// Tail is blank lines after the last block (the transcript's bottom margin, which
	// scrolls away with it).
	Tail int
	tail int // the tail counted in total
}

// NewViewport returns a viewport that follows the bottom.
func NewViewport(height int) *Viewport { return &Viewport{Height: max(1, height), follow: true} }

// SetBlocks updates the document. Lines added while the user has scrolled up count as
// "new" for the jump-to-bottom pill.
func (v *Viewport) SetBlocks(blocks []Block) {
	prev := v.total
	v.starts, v.ids = v.starts[:0], v.ids[:0]
	total := 0
	for _, b := range blocks {
		v.starts = append(v.starts, total)
		v.ids = append(v.ids, b.ID)
		total += len(b.Lines)
	}
	v.tail = 0
	if total > 0 {
		v.tail = v.Tail
		total += v.tail
	}
	v.total = total
	if v.follow {
		v.unseen = 0
	} else if total > prev {
		v.unseen += total - prev
	}
	v.clamp()
}

// SetHeight changes the window height (resize).
func (v *Viewport) SetHeight(h int) {
	v.Height = max(1, h)
	v.clamp()
}

func (v *Viewport) maxOffset() int { return max(0, v.total-v.Height) }

func (v *Viewport) clamp() {
	if v.follow {
		v.offset = v.maxOffset()
		return
	}
	v.offset = max(0, min(v.offset, v.maxOffset()))
}

// Offset is the first visible line.
func (v *Viewport) Offset() int { return v.offset }

// Total is the document's line count.
func (v *Viewport) Total() int { return v.total }

// Following reports whether the view sticks to the bottom.
func (v *Viewport) Following() bool { return v.follow }

// Unseen is the number of lines added below the window since the user scrolled up.
func (v *Viewport) Unseen() int { return v.unseen }

// ScrollBy moves the window n lines (negative = up). Reaching the bottom resumes
// following.
func (v *Viewport) ScrollBy(n int) {
	v.offset = max(0, min(v.offset+n, v.maxOffset()))
	v.follow = v.offset >= v.maxOffset()
	if v.follow {
		v.unseen = 0
	}
}

// Page scrolls by a window height minus one line of context.
func (v *Viewport) Page(dir int) { v.ScrollBy(dir * max(1, v.Height-1)) }

// HalfPage scrolls by half a window.
func (v *Viewport) HalfPage(dir int) { v.ScrollBy(dir * max(1, v.Height/2)) }

// Top jumps to the start.
func (v *Viewport) Top() {
	v.offset, v.follow = 0, v.maxOffset() == 0
}

// ResetTop jumps to the start of a new document: nothing counts as new below.
func (v *Viewport) ResetTop() {
	v.unseen = 0
	v.Top()
}

// Bottom jumps to the end and resumes following.
func (v *Viewport) Bottom() {
	v.follow, v.unseen = true, 0
	v.offset = v.maxOffset()
}

// ScrollTo makes line visible, centring it when it was outside the window.
func (v *Viewport) ScrollTo(line int) {
	if line >= v.offset && line < v.offset+v.Height {
		return
	}
	v.offset = max(0, min(line-v.Height/2, v.maxOffset()))
	v.follow = v.offset >= v.maxOffset()
}

// Visible returns the index range [from, to) of blocks that intersect the window,
// widened by margin lines on each side.
func (v *Viewport) Visible(margin int) (from, to int) {
	lo, hi := max(0, v.offset-margin), v.offset+v.Height+margin
	from, to = len(v.starts), len(v.starts)
	for i, s := range v.starts {
		end := v.total
		if i+1 < len(v.starts) {
			end = v.starts[i+1]
		}
		if end > lo && from == len(v.starts) {
			from = i
		}
		if s >= hi {
			to = i
			break
		}
	}
	return min(from, to), to
}

// BlockAt returns the block index and the line within it for a document line.
func (v *Viewport) BlockAt(line int) (block, within int, ok bool) {
	if line < 0 || line >= v.total || (len(v.starts) > 0 && line >= v.total-v.tail) {
		return 0, 0, false // outside the document, or in the blank tail
	}
	lo, hi := 0, len(v.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if v.starts[mid] <= line {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, line - v.starts[lo], true
}

// BlockStart is the first document line of block i.
func (v *Viewport) BlockStart(i int) int {
	if i < 0 || i >= len(v.starts) {
		return v.total
	}
	return v.starts[i]
}

// BlockID returns block i's ID.
func (v *Viewport) BlockID(i int) string {
	if i < 0 || i >= len(v.ids) {
		return ""
	}
	return v.ids[i]
}

// Wheel turns wheel events into a line count: base lines per notch (scroll speed),
// accelerated while notches arrive quickly (each consecutive fast notch adds one more
// line, up to 4×).
type Wheel struct {
	Base       int  // lines per notch (/scroll-speed); 0 = 3
	Accelerate bool // wheelScrollAccelerationEnabled
	streak     int
	lastMS     int64
	seen       bool
}

// Notch returns the lines to scroll for one notch at time nowMS (milliseconds).
func (w *Wheel) Notch(nowMS int64) int {
	base := w.Base
	if base <= 0 {
		base = 3
	}
	if !w.Accelerate {
		return base
	}
	if w.seen && nowMS-w.lastMS <= 50 {
		w.streak = min(w.streak+1, 3*base)
	} else {
		w.streak = 0
	}
	w.lastMS, w.seen = nowMS, true
	return base + w.streak
}
