// Command sidebyside runs the parity scenarios against interactive targets (claude
// and mantle) with the scripted fakeapi, and writes a side-by-side report.
//
//	go run ./test/parity/cmd/sidebyside -targets claude -run plain
//
// It costs nothing: every target talks to fakeapi with an isolated config.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/test/parity"
)

func main() {
	scenarios := flag.String("scenarios", "test/parity/scenarios", "directory of .scn files")
	targets := flag.String("targets", "claude,mantle", "comma-separated targets; the first is the reference")
	out := flag.String("out", "test/parity/out", "report directory (gitignored)")
	allow := flag.String("allow", "test/parity/allowlist.txt", "allowlist of intentional differences")
	runRe := flag.String("run", "", "only scenarios whose name matches this regexp")
	claudeBin := flag.String("claude", "", "claude binary (default: claude on PATH)")
	timeout := flag.Duration("timeout", 2*time.Minute, "per-scenario timeout")
	strict := flag.Bool("strict", false, "exit 1 when any checkpoint differs or is missing")
	keep := flag.Bool("keep", false, "keep workspaces for debugging")
	raw := flag.Bool("raw", false, "save each terminal's raw output under <out>/raw")
	flag.Parse()

	if err := run(*scenarios, *targets, *out, *allow, *runRe, *claudeBin, *timeout, *strict, *keep, *raw); err != nil {
		fmt.Fprintln(os.Stderr, "sidebyside:", err)
		os.Exit(1)
	}
}

func run(dir, targetList, out, allowPath, runRe, claudeBin string, timeout time.Duration, strict, keep, raw bool) error {
	scs, err := parity.LoadScenarios(dir)
	if err != nil {
		return err
	}
	if runRe != "" {
		re, err := regexp.Compile(runRe)
		if err != nil {
			return err
		}
		var kept []*parity.Scenario
		for _, sc := range scs {
			if re.MatchString(sc.Name) {
				kept = append(kept, sc)
			}
		}
		scs = kept
	}
	if len(scs) == 0 {
		return fmt.Errorf("no scenarios in %s", dir)
	}
	var tgs []parity.Target
	var notes []string
	for _, name := range strings.Split(targetList, ",") {
		switch strings.TrimSpace(name) {
		case "claude":
			tgs = append(tgs, parity.ClaudeTarget{Bin: claudeBin})
			if v := claudeVersion(claudeBin); v != "" {
				notes = append(notes, "Reference engine: claude "+v+".")
			}
		case "mantle":
			bin, err := parity.BuildMantleUI(out + "/.bin")
			if err != nil {
				return err
			}
			tgs = append(tgs, parity.MantleTarget{Bin: bin})
		default:
			return fmt.Errorf("unknown target %q (available: claude, mantle)", name)
		}
	}
	al, err := parity.LoadAllowlist(allowPath)
	if err != nil {
		return err
	}
	rep := &parity.Report{Normalizer: parity.DefaultNormalizer(), Allow: al, Generated: time.Now(), Notes: notes}
	for _, tg := range tgs {
		rep.Targets = append(rep.Targets, tg.Name())
	}
	logf := func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
	rawDir := ""
	if raw {
		rawDir = filepath.Join(out, "raw")
	}
	for _, sc := range scs {
		run := parity.ScenarioRun{Scenario: sc, Results: map[string]*parity.Result{}}
		for _, tg := range tgs {
			res := parity.Run(context.Background(), tg, sc, parity.RunOptions{Timeout: timeout, KeepWorkspace: keep, Logf: logf, RawDir: rawDir})
			status := "ok"
			if res.Err != nil {
				status = res.Err.Error()
			}
			logf("%-24s %-8s %6s  %s", sc.Name, tg.Name(), res.Duration.Round(100*time.Millisecond), status)
			run.Results[tg.Name()] = res
		}
		rep.Runs = append(rep.Runs, run)
	}
	path, err := rep.Write(out)
	if err != nil {
		return err
	}
	fmt.Println(path)
	if strict {
		for _, d := range rep.Diffs() {
			if d.Status == parity.StatusDiff || d.Status == parity.StatusMissing {
				return fmt.Errorf("%s/%s: %s", d.Scenario, d.Checkpoint, d.Status)
			}
		}
	}
	return nil
}

// claudeVersion is `claude --version` (local, no API call), or "".
func claudeVersion(bin string) string {
	if bin == "" {
		bin = "claude"
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
