package selfmod

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const stubGo = `#!/bin/sh
echo "$* | GOTOOLCHAIN=$GOTOOLCHAIN MANTLE_HOME=$MANTLE_HOME BASE=$ANTHROPIC_BASE_URL" >> "$STUB_DIR/go.calls"
sub="$1"
[ "$sub" = test ] && [ "$2" = -race ] && sub=race
[ -f "$STUB_DIR/$sub.sleep" ] && { echo $$ > "$STUB_DIR/$sub.pid"; sleep "$(cat "$STUB_DIR/$sub.sleep")"; }
[ -f "$STUB_DIR/$sub.out" ] && cat "$STUB_DIR/$sub.out"
if [ "$sub" = build ]; then
  out=""; prev=""
  for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
  [ -n "$out" ] && [ ! -f "$STUB_DIR/build.exit" ] && cp "$STUB_DIR/fake-ui" "$out" && chmod +x "$out"
fi
[ -f "$STUB_DIR/$sub.exit" ] && exit "$(cat "$STUB_DIR/$sub.exit")"
exit 0
`

const stubGofmt = `#!/bin/sh
[ -f "$STUB_DIR/gofmt.out" ] && cat "$STUB_DIR/gofmt.out"
exit 0
`

// fakeUI is a stand-in mantle-ui: "selftest" mode, else an interactive
// session that answers a prompt and /help, then exits on the next line.
const fakeUIScript = `#!/bin/sh
if [ "$1" = selftest ]; then
  if [ -f "$STUB_DIR/selftest.fail" ]; then echo "story chrome.footer: line exceeds width 60"; exit 1; fi
  echo "selftest ok"; exit 0
fi
[ -f "$STUB_DIR/ui.crash" ] && { echo "panic: boom"; exit 2; }
printf '\033[2J\033[H\033[1mmantle\033[0m \033]0;title\007ready\r\n> '
read line
printf 'reply to %s\r\n' "$line"
read line2
[ "$line2" = "/help" ] && printf '\033[32mHelp:\033[0m commands\r\n'
read q
exit "${SMOKE_EXIT:-0}"
`

type pipeFixture struct {
	t       *testing.T
	repo    Git
	base    string
	stubDir string
	logDir  string
	cfg     Config
}

