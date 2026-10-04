package transcript

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// fixtureFiles lists engine stdout recordings: plan 02's (recorded from the
// real engine) and plan 03's own.
func fixtureFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"02", "03"} {
		m, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", dir, "*.ndjson"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	return files
}

// TestFixtureReplays replays every fixture through the store and commit policy
// at 60/100/160 columns and compares the scrollback with a golden. A fixture
// without a golden yet is skipped until `go test -update` records one.
func TestFixtureReplays(t *testing.T) {
	files := fixtureFiles(t)
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	update := false
	if f := flag.Lookup("update"); f != nil {
		update = f.Value.String() == "true"
	}
	for _, path := range files {
		plan := filepath.Base(filepath.Dir(path))
		name := plan + "_" + strings.TrimSuffix(filepath.Base(path), ".ndjson")
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join("testdata", t.Name()+".golden")); err != nil && !update {
				t.Skipf("no golden for %s yet; run go test -run TestFixtureReplays -update", path)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			for _, w := range []int{60, 100, 160} {
				g := newRig(t, w+1) // print width w
				g.c.SettingsV = exttest.NewSettings(map[string]any{"timeZone": "UTC"})
				g.deliver(ext.SettingsMsg{})
				g.send(onlyJSON(string(raw)))
				for _, blk := range g.c.Printed {
					checkLines(t, strings.Split(blk, "\n"), w)
				}
				fmt.Fprintf(&b, "── width %d ──\n", w)
				for _, blk := range g.c.Printed {
					b.WriteString(blk)
					b.WriteByte('\n')
				}
				if live := g.f.live.View(g.c, areaInline(w+1)).Text; live != "" {
					b.WriteString("── live ──\n" + live + "\n")
				}
			}
			golden.RequireEqual(t, b.String())
		})
	}
}

// onlyJSON drops lines that are not JSON objects (debug output in recordings).
func onlyJSON(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "{") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func areaInline(w int) ext.Area { return ext.Area{Width: w, MaxHeight: 30, Mode: ext.Inline} }
