package testkit

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// DefaultWidths are the widths a story renders at when it lists none (also what
// `mantle-ui selftest` checks).
var DefaultWidths = []int{60, 100, 160}

// RenderStory renders a story at one width with a fresh exttest.Ctx (default theme,
// frozen clock). It returns the output and an error if the story panics or a line is
// wider than the width.
func RenderStory(s ext.Story, width int) (out string, err error) {
	c := exttest.NewCtx()
	c.W = width
	return RenderStoryCtx(c, s, width)
}

// RenderStoryCtx is RenderStory with a caller-provided Ctx.
func RenderStoryCtx(c ext.Ctx, s ext.Story, width int) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("story %s at width %d panicked: %v", s.ID, width, r)
		}
	}()
	if s.Render == nil {
		return "", fmt.Errorf("story %s has no Render", s.ID)
	}
	r := s.Render(c, ext.Area{Width: width, Mode: c.Layout()})
	for i, line := range strings.Split(r.Text, "\n") {
		if w := ansi.StringWidth(line); w > width {
			return r.Text, fmt.Errorf("story %s at width %d: line %d is %d cells wide", s.ID, width, i+1, w)
		}
	}
	return r.Text, nil
}

// RunStory renders a story at each width (the story's own, or widths, or
// DefaultWidths) as subtests named w<width>, checks it fits, and compares it with
// testdata/<TestName>/w<width>.golden.
func RunStory(t *testing.T, s ext.Story, widths ...int) {
	t.Helper()
	if len(widths) == 0 {
		widths = s.Widths
	}
	if len(widths) == 0 {
		widths = DefaultWidths
	}
	for _, w := range widths {
		t.Run(fmt.Sprintf("w%d", w), func(t *testing.T) {
			out, err := RenderStory(s, w)
			if err != nil {
				t.Fatal(err)
			}
			RequireGolden(t, out)
		})
	}
}