func newPipeFixture(t *testing.T) *pipeFixture {
	t.Helper()
	f := &pipeFixture{t: t, repo: newRepo(t), stubDir: t.TempDir(), logDir: filepath.Join(t.TempDir(), "build")}
	f.base, _ = f.repo.HeadSHA()
	for name, content := range map[string]string{"go": stubGo, "gofmt": stubGofmt, "fake-ui": fakeUIScript} {
		if err := os.WriteFile(filepath.Join(f.stubDir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg = Config{
		Go:    filepath.Join(f.stubDir, "go"),
		Gofmt: filepath.Join(f.stubDir, "gofmt"),
		Env:   []string{"STUB_DIR=" + f.stubDir},
		ArchCheck: func(context.Context, string) ([]string, error) {
			data, _ := os.ReadFile(filepath.Join(f.stubDir, "arch.violations"))
			return strings.Fields(string(data)), nil
		},
		Smoke: SmokeConfig{Script: []SmokeAction{
			Expect(`mantle\s+ready`), Send("hello\r"), Expect(`reply to hello`),
			Send("/help\r"), Expect(`Help: commands`), Send("q\r"),
		}},
		Timeouts: map[StepID]time.Duration{StepSmoke: 10 * time.Second},
	}
	return f
}

func (f *pipeFixture) stub(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.stubDir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *pipeFixture) change(files map[string]string) { writeFiles(f.t, f.repo.Dir, files) }

func (f *pipeFixture) run(steps ...StepID) *Report {
	f.t.Helper()
	rep, err := NewPipeline(f.cfg).Run(context.Background(), Run{BuildID: "b1", Dir: f.repo.Dir, Base: f.base, LogDir: f.logDir, Steps: steps})
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

func (f *pipeFixture) calls() string {
	data, _ := os.ReadFile(filepath.Join(f.stubDir, "go.calls"))
	return string(data)
}

func stepIDs(rep *Report) []StepID {
	var ids []StepID
	for _, r := range rep.Results {
		ids = append(ids, r.Step)
	}
	return ids
}

func TestPipelineAllPass(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"mods/demo/demo.go": "package demo\n", "internal/engine/doc.go": "package engine\n"})
	rep := f.run()
	if !rep.OK {
		t.Fatalf("pipeline failed:\n%s", TrimForBuilder(rep))
	}
	if got := stepIDs(rep); !slices.Equal(got, AllSteps) {
		t.Errorf("steps = %v", got)
	}
	if r, _ := rep.Result(StepAPIDiff); !r.Skipped || r.Summary != "no changes under pkg/" {
		t.Errorf("apidiff = %+v", r)
	}
	if r, _ := rep.Result(StepGofmt); r.Skipped {
		t.Errorf("gofmt skipped with changed Go files: %+v", r)
	}
	if !slices.Equal(rep.Changed, []string{"internal/engine/doc.go", "mods/demo/demo.go"}) {
		t.Errorf("changed = %v", rep.Changed)
	}
	if rep.Binary == "" || !fileExists(rep.Binary) {
		t.Errorf("binary %q missing", rep.Binary)
	}
	calls := f.calls()
	for _, want := range []string{"vet ./... | GOTOOLCHAIN=local", "build -trimpath -o " + rep.Binary + " ./cmd/mantle-ui", "test ./... | GOTOOLCHAIN=local MANTLE_HOME=" + filepath.Join(f.logDir, "home") + " BASE=http://127.0.0.1:9", "test -race ./internal/engine/..."} {
		if !strings.Contains(calls, want) {
			t.Errorf("go calls missing %q:\n%s", want, calls)
		}
	}
	if strings.Contains(strings.Split(calls, "\n")[0], "BASE=http") {
		t.Errorf("vet should not get the test safety env: %s", calls)
	}
	var onDisk Report
	data, err := os.ReadFile(filepath.Join(f.logDir, "report.json"))
	if err != nil || json.Unmarshal(data, &onDisk) != nil || !onDisk.OK || len(onDisk.Results) != 9 {
		t.Errorf("report.json: %v %s", err, data)
	}
	for _, r := range rep.Results {
		if !fileExists(r.LogPath) {
			t.Errorf("%s: log %s missing", r.Step, r.LogPath)
		}
	}
	smokeLog, _ := os.ReadFile(mustResult(t, rep, StepSmoke).LogPath)
	if !strings.Contains(string(smokeLog), "[smoke] exited with code 0") {
		t.Errorf("smoke log:\n%s", smokeLog)
	}
	if TrimForBuilder(rep) != "" {
		t.Error("TrimForBuilder of a passing report is not empty")
	}
	sum := rep.Summary("builds/b1")
	if !sum.OK || len(sum.Steps) != 9 || sum.Steps[4].Step != "apidiff" || !sum.Steps[4].Skipped {
		t.Errorf("summary = %+v", sum)
	}
}

func mustResult(t *testing.T, rep *Report, s StepID) Result {
	t.Helper()
	r, ok := rep.Result(s)
	if !ok {
		t.Fatalf("step %s did not run; ran %v", s, stepIDs(rep))
	}
	return r
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestProtectedPathsRejected(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{
		"internal/launcher/doc.go": "package launcher // sneaky\n",
		"cmd/mantle/extra.go":      "package main\n",
		"cmd/mantle-ui/main.go":    "package main // allowed\n",
		".claude/settings.json":    "{}\n",
		"go.mod":                   "module example.com/m\n\ngo 1.27\n\ntoolchain go1.99.0\n",
		"mods/x/x.go":              "package x\n",
	})
	rep := f.run()
	if rep.OK {
		t.Fatal("pipeline passed with protected changes")
	}
	if got := stepIDs(rep); !slices.Equal(got, []StepID{StepProtected}) {
		t.Errorf("steps = %v; the pipeline must stop at step 1", got)
	}
	r := rep.Results[0]
	want := []string{
		"protected path changed: .claude/settings.json",
		"protected path changed: cmd/mantle/extra.go",
		"protected path changed: internal/launcher/doc.go",
		`go.mod: toolchain line changed from "go1.27.1" to "go1.99.0"`,
	}
	if !slices.Equal(r.Errors, want) {
		t.Errorf("errors =\n%s\nwant\n%s", strings.Join(r.Errors, "\n"), strings.Join(want, "\n"))
	}
	if f.calls() != "" {
		t.Errorf("go ran after a protected-path failure: %s", f.calls())
	}
	text := TrimForBuilder(rep)
	if !strings.Contains(text, "step protected failed") || !strings.Contains(text, "internal/launcher/doc.go") {
		t.Errorf("TrimForBuilder:\n%s", text)
	}
}

func TestGoModChangeWithoutToolchainChangeIsAllowed(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"go.mod": "module example.com/m\n\ngo 1.27\n\ntoolchain go1.27.1 // pinned\n\nrequire example.com/x v1.0.0\n"})
	rep := f.run(StepProtected)
	if !rep.OK {
		t.Fatalf("rejected:\n%s", TrimForBuilder(rep))
	}
}

