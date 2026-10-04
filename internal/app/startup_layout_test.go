package app

import (
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestStartupLayout(t *testing.T) {
	cases := []struct {
		name string
		in   LayoutInputs
		want ext.LayoutMode
	}{
		{"unset, engine default fullscreen (2.1.289+)", LayoutInputs{EngineDefaultTUI: "fullscreen"}, ext.Fullscreen},
		{"unset, engine default inline (older)", LayoutInputs{EngineDefaultTUI: "default"}, ext.Inline},
		{"unset, unknown default", LayoutInputs{}, ext.Inline},
		{"explicit default beats engine fullscreen", LayoutInputs{TUI: "default", EngineDefaultTUI: "fullscreen"}, ext.Inline},
		{"explicit fullscreen beats engine inline", LayoutInputs{TUI: "fullscreen", EngineDefaultTUI: "default"}, ext.Fullscreen},
		{"alt screen disabled beats explicit fullscreen", LayoutInputs{TUI: "fullscreen", NoAltScreen: true}, ext.Inline},
		{"alt screen disabled beats engine fullscreen", LayoutInputs{EngineDefaultTUI: "fullscreen", NoAltScreen: true}, ext.Inline},
		{"screen reader is inline", LayoutInputs{EngineDefaultTUI: "fullscreen", ScreenReader: true}, ext.Inline},
		{"no-flicker asks for fullscreen", LayoutInputs{NoFlicker: true, EngineDefaultTUI: "default"}, ext.Fullscreen},
		{"explicit default beats no-flicker", LayoutInputs{TUI: "default", NoFlicker: true}, ext.Inline},
	}
	for _, c := range cases {
		if got := StartupLayout(c.in); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
