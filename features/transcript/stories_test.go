package transcript

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/render"
)

func setupFeature(t testing.TB) (*Feature, *exttest.Registrar) {
	t.Helper()
	f := New(ext.MainEngine)
	r := exttest.NewRegistrar()
	if err := f.Setup(r); err != nil {
		t.Fatal(err)
	}
	return f, r
}

// TestStories renders every story at 60/100/160 columns: goldens plus width and
// escape-sequence invariants.
func TestStories(t *testing.T) {
	_, r := setupFeature(t)
	if len(r.Stories) == 0 {
		t.Fatal("no stories registered")
	}
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) {
			var b strings.Builder
			for _, w := range []int{60, 100, 160} {
				c := exttest.NewCtx()
				c.W = w
				out := s.Render(c, ext.Area{Width: w})
				lines := strings.Split(out.Text, "\n")
				if out.Text == "" {
					t.Fatalf("story %s rendered nothing at %d", s.ID, w)
				}
				checkLines(t, lines, w)
				fmt.Fprintf(&b, "── width %d ──\n%s\n", w, out.Text)
			}
			golden.RequireEqual(t, b.String())
		})
	}
}

func checkLines(t *testing.T, lines []string, width int) {
	t.Helper()
	for i, l := range lines {
		if w := render.Width(l); w > width {
			t.Errorf("line %d is %d wide (limit %d): %q", i, w, width, render.Strip(l))
		}
		if strings.ContainsAny(render.Strip(l), "\t\r\x07\x00") {
			t.Errorf("line %d holds control characters: %q", i, l)
		}
	}
}

// TestPreviewSession prints the sample session for eyeballing:
//
//	MANTLE_PREVIEW=1 MANTLE_WIDTH=100 go test ./features/transcript -run TestPreviewSession -v
func TestPreviewSession(t *testing.T) {
	if os.Getenv("MANTLE_PREVIEW") == "" {
		t.Skip("set MANTLE_PREVIEW=1")
	}
	w := 100
	if n, err := strconv.Atoi(os.Getenv("MANTLE_WIDTH")); err == nil {
		w = n
	}
	_, r := setupFeature(t)
	for _, s := range r.Stories {
		if s.ID != FeatureID+"/session" {
			continue
		}
		c := exttest.NewCtx()
		c.W = w
		for _, l := range strings.Split(s.Render(c, ext.Area{Width: w}).Text, "\n") {
			if os.Getenv("MANTLE_PLAIN") != "" {
				l = render.Strip(l)
			}
			fmt.Println(l + "\x1b[0m")
		}
	}
}
