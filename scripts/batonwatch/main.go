// Command batonwatch records file-baton's lock state while several Claude Code
// sessions edit this checkout, then checks that the locks behaved.
//
//	go run ./scripts/batonwatch record   # poll until interrupted
//	go run ./scripts/batonwatch report   # timeline + invariant checks (markdown)
//	go run ./scripts/batonwatch live     # one-screen table, refreshed every second
//
// file-baton keeps its state in .git/file-baton/state.json and a decision log in
// .git/file-baton/log. The log does not name the files a stop or session-end
// releases, so record diffs successive state snapshots to see them. Events go to
// .git/file-baton-watch/events.jsonl. Dev-only: not built into bin/.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	interval := fs.Duration("interval", 500*time.Millisecond, "state poll interval (record)")
	out := fs.String("o", "docs/plans/13-baton-report.md", "report file (report; - for stdout)")
	_ = fs.Parse(args)

	p, err := locate()
	if err != nil {
		fatal(err)
	}
	switch cmd {
	case "record":
		err = record(p, *interval)
	case "report":
		err = writeReport(p, *out)
	case "live":
		err = live(p)
	default:
		usage()
	}
	if err != nil {
		fatal(err)
	}
}

// paths are the files batonwatch reads and writes.
type paths struct {
	Repo   string // worktree root
	State  string // file-baton state.json
	Log    string // file-baton log
	Events string // our events.jsonl
}

func locate() (paths, error) {
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return paths{}, err
	}
	common, err := git("rev-parse", "--git-common-dir")
	if err != nil {
		return paths{}, err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	fb := filepath.Join(common, "file-baton")
	return paths{
		Repo:   top,
		State:  filepath.Join(fb, "state.json"),
		Log:    filepath.Join(fb, "log"),
		Events: filepath.Join(common, "file-baton-watch", "events.jsonl"),
	}, nil
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: batonwatch record|report|live [-interval 500ms] [-o file]")
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "batonwatch:", err)
	os.Exit(1)
}
