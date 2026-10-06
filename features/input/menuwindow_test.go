package input

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// claudeList mirrors the start and end of claude 2.1.290's / menu at 100 columns:
// names, and whether the description wraps to a second row there.
var claudeList = []struct {
	name  string
	wraps bool
}{
	{"add-dir", false}, {"autocompact", false}, {"background", false}, {"branch", false},
	{"btw", true}, {"bug", false}, {"cd", false}, {"clear", true}, {"color", false},
	{"compact", false}, {"config", false}, {"context", false}, {"copy", true}, {"diff", false},
	{"doctor", true}, {"effort", false}, {"export", false}, {"fast", false},
	{"team-onboarding", false}, {"update-config", true}, {"verify", true}, {"workflow-authoring", true},
}

func claudeMenu(sel int) *completion {
	m := &completion{kind: compSlash, sel: sel}
	for _, it := range claudeList {
		desc := "Short description"
		if it.wraps {
			desc = strings.Repeat("long words ", 8) // 88 columns: two rows at width 100
		}
		m.items = append(m.items, compItem{value: it.name, label: "/" + it.name, desc: desc})
	}
	return m
}

// names returns the item names shown in rendered menu lines, "❯" marking the
// selection.
func names(lines []string) string {
	var out []string
	for _, l := range lines {
		l = xansi.Strip(l)
		if !strings.HasPrefix(l, "  ❯ /") && !strings.HasPrefix(l, "    /") {
			continue
		}
		f := strings.Fields(l)
		if f[0] == "❯" {
			out = append(out, "❯"+f[1][1:])
		} else {
			out = append(out, f[0][1:])
		}
	}
	return strings.Join(out, " ")
}

// TestFullscreenMenuWindow: five items in five rows, two items above the selection,
// clamped at the ends, trimmed below the selection first (claude 2.1.290 frames).
func TestFullscreenMenuWindow(t *testing.T) {
	th := theme.Default()
	n := len(claudeList)
	for _, tc := range []struct {
		sel  int
		want string
	}{
		{0, "❯add-dir autocompact background branch"},
		{2, "add-dir autocompact ❯background branch"},
		{3, "autocompact background ❯branch btw"},
		{4, "background branch ❯btw bug"},
		{5, "branch btw ❯bug cd"},
		{10, "color compact ❯config context"},
		{n - 2, "team-onboarding update-config ❯verify"},
		{n - 1, "verify ❯workflow-authoring"},
	} {
		lines := claudeMenu(tc.sel).viewOverlay(&th, 100)
		if got := names(lines); got != tc.want {
			t.Errorf("sel %d: %q, want %q", tc.sel, got, tc.want)
		}
		if len(lines) > fsMenuRows {
			t.Errorf("sel %d: %d rows", tc.sel, len(lines))
		}
	}
	// One-row items at the end of the list: five items, the selection last.
	m := &completion{kind: compSlash}
	for _, s := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		m.items = append(m.items, compItem{value: s, label: "/" + s, desc: "d"})
	}
	m.sel = 6
	if got := names(m.viewOverlay(&th, 100)); got != "c d e f ❯g" {
		t.Errorf("end of a one-row list: %q", got)
	}
	// Column layout: "  ❯ /name" padded so descriptions start at column 32.
	l := xansi.Strip(claudeMenu(0).viewOverlay(&th, 100)[0])
	if !strings.HasPrefix(l, "  ❯ /add-dir                    Short") {
		t.Errorf("layout: %q", l)
	}
}

// TestInlineMenuWindow: half the terminal height in rows; filled from the top, and
// once scrolled the rows above the selection fill up to half the budget.
func TestInlineMenuWindow(t *testing.T) {
	th := theme.Default()
	for _, tc := range []struct {
		sel, budget int
		want        string
	}{
		// 100x30: twelve items in fourteen rows (the next item wraps).
		{0, 15, "❯add-dir autocompact background branch btw bug cd clear color compact config context"},
		// 100x20 after thirteen downs: compact first.
		{13, 10, "compact config context copy ❯diff doctor effort export"},
	} {
		lines := claudeMenu(tc.sel).view(&th, 100, tc.budget)
		if got := names(lines); got != tc.want {
			t.Errorf("sel %d budget %d: %q, want %q", tc.sel, tc.budget, got, tc.want)
		}
		if len(lines) > tc.budget {
			t.Errorf("sel %d: %d rows > %d", tc.sel, len(lines), tc.budget)
		}
	}
}
