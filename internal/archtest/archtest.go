package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Module is mantle's module path.
const Module = "github.com/KaitouKid1412/mantle"

// Violation is one forbidden import.
type Violation struct {
	Pkg, Import, Rule string
}

func (v Violation) String() string { return fmt.Sprintf("%s imports %s: %s", v.Pkg, v.Import, v.Rule) }

// Package is the subset of `go list -json` output the rules need.
type Package struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Standard     bool
}

// List runs `go list -json` for ./... in the module at root (with the given build tags,
// so per-area tags can be checked) and decodes the result.
func List(root string, tags ...string) ([]Package, error) {
	args := []string{"list", "-e", "-json=ImportPath,Imports,TestImports,XTestImports,Standard"}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "./...")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, stderr.String())
	}
	var pkgs []Package
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p Package
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// Check applies mantle's import rules to pkgs:
//
//  1. features/X never imports features/Y (X ≠ Y); only features/all and commands link
//     areas together.
//  2. mods/... imports only pkg/... from this module (plus the stdlib and third-party
//     modules).
//  3. pkg/... never imports internal/..., features/... or mods/....
//  4. internal/... never imports features/... or mods/... (so rule 1 holds transitively).
//  5. cmd/mantle and internal/launcher (the protected launcher) use only the stdlib and
//     internal/launcher.
func Check(pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		rel, ok := relPath(p.ImportPath)
		if !ok {
			continue
		}
		imports := map[string]bool{} // import → only from tests
		for _, imp := range p.Imports {
			imports[imp] = false
		}
		for _, imp := range append(append([]string(nil), p.TestImports...), p.XTestImports...) {
			if _, ok := imports[imp]; !ok {
				imports[imp] = true
			}
		}
		for imp, testOnly := range imports {
			if testOnly && under(strings.TrimPrefix(imp, Module+"/"), "internal/testkit") {
				continue // test infrastructure may be used by any package's tests
			}
			if v, bad := checkOne(rel, imp); bad {
				out = append(out, Violation{Pkg: p.ImportPath, Import: imp, Rule: v})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pkg != out[j].Pkg {
			return out[i].Pkg < out[j].Pkg
		}
		return out[i].Import < out[j].Import
	})
	return out
}

func checkOne(rel, imp string) (rule string, bad bool) {
	impRel, internal := relPath(imp)
	isStd := !strings.Contains(strings.SplitN(imp, "/", 2)[0], ".")

	switch {
	case under(rel, "cmd/mantle") || under(rel, "internal/launcher"):
		if internal && !under(impRel, "internal/launcher") {
			return "the launcher may import only the stdlib and internal/launcher", true
		}
		if !internal && !isStd {
			return "the launcher may import only the stdlib", true
		}
		return "", false
	}
	if !internal {
		return "", false
	}
	switch {
	case under(rel, "features"):
		if under(rel, "features/all") {
			return "", false
		}
		if under(impRel, "features") && area(rel) != area(impRel) {
			return "features must not import other feature areas; talk through pkg/ext IDs and messages", true
		}
		if under(impRel, "mods") {
			return "features must not import mods", true
		}
	case under(rel, "mods"):
		if rel == "mods" && under(impRel, "mods") {
			return "", false // the root package links every mod (mods/link_<id>.go)
		}
		if !under(impRel, "pkg") && !sameMod(rel, impRel) {
			return "mods may import only pkg/... from mantle", true
		}
	case under(rel, "pkg"):
		if under(impRel, "internal") || under(impRel, "features") || under(impRel, "mods") || under(impRel, "cmd") {
			return "pkg must not import internal, features, mods or cmd", true
		}
	case under(rel, "internal"):
		if under(impRel, "features") || under(impRel, "mods") {
			return "internal must not import features or mods", true
		}
	}
	return "", false
}

// relPath returns the path relative to the module ("pkg/ext") and whether p is in it.
func relPath(p string) (string, bool) {
	if p == Module {
		return "", true
	}
	rel, ok := strings.CutPrefix(p, Module+"/")
	return rel, ok
}

func under(rel, dir string) bool { return rel == dir || strings.HasPrefix(rel, dir+"/") }

// area is the first two path elements: "features/input/sub" → "features/input".
func area(rel string) string {
	parts := strings.SplitN(rel, "/", 3)
	if len(parts) < 2 {
		return rel
	}
	return parts[0] + "/" + parts[1]
}

// sameMod reports whether two paths are in the same mod (mods/<id>/...).
func sameMod(a, b string) bool { return under(b, "mods") && area(a) == area(b) }

// ModuleRoot finds the directory holding mantle's go.mod, starting at dir.
func ModuleRoot(dir string) (string, error) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && bytes.Contains(data, []byte("module "+Module)) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("archtest: mantle go.mod not found")
		}
		dir = parent
	}
}
