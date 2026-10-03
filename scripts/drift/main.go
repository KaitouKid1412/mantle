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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
		knownOut  = fs.String("known", "internal/cli/known_engine.json", "with -accept, also write the engine tables mantle embeds here")
		catalog   = fs.String("catalog", "auto", "mantle-ui catalog JSON file; 'auto' runs go run ./cmd/mantle-ui catalog --json; 'none' skips")
		noEngine  = fs.Bool("no-engine", false, "skip the zero-token engine session (commands, tools, output styles, models)")
		runtime   = fs.String("runtime", "", "run mantle's startup drift check with this state file and print its notice")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *runtime != "" {
		s, ran, err := cli.RuntimeDrift(context.Background(), *runtime)
		if err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			return 2
		}
		fmt.Printf("ran=%v engine=%s known=%s notice=%q errors=%q\n", ran, s.EngineVersion, s.KnownVersion, s.Notice(), s.Errors)
		return 0
	}

	var snap Snapshot
	if *from != "" {
		var err error
		if snap, err = loadSnapshot(*from); err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			return 2
		}
	} else {
		c := &Collector{Claude: *claudeBin, Binary: *binary, Cache: *cache, Offline: *offline, Log: os.Stderr,
			NoEngine: *noEngine, SDKDiff: sdkDiffRunner()}
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
		if *knownOut != "" {
			if err := saveJSON(*knownOut, knownEngine(snap)); err != nil {
				fmt.Fprintln(os.Stderr, "drift:", err)
				return 2
			}
		}
	}

	known, err := loadKnown(*baseline, *parity)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drift:", err)
		return 2
	}
	if known.Catalog, err = loadCatalog(context.Background(), *catalog); err != nil {
		fmt.Fprintf(os.Stderr, "drift: catalog unavailable, using PARITY.md and the baseline only: %v\n", err)
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

// knownEngine extracts the engine tables mantle embeds for its runtime drift check:
// command names with aliases, and tool names.
func knownEngine(s Snapshot) cli.KnownEngine {
	k := cli.KnownEngine{ClaudeVersion: s.ClaudeVersion, Commands: []string{}, Tools: []string{}}
	if l := s.List(KindSlash); l.Available() {
		for _, it := range l.Items {
			k.Commands = append(k.Commands, it.Name)
			if a := it.Attrs["aliases"]; a != "" {
				k.Commands = append(k.Commands, strings.Split(a, ",")...)
			}
		}
	}
	if l := s.List(KindTool); l.Available() {
		for _, it := range l.Items {
			k.Tools = append(k.Tools, it.Name)
		}
	}
	slices.Sort(k.Commands)
	k.Commands = slices.Compact(k.Commands)
	slices.Sort(k.Tools)
	return k
}

// sdkDiffRunner runs plan 02's scripts/sdk-diff when the tree has it. Its JSON output is
// read tolerantly: an array of names, or of objects with "name" and optional "kind".
func sdkDiffRunner() func(ctx context.Context) ([]Item, error) {
	if st, err := os.Stat("scripts/sdk-diff"); err != nil || !st.IsDir() {
		return nil
	}
	return func(ctx context.Context) ([]Item, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "go", "run", "./scripts/sdk-diff", "-json").Output()
		if err != nil {
			return nil, fmt.Errorf("scripts/sdk-diff -json: %w", err)
		}
		return parseSDKDiff(out)
	}
}

func parseSDKDiff(out []byte) ([]Item, error) {
	var names []string
	if json.Unmarshal(out, &names) == nil {
		items := make([]Item, 0, len(names))
		for _, n := range names {
			items = append(items, Item{Name: n})
		}
		return items, nil
	}
	var objs []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(out, &objs); err != nil {
		return nil, fmt.Errorf("scripts/sdk-diff output: %w", err)
	}
	var items []Item
	for _, o := range objs {
		if o.Name != "" {
			items = append(items, Item{Name: o.Name, Scope: o.Kind})
		}
	}
	return items, nil
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