func TestLintGroupCollectsAllFailures(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"internal/app/app.go": "package app\nfunc  x() {}\n", "mods/a/a.go": "package a\n"})
	f.stub("gofmt.out", "internal/app/app.go\n")
	f.stub("vet.out", "# example.com/m/internal/app\n"+f.repo.Dir+"/internal/app/app.go:2:7: x declared and not used\nnoise line\n")
	f.stub("vet.exit", "1")
	f.stub("arch.violations", "mods/a-imports-internal/app")
	rep := f.run()
	if got := stepIDs(rep); !slices.Equal(got, []StepID{StepProtected, StepGofmt, StepVet, StepArch, StepAPIDiff}) {
		t.Errorf("steps = %v; want the whole lint group and no build", got)
	}
	if rep.OK {
		t.Fatal("pipeline passed")
	}
	failed := rep.Failed()
	if len(failed) != 3 {
		t.Errorf("failed = %+v", failed)
	}
	vet := mustResult(t, rep, StepVet)
	if !slices.Equal(vet.Errors, []string{"# example.com/m/internal/app", "internal/app/app.go:2:7: x declared and not used"}) {
		t.Errorf("vet errors = %q", vet.Errors)
	}
	text := TrimForBuilder(rep)
	for _, want := range []string{"step gofmt failed: 1 file(s) need gofmt -w", "needs gofmt: internal/app/app.go", "step vet failed", "step archtest failed: 1 import rule violation(s)", "full log: "} {
		if !strings.Contains(text, want) {
			t.Errorf("TrimForBuilder missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, f.repo.Dir+"/internal") {
		t.Errorf("absolute worktree paths in builder text:\n%s", text)
	}
}

func TestBuildFailureStopsBeforeTests(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"mods/a/a.go": "package a\n"})
	f.stub("build.out", "mods/a/a.go:3:1: syntax error: unexpected }\n")
	f.stub("build.exit", "1")
	rep := f.run()
	if got := stepIDs(rep); got[len(got)-1] != StepBuild {
		t.Errorf("steps = %v; want to stop at build", got)
	}
	if r := mustResult(t, rep, StepBuild); !slices.Equal(r.Errors, []string{"mods/a/a.go:3:1: syntax error: unexpected }"}) {
		t.Errorf("errors = %q", r.Errors)
	}
	if rep.Binary != "" {
		t.Error("binary recorded after a failed build")
	}
}

