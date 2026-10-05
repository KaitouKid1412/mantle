package parity

import (
	"slices"
	"time"
)

// steadyFrame takes a checkpoint frame that doesn't depend on where an animation was:
// when two snapshots 250 ms apart differ, it samples a little longer and merges the
// samples with mergeBlink, so a blinking glyph (Claude Code's pending-tool bullet)
// always counts as shown. Changing text (a spinner) keeps the last sample; the
// normalizer covers those.
func steadyFrame(t *Term) Frame {
	a := t.Snapshot()
	time.Sleep(250 * time.Millisecond)
	b := t.Snapshot()
	if slices.Equal(a.Screen, b.Screen) {
		return b
	}
	samples := []Frame{a, b}
	for range 4 {
		time.Sleep(250 * time.Millisecond)
		samples = append(samples, t.Snapshot())
	}
	return mergeBlink(samples)
}

// mergeBlink returns the last sample with every screen cell that is blank there but
// shown in another sample of the same line filled in, as long as the two lines differ
// only in such cells.
func mergeBlink(samples []Frame) Frame {
	out := samples[len(samples)-1]
	out.Screen = slices.Clone(out.Screen)
	for _, s := range samples[:len(samples)-1] {
		if len(s.Screen) != len(out.Screen) {
			continue
		}
		for i, line := range s.Screen {
			if merged, ok := fillBlanks([]rune(out.Screen[i]), []rune(line)); ok {
				out.Screen[i] = merged
			}
		}
	}
	return out
}

// fillBlanks fills the blanks of base with the cells other shows there; ok is false
// when the lines differ in any other way.
func fillBlanks(base, other []rune) (string, bool) {
	n := max(len(base), len(other))
	out := make([]rune, n)
	changed := false
	for j := range n {
		x, y := ' ', ' '
		if j < len(base) {
			x = base[j]
		}
		if j < len(other) {
			y = other[j]
		}
		switch {
		case x == y:
			out[j] = x
		case x == ' ':
			out[j], changed = y, true
		case y == ' ':
			out[j] = x
		default:
			return "", false
		}
	}
	if !changed {
		return "", false
	}
	return string(out), true
}
