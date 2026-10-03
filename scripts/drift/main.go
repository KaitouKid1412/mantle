// Command drift detects when a new claude version adds or removes flags, subcommands,
// keybinding actions and contexts, or settings keys that mantle doesn't know about.
//
//	go run ./scripts/drift              # collect, compare, write docs/parity-drift.{md,json}
//	go run ./scripts/drift -offline     # use the cached settings schema
//	go run ./scripts/drift -from FILE   # compare a saved snapshot instead of collecting
//	go run ./scripts/drift -accept      # make the collected lists the new baseline
//
// It only ever runs `claude --version`, `claude --help` and `claude <known
// subcommand> --help`. Exit status: 0 clean, 1 unclassified or mismatched items,
// 2 the tool itself failed.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	home, _ := os.UserHomeDir()
	var (
		claudeBin = fs.String("claude", "", "claude binary (default: $MANTLE_CLAUDE_BIN or claude on PATH)")
		binary    = fs.String("binary", "", "file to scan for keybindings (default: the claude binary's symlink target)")
		cache     = fs.String("cache", filepath.Join(home, ".mantle", "cache", "drift"), "cache directory")
		offline   = fs.Bool("offline", false, "don't fetch the settings schema; use the cached copy")
		from      = fs.String("from", "", "compare this saved snapshot instead of collecting")
		save      = fs.String("save", "", "also write the collected snapshot here")
		baseline  = fs.String("baseline", "scripts/drift/baseline.json", "accepted snapshot")
		parity    = fs.String("parity", "docs/PARITY.md", "PARITY.md")
		outMD     = fs.String("out", "docs/parity-drift.md", "markdown report ('' to skip)")
		outJSON   = fs.String("json", "docs/parity-drift.json", "JSON report ('' to skip)")
		accept    = fs.Bool("accept", false, "write the collected snapshot to the baseline")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var snap Snapshot
	if *from != "" {
		var err error
		if snap, err = loadSnapshot(*from); err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			return 2
		}
	} else {
		c := &Collector{Claude: *claudeBin, Binary: *binary, Cache: *cache, Offline: *offline, Log: os.Stderr}
		snap = c.Collect(context.Background())
		if err := os.MkdirAll(*cache, 0o755); err == nil {
			_ = saveJSON(filepath.Join(*cache, "snapshot.json"), snap)
		}
	}
	if *save != "" {
		if err := saveJSON(*save, snap); err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			return 2
		}
	}
	if *accept {
		if err := saveJSON(*baseline, snap); err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			return 2
		}
		fmt.Fprintf(os.Stderr, "drift: baseline %s now holds claude %s\n", *baseline, snap.ClaudeVersion)
	}

	known, err := loadKnown(*baseline, *parity)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drift:", err)
		return 2
	}
	r := Compare(snap, known)
	r.Generated = time.Now().UTC().Format("2006-01-02")
	if err := writeReports(r, *outMD, *outJSON); err != nil {
		fmt.Fprintln(os.Stderr, "drift:", err)
		return 2
	}
	for _, s := range r.Summary {
		if s.Error != "" {
			fmt.Fprintf(os.Stderr, "drift: %-20s not collected: %s\n", s.Kind, s.Error)
			continue
		}
		fmt.Fprintf(os.Stderr, "drift: %-20s %4d items, %d unclassified, %d mismatched, %d removed, %d new\n",
			s.Kind, s.Items, s.Unclassified, s.Mismatch, s.Removed, s.New)
	}
	if r.Failed() {
		fmt.Fprintf(os.Stderr, "drift: unclassified items found; see %s\n", *outMD)
		return 1
	}
	return 0
}

// loadKnown gathers mantle's tables, PARITY.md's indexes and the baseline. A missing
// baseline is an empty one.
func loadKnown(baselinePath, parityPath string) (Known, error) {
	k := Known{Flags: cli.Flags, Subcommands: cli.Subcommands, Contexts: ext.Contexts}
	for _, a := range ext.ClaudeActions {
		k.Actions = append(k.Actions, string(a))
	}
	if b, err := loadSnapshot(baselinePath); err == nil {
		k.Baseline = b
	} else if !os.IsNotExist(err) {
		return k, err
	}
	f, err := os.Open(parityPath)
	if err != nil {
		return k, err
	}
	defer f.Close()
	k.Parity, err = parseParity(f)
	return k, err
}

func writeReports(r Report, md, js string) error {
	if md != "" {
		var b bytes.Buffer
		if err := writeMarkdown(&b, r); err != nil {
			return err
		}
		if err := os.WriteFile(md, b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	if js != "" {
		return saveJSON(js, r)
	}
	return nil
}
