package parity

import (
	"slices"
	"testing"
)

func TestMergeBlink(t *testing.T) {
	frames := []Frame{
		{Screen: []string{"⏺ Creating a marker file", "✻ Working", "same"}},
		{Screen: []string{"  Creating a marker file", "✶ Working", "same"}},
	}
	got := mergeBlink(frames).Screen
	want := []string{"⏺ Creating a marker file", "✶ Working", "same"}
	if !slices.Equal(got, want) {
		t.Errorf("merged = %q", got)
	}
	if frames[1].Screen[0] != "  Creating a marker file" {
		t.Error("mergeBlink must not modify its input")
	}
	// Lines that differ in shown text are left as the last sample has them.
	if s, ok := fillBlanks([]rune("abc"), []rune("abd")); ok {
		t.Errorf("fillBlanks = %q", s)
	}
	if s, ok := fillBlanks([]rune("a"), []rune("a  x")); !ok || s != "a  x" {
		t.Errorf("trailing cell = %q %v", s, ok)
	}
}
