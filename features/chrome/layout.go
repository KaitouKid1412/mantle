package chrome

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// segment is a piece of a one-line bar. Plain is the unstyled text (for width and
// screen-reader output); Styled is what is drawn.
type segment struct {
	Plain, Styled string
	// Drop is the order in which segments are dropped when the line is too narrow
	// (higher first); 0 is never dropped, only truncated.
	Drop int
}

func seg(t *theme.Theme, tok theme.Token, s string, drop int) segment {
	return segment{Plain: s, Styled: t.Paint(tok, s), Drop: drop}
}

// bar lays out left and right segments on one line of width w: right-aligned right
// part, dropping segments by Drop priority and finally truncating. plain renders
// without styling (screen-reader mode).
func bar(left, right []segment, w int, plain bool) string {
	const gap = 2
	for {
		lw, rw := width(left, " "), width(right, "  ")
		if lw+rw+gapIf(lw, rw, gap) <= w || !dropOne(&left, &right) {
			break
		}
	}
	l, r := join(left, " ", plain), join(right, "  ", plain)
	lw, rw := ansi.StringWidth(l), ansi.StringWidth(r)
	if rw == 0 {
		return ansi.Truncate(l, w, "…")
	}
	if lw+gap+rw > w {
		// Only undroppable segments remain: give the left side priority.
		room := w - lw - gap
		if room < 4 {
			return ansi.Truncate(l, w, "…")
		}
		r = ansi.Truncate(r, room, "…")
		rw = ansi.StringWidth(r)
	}
	return l + strings.Repeat(" ", max(gap, w-lw-rw)) + r
}

func gapIf(lw, rw, gap int) int {
	if lw > 0 && rw > 0 {
		return gap
	}
	return 0
}

func width(segs []segment, sep string) int {
	n := 0
	for i, s := range segs {
		if i > 0 {
			n += len(sep)
		}
		n += ansi.StringWidth(s.Plain)
	}
	return n
}

func join(segs []segment, sep string, plain bool) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		if plain {
			parts = append(parts, s.Plain)
		} else {
			parts = append(parts, s.Styled)
		}
	}
	return strings.Join(parts, sep)
}

// dropOne removes the droppable segment with the highest Drop from either side.
func dropOne(left, right *[]segment) bool {
	best, side, idx := 0, 0, -1
	for i, s := range *left {
		if s.Drop > best {
			best, side, idx = s.Drop, 0, i
		}
	}
	for i, s := range *right {
		if s.Drop > best {
			best, side, idx = s.Drop, 1, i
		}
	}
	if idx < 0 {
		return false
	}
	if side == 0 {
		*left = append((*left)[:idx], (*left)[idx+1:]...)
	} else {
		*right = append((*right)[:idx], (*right)[idx+1:]...)
	}
	return true
}

// firstKey returns the chord to show for an action: def when it is still bound,
// else the first bound chord, else def.
func firstKey(ctx ext.Ctx, context string, a ext.ActionID, def string) string {
	keys := ctx.KeysFor(context, a)
	for _, k := range keys {
		if displayKey(k) == def {
			return def
		}
	}
	if len(keys) > 0 {
		return displayKey(keys[0])
	}
	return def
}

// displayKey shortens a chord for hints ("escape" → "esc").
func displayKey(k string) string { return strings.ReplaceAll(k, "escape", "esc") }

// truncateLines cuts every line to w cells and keeps at most maxLines (0 = all).
func truncateLines(lines []string, w, maxLines int) []string {
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			l = ansi.Truncate(l, w, "…")
		}
		out[i] = l
	}
	return out
}
