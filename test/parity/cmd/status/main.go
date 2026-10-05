// Command status writes docs/parity-status.md: every docs/PARITY.md row with the
// evidence the repository has for it (Parity tags on registrations, side-by-side
// scenarios) and plan 12's open triage requests. `make parity` runs it.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KaitouKid1412/mantle/test/parity"
)

func main() {
	out := flag.String("out", "docs/parity-status.md", "report path, relative to the module root")
	flag.Parse()
	root, err := moduleRoot()
	if err == nil {
		path := *out
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		err = run(root, path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "parity status:", err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	st, err := parity.BuildParityStatus(root)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(st.Markdown()), 0o644); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, r := range st.Rows {
		counts[st.RowState(r)]++
	}
	fmt.Printf("%s: %d rows · compared %d · tagged %d · scenario only %d · untagged %d · hand-off %d · gap %d\n",
		out, len(st.Rows), counts["compared"], counts["tagged"], counts["scenario only"], counts["untagged"],
		counts["hand-off"], counts["gap"])
	if len(st.Unknown) > 0 {
		fmt.Printf("unknown IDs: %v\n", st.Unknown)
	}
	return nil
}

// moduleRoot walks up from the working directory to the directory holding go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above the working directory")
		}
		dir = parent
	}
}
