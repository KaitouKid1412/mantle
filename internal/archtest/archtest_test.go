package archtest

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// TestImportRules runs the rules over the whole module. It is what `make archtest` and
// the /mantle pipeline run.
func TestImportRules(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) < 10 {
		t.Fatalf("go list found only %d packages", len(pkgs))
	}
	for _, v := range Check(pkgs) {
		t.Error(v)
	}
}

func TestRules(t *testing.T) {
	m := Module + "/"
	pkgs := []Package{
		{ImportPath: m + "features/input", Imports: []string{m + "features/turn", m + "pkg/ext", m + "internal/config", "fmt"}},
		{ImportPath: m + "features/input/sub", Imports: []string{m + "features/input"}},
		{ImportPath: m + "features/all", Imports: []string{m + "features/input", m + "features/turn"}},
		{ImportPath: m + "mods/pirate", Imports: []string{m + "pkg/ext", m + "internal/app", m + "mods/pirate/sub", m + "mods/other", "charm.land/lipgloss/v2"}},
		{ImportPath: m + "mods", Imports: []string{m + "mods/pirate", m + "mods/other", m + "internal/app"}},
		{ImportPath: m + "pkg/ui", TestImports: []string{m + "internal/testkit", m + "internal/app"}, Imports: []string{m + "pkg/theme"}},
		{ImportPath: m + "internal/app", Imports: []string{m + "features/chrome"}},
		{ImportPath: m + "cmd/mantle", Imports: []string{"os", m + "internal/launcher", m + "pkg/ext", "github.com/creack/pty"}},
		{ImportPath: "example.com/other", Imports: []string{m + "features/x"}},
	}
	var got []string
	for _, v := range Check(pkgs) {
		got = append(got, strings.TrimPrefix(v.Pkg, m)+" -> "+strings.TrimPrefix(v.Import, m))
	}
	want := []string{
		"cmd/mantle -> github.com/creack/pty",
		"cmd/mantle -> pkg/ext",
		"features/input -> features/turn",
		"internal/app -> features/chrome",
		"mods -> internal/app",
		"mods/pirate -> internal/app",
		"mods/pirate -> mods/other",
		"pkg/ui -> internal/app",
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("violations:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
