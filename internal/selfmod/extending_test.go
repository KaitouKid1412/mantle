package selfmod

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/archtest"
)

// TestExtendingExamplesCompile compiles every complete Go example in
// docs/EXTENDING.md (blocks that start with a package clause) against the
// real pkg/ext, through a go vet overlay: nothing is written into the repo.
// The builder learns the API from this guide, so it must not drift.
func TestExtendingExamplesCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go vet")
	}
	wd, _ := os.Getwd()
	root, err := archtest.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(root, "docs", "EXTENDING.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```go\n(.*?)```").FindAllStringSubmatch(string(doc), -1)
	pkgRe := regexp.MustCompile(`^package (\w+)`)
	tmp := t.TempDir()
	replace := map[string]string{}
	dirs := map[string]bool{}
	n := 0
	for _, b := range blocks {
		src := dedent(b[1])
		m := pkgRe.FindStringSubmatch(src)
		if m == nil {
			continue // a fragment
		}
		// package mods is the link file; any other package is a mod of that name.
		target := filepath.Join(root, "mods", m[1], "example_doc.go")
		if m[1] == "mods" {
			target = filepath.Join(root, "mods", "link_example_doc.go")
			// The link imports mods/<id>; map that id to the package's directory.
			if im := regexp.MustCompile(`mantle/mods/([a-z0-9-]+)"`).FindStringSubmatch(src); im != nil {
				src = strings.Replace(src, im[0], "mantle/mods/"+strings.ReplaceAll(im[1], "-", "")+`"`, 1)
			}
		} else {
			dirs["./mods/"+m[1]] = true
		}
		n++
		file := filepath.Join(tmp, strings.ReplaceAll(strings.TrimPrefix(target, root+"/"), "/", "_"))
		if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		replace[target] = file
	}
	if n < 4 {
		t.Fatalf("found %d complete examples in EXTENDING.md; expected the link file and three mods", n)
	}
	overlay, _ := json.Marshal(map[string]any{"Replace": replace})
	overlayFile := filepath.Join(tmp, "overlay.json")
	os.WriteFile(overlayFile, overlay, 0o644)
	args := []string{"build", "-overlay=" + overlayFile, "./mods"}
	for d := range dirs {
		args = append(args, d)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("EXTENDING.md examples do not compile: %v\n%s", err, out)
	}
}

// dedent removes the indentation common to every non-empty line (examples
// inside list items are indented in Markdown).
func dedent(s string) string {
	lines := strings.Split(s, "\n")
	min := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if n := len(ln) - len(strings.TrimLeft(ln, " ")); min < 0 || n < min {
			min = n
		}
	}
	for i, ln := range lines {
		if len(ln) >= min && min > 0 {
			lines[i] = ln[min:]
		}
	}
	return strings.Join(lines, "\n")
}
