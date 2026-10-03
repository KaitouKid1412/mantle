package chrome

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// chromeStories returns every story registered by this package's features.
func chromeStories(t *testing.T) []ext.Story {
	t.Helper()
	var out []ext.Story
	for _, f := range ext.Pending() {
		if !strings.HasPrefix(f.ID, "chrome.") {
			continue
		}
		r, err := exttest.Setup(f)
		if err != nil {
			t.Fatalf("%s setup: %v", f.ID, err)
		}
		out = append(out, r.Stories...)
	}
	slices.SortFunc(out, func(a, b ext.Story) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// TestStories renders every story at 60/100/160 and compares with goldens
// (go test ./features/chrome -run TestStories -update rewrites them).
func TestStories(t *testing.T) {
	stories := chromeStories(t)
	if len(stories) < 10 {
		t.Fatalf("only %d stories", len(stories))
	}
	for _, s := range stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) {
			testkit.RunStory(t, s)
		})
	}
}

// TestScreenReaderStories: in screen-reader mode nothing draws boxes or rules and no
// colour codes are emitted.
func TestScreenReaderStories(t *testing.T) {
	for _, s := range chromeStories(t) {
		c := exttest.NewCtx()
		c.A11y.ScreenReader = true
		out, err := testkit.RenderStoryCtx(c, s, 100)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(out, "─│╭╮╰╯┌┐└┘") {
			t.Errorf("%s draws box characters in screen-reader mode:\n%s", s.ID, out)
		}
		if strings.Contains(ansi.Strip(out), "\x1b") || strings.Contains(out, "\x1b[") {
			t.Errorf("%s emits styling in screen-reader mode: %q", s.ID, out)
		}
	}
}