func TestTestFailure(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"mods/a/a.go": "package a\n"})
	f.stub("test.out", "=== RUN   TestX\n--- FAIL: TestX (0.00s)\n    a_test.go:9: got 1, want 2\nFAIL\nFAIL\texample.com/m/mods/a\t0.1s\nok  \texample.com/m/pkg/b\t0.1s\n")
	f.stub("test.exit", "1")
	rep := f.run()
	r := mustResult(t, rep, StepTest)
	if r.OK || !slices.Equal(r.Errors, []string{"--- FAIL: TestX (0.00s)", "    a_test.go:9: got 1, want 2"}) {
		t.Errorf("test result = %+v", r)
	}
	if _, ran := rep.Result(StepSelftest); ran {
		t.Error("selftest ran after failing tests")
	}
}

func TestSelftestFailure(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"mods/a/a.go": "package a\n"})
	f.stub("selftest.fail", "")
	rep := f.run()
	r := mustResult(t, rep, StepSelftest)
	if r.OK || len(r.Errors) == 0 || !strings.Contains(r.Errors[0], "line exceeds width 60") {
		t.Errorf("selftest = %+v", r)
	}
}

func TestSmokeFailures(t *testing.T) {
	t.Run("crash", func(t *testing.T) {
		f := newPipeFixture(t)
		f.change(map[string]string{"mods/a/a.go": "package a\n"})
		f.stub("ui.crash", "")
		rep := f.run()
		r := mustResult(t, rep, StepSmoke)
		if r.OK || !strings.Contains(r.Summary, "exited (exit status 2) before") {
			t.Errorf("smoke = %+v", r)
		}
		if !slices.ContainsFunc(r.Errors, func(s string) bool { return strings.Contains(s, "panic: boom") }) {
			t.Errorf("screen tail missing from errors: %q", r.Errors)
		}
	})
	t.Run("missing reply", func(t *testing.T) {
		f := newPipeFixture(t)
		f.change(map[string]string{"mods/a/a.go": "package a\n"})
		f.cfg.Smoke.Script[2] = Expect("never printed")
		f.cfg.Timeouts[StepSmoke] = 1 * time.Second
		rep := f.run()
		r := mustResult(t, rep, StepSmoke)
		if r.OK || !r.TimedOut {
			t.Errorf("smoke = %+v", r)
		}
	})
	t.Run("exit code", func(t *testing.T) {
		f := newPipeFixture(t)
		f.change(map[string]string{"mods/a/a.go": "package a\n"})
		f.cfg.Smoke.Env = []string{"SMOKE_EXIT=3"}
		rep := f.run()
		r := mustResult(t, rep, StepSmoke)
		if r.OK || !strings.Contains(r.Summary, "want exit code 0") {
			t.Errorf("smoke = %+v", r)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		f := newPipeFixture(t)
		f.change(map[string]string{"mods/a/a.go": "package a\n"})
		f.cfg.Smoke.Disabled = true
		rep := f.run()
		if r := mustResult(t, rep, StepSmoke); !r.Skipped || !rep.OK {
			t.Errorf("smoke = %+v", r)
		}
	})
}

func TestStepTimeoutKillsProcessGroup(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"mods/a/a.go": "package a\n"})
	f.stub("vet.sleep", "30")
	f.cfg.Timeouts[StepVet] = 2 * time.Second // room for the stub shell to start on a loaded machine
	start := time.Now()
	rep := f.run(StepVet)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("timeout took %s", d)
	}
	r := mustResult(t, rep, StepVet)
	if r.OK || !r.TimedOut || r.Summary != "timed out after 2s" {
		t.Errorf("vet = %+v", r)
	}
	// The stub runs in its own process group (pid = pgid); its sleep child
	// must be gone too.
	data, err := os.ReadFile(filepath.Join(f.stubDir, "vet.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pgid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(-pgid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(-pgid, syscall.SIGKILL)
			t.Fatalf("process group %d survived the timeout", pgid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStepsSubset(t *testing.T) {
	f := newPipeFixture(t)
	f.change(map[string]string{"internal/launcher/doc.go": "package launcher // would fail step 1\n"})
	rep := f.run(PostRebaseSteps...)
	if !rep.OK || !slices.Equal(stepIDs(rep), PostRebaseSteps) {
		t.Errorf("steps = %v ok=%v\n%s", stepIDs(rep), rep.OK, TrimForBuilder(rep))
	}
}

func TestSkipsWithoutChanges(t *testing.T) {
	f := newPipeFixture(t)
	rep := f.run(StepProtected, StepGofmt, StepAPIDiff)
	if !rep.OK || !mustResult(t, rep, StepGofmt).Skipped || !mustResult(t, rep, StepAPIDiff).Skipped {
		t.Errorf("report = %+v", rep.Results)
	}
}

func TestRunValidation(t *testing.T) {
	if _, err := NewPipeline(Config{}).Run(context.Background(), Run{}); err == nil {
		t.Error("empty run accepted")
	}
}

func TestIsProtected(t *testing.T) {
	for path, want := range map[string]bool{
		"cmd/mantle/main.go":            true,
		"cmd/mantle":                    true,
		"cmd/mantle-ui/main.go":         false,
		"internal/launcher/x/y.go":      true,
		"internal/launcherx/y.go":       false,
		"internal/selfmod/protected.go": true,
		".mcp.json":                     true,
		"sub/.mcp.json":                 false,
		".claude/agents/x.md":           true,
		"mods/demo/demo.go":             false,
		"features/selfmod/selfmod.go":   false,
		"internal/selfmodx/whatever.go": false,
	} {
		if got := IsProtected(path, ProtectedPaths); got != want {
			t.Errorf("IsProtected(%q) = %v", path, got)
		}
	}
}

func TestToolchainLine(t *testing.T) {
	for in, want := range map[string]string{
		"module m\n\ngo 1.27\n\ntoolchain go1.27.1\n":  "go1.27.1",
		"module m\ntoolchain\tgo1.28 // pinned\n":      "go1.28",
		"module m\n\ngo 1.27\n":                        "",
		"module m\n// toolchain go1.1\ntoolchainx y\n": "",
	} {
		if got := ToolchainLine([]byte(in)); got != want {
			t.Errorf("ToolchainLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	clean, rest := stripANSI([]byte("a\x1b[1;31mb\x1b[2Cc\x1b]0;t\x07d\x1bPdcs\x1b\\e\x1b(Bf\r\ng\x07"))
	if clean != "ab cdef \ng" || rest != nil {
		t.Errorf("clean=%q rest=%q", clean, rest)
	}
	for _, partial := range []string{"x\x1b", "x\x1b[3", "x\x1b]0;ti", "x\x1b(", "x\x1bPd\x1b"} {
		if clean, rest := stripANSI([]byte(partial)); clean != "x" || string(rest) != partial[1:] {
			t.Errorf("stripANSI(%q) = %q, %q", partial, clean, rest)
		}
	}
	// A sequence split across writes is completed by the next write.
	var s screenBuffer
	s.write([]byte("hel\x1b[3"))
	s.write([]byte("8;5;1mlo"))
	if !s.consume(regexp.MustCompile(`hello`)) {
		t.Errorf("text = %q", s.text.String())
	}
	if s.consume(regexp.MustCompile(`hel`)) {
		t.Error("consume should move past the previous match")
	}
}

func TestExtractErrors(t *testing.T) {
	out := "go: downloading x\n# pkg/a\npkg/a/a.go:3:2: undefined: y\n--- FAIL: TestZ (0.01s)\n    z_test.go:5: boom\npanic: oh no [recovered]\nFAIL\tpkg/a\t0.2s\n"
	got := extractErrors(out)
	want := []string{"# pkg/a", "pkg/a/a.go:3:2: undefined: y", "--- FAIL: TestZ (0.01s)", "    z_test.go:5: boom", "panic: oh no [recovered]"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if got := extractErrors("one\ntwo\n"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("fallback = %q", got)
	}
	lines, more := capLines([]string{"a", "b", "c"}, 2)
	if len(lines) != 2 || more != 1 {
		t.Errorf("capLines = %v %d", lines, more)
	}
}
