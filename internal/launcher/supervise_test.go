package launcher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fakeTerm struct{ saves, restores int }

func (f *fakeTerm) Save()    { f.saves++ }
func (f *fakeTerm) Restore() { f.restores++ }

type harness struct {
	t      *testing.T
	l      Layout
	store  Store
	dir    string
	term   *fakeTerm
	stderr *bytes.Buffer
	sigs   chan os.Signal
	sup    *Supervisor
	exe    string
	n      int
}

func newHarness(t *testing.T, modes ...string) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:      t,
		l:      Layout{Root: filepath.Join(root, "home")},
		dir:    filepath.Join(root, "fake"),
		term:   &fakeTerm{},
		stderr: &bytes.Buffer{},
		sigs:   make(chan os.Signal, 4),
	}
	h.store = Store{L: h.l}
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.setModes(modes...)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h.exe = exe
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "MANTLE_") {
			env = append(env, kv)
		}
	}
	h.sup = &Supervisor{
		Layout:    h.l,
		Stderr:    h.stderr,
		Env:       append(env, envFakeUI+"="+h.dir),
		Terminal:  h.term,
		Signals:   h.sigs,
		KillGrace: 500 * time.Millisecond,
	}
	return h
}

func (h *harness) setModes(modes ...string) {
	if err := os.WriteFile(filepath.Join(h.dir, "modes"), []byte(strings.Join(modes, "\n")+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// install adds a version built from the test binary; later installs are newer.
func (h *harness) install(id string, healthy bool) {
	h.t.Helper()
	h.n++
	created := time.Date(2026, 10, 1, 0, 0, h.n, 0, time.UTC)
	if _, err := h.store.Install(id, h.exe, Manifest{Created: created, Source: "test"}); err != nil {
		h.t.Fatal(err)
	}
	if healthy {
		if err := MarkHealthy(h.l, id); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *harness) current(id string)  { h.must(h.store.SetCurrent(id)) }
func (h *harness) lastGood(id string) { h.must(h.store.SetLastGood(id)) }
func (h *harness) must(err error) {
	h.t.Helper()
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) run(args ...string) int {
	return h.sup.Run(LaunchOptions{Args: args})
}

func TestCleanExitPassesProbation(t *testing.T) {
	h := newHarness(t, "exit:0")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v2")
	h.lastGood("v1")

	if code := h.run("hello"); code != 0 {
		t.Fatalf("exit code %d, stderr:\n%s", code, h.stderr)
	}
	if !IsHealthy(h.l, "v2") {
		t.Error("v2 not marked healthy after a clean exit")
	}
	if got := h.store.LastGoodID(); got != "v2" {
		t.Errorf("last-good = %q, want v2", got)
	}
	if h.term.saves != 1 || h.term.restores != 0 {
		t.Errorf("terminal saves=%d restores=%d, want 1/0", h.term.saves, h.term.restores)
	}
	ls := readLaunches(t, h.dir)
	if len(ls) != 1 {
		t.Fatalf("%d launches, want 1", len(ls))
	}
	r := ls[0]
	if !slices.Equal(r.Args, []string{"hello"}) {
		t.Errorf("args = %q", r.Args)
	}
	if r.Env[EnvBuildID] != "v2" || r.Env[EnvProbation] != "1" || r.Env[EnvHome] != h.l.Root {
		t.Errorf("env = %v", r.Env)
	}
	if r.Env[EnvLauncherPID] != strconv.Itoa(os.Getpid()) {
		t.Errorf("launcher pid = %q", r.Env[EnvLauncherPID])
	}
	if _, ok := r.Env[EnvSafe]; ok {
		t.Error("MANTLE_SAFE set without --safe")
	}
	if h.stderr.Len() != 0 {
		t.Errorf("unexpected notice: %s", h.stderr)
	}
}

func TestHealthyBuildHasNoProbationEnv(t *testing.T) {
	h := newHarness(t, "exit:0")
	h.install("v1", true)
	h.current("v1")
	h.run()
	if r := readLaunches(t, h.dir)[0]; r.Env[EnvProbation] != "" {
		t.Errorf("MANTLE_PROBATION=%q for a healthy build", r.Env[EnvProbation])
	}
}

func TestMarkerWrittenDuringRunPassesProbationEvenIfLaterCrash(t *testing.T) {
	h := newHarness(t, "healthy:3")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v2")
	h.lastGood("v1")
	if code := h.run(); code != 3 {
		t.Fatalf("exit code %d", code)
	}
	if h.store.CurrentID() != "v2" || h.store.LastGoodID() != "v2" {
		t.Errorf("current=%s last-good=%s, want v2/v2", h.store.CurrentID(), h.store.LastGoodID())
	}
	if st := LoadProbation(h.l, "v2"); !st.Healthy || st.Failures != 0 {
		t.Errorf("state = %+v", st)
	}
	if h.term.restores != 1 {
		t.Errorf("restores = %d, want 1", h.term.restores)
	}
}

func TestCrashOnProbationThenRollback(t *testing.T) {
	h := newHarness(t, "panic", "panic", "exit:0")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v2")
	h.lastGood("v1")

	// First failed launch: notice, exit, still current.
	if code := h.run("first prompt"); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
	out := h.stderr.String()
	for _, want := range []string{"exited unexpectedly (exit status 2, build v2)", "panic: boom red", "goroutine 1 [running]:", "failed launch 1 of 2", "log: "} {
		if !strings.Contains(out, want) {
			t.Errorf("notice missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("notice contains an escape character: %q", out)
	}
	if strings.Contains(out, "some earlier noise") {
		t.Errorf("excerpt should start at the panic line:\n%s", out)
	}
	if h.term.restores != 1 {
		t.Errorf("restores = %d, want 1", h.term.restores)
	}
	if st := LoadProbation(h.l, "v2"); st.Failures != 1 || st.Healthy {
		t.Errorf("state after first crash = %+v", st)
	}
	if h.store.CurrentID() != "v2" {
		t.Fatalf("current = %s after one failure", h.store.CurrentID())
	}
	if runs, _ := h.l.RunFiles(); len(runs) != 0 {
		t.Errorf("run files left behind: %+v", runs)
	}
	logs, _ := filepath.Glob(filepath.Join(h.l.Logs(), "*.log"))
	if len(logs) != 1 {
		t.Errorf("logs = %v, want the crash log kept", logs)
	}

	// Second failed launch: roll back to v1 and resume the session.
	h.stderr.Reset()
	if code := h.run(); code != 0 {
		t.Fatalf("exit code %d after rollback, stderr:\n%s", code, h.stderr)
	}
	if h.store.CurrentID() != "v1" {
		t.Errorf("current = %s, want v1", h.store.CurrentID())
	}
	if !strings.Contains(h.stderr.String(), "rolled back to v1") {
		t.Errorf("missing rollback notice:\n%s", h.stderr)
	}
	ls := readLaunches(t, h.dir)
	if len(ls) != 3 {
		t.Fatalf("%d launches, want 3", len(ls))
	}
	if ls[2].Env[EnvBuildID] != "v1" {
		t.Errorf("relaunch ran %s, want v1", ls[2].Env[EnvBuildID])
	}
	if !slices.Equal(ls[2].Args, []string{"--resume", "sess-crash"}) {
		t.Errorf("relaunch args = %q, want --resume sess-crash", ls[2].Args)
	}
}

func TestCrashWithoutRollbackTarget(t *testing.T) {
	h := newHarness(t, "exit:1")
	h.install("v1", false)
	h.current("v1")
	h.lastGood("v1")
	h.run()
	h.stderr.Reset()
	if code := h.run(); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(h.stderr.String(), "no other good build") {
		t.Errorf("notice:\n%s", h.stderr)
	}
	if len(readLaunches(t, h.dir)) != 2 {
		t.Error("expected no relaunch without a rollback target")
	}
}

func TestHealthyBuildCrashDoesNotRollBack(t *testing.T) {
	h := newHarness(t, "signal:KILL")
	h.install("v1", true)
	h.install("v2", true)
	h.current("v2")
	h.lastGood("v1")
	for range 3 {
		if code := h.run(); code != 128+int(syscall.SIGKILL) {
			t.Fatalf("exit code %d", code)
		}
	}
	if h.store.CurrentID() != "v2" {
		t.Errorf("healthy build rolled back")
	}
	if !strings.Contains(h.stderr.String(), "killed by killed") {
		t.Errorf("notice:\n%s", h.stderr)
	}
	if strings.Contains(h.stderr.String(), "probation") {
		t.Errorf("probation notice for a healthy build:\n%s", h.stderr)
	}
}

func TestRestartRelaunchesWithHandoffArgs(t *testing.T) {
	h := newHarness(t, "runfile:75", "exit:0")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v1")
	// Simulate a promotion while v1 runs: the relaunch must pick up v2.
	h.sup.OnStart = func(int) {
		if h.store.CurrentID() == "v1" {
			h.current("v2")
		}
	}
	if code := h.run("initial prompt"); code != 0 {
		t.Fatalf("exit code %d, stderr:\n%s", code, h.stderr)
	}
	ls := readLaunches(t, h.dir)
	if len(ls) != 2 {
		t.Fatalf("%d launches, want 2", len(ls))
	}
	if ls[1].Env[EnvBuildID] != "v2" {
		t.Errorf("relaunch ran %s, want v2", ls[1].Env[EnvBuildID])
	}
	if want := []string{"--model", "opus", "--resume", "sess-0"}; !slices.Equal(ls[1].Args, want) {
		t.Errorf("relaunch args = %q, want %q", ls[1].Args, want)
	}
	if ls[1].Cwd != resolve(t, h.dir) {
		t.Errorf("relaunch cwd = %s, want %s", ls[1].Cwd, h.dir)
	}
	if h.term.restores != 0 {
		t.Errorf("terminal restored on a restart")
	}
	if !IsHealthy(h.l, "v2") || h.store.LastGoodID() != "v2" {
		t.Error("v2 should pass probation with its clean exit")
	}
}

func resolve(t *testing.T, p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRestartWithoutRunFileDropsArgs(t *testing.T) {
	h := newHarness(t, "exit:75", "exit:0")
	h.install("v1", true)
	h.current("v1")
	h.run("do not resend this prompt")
	ls := readLaunches(t, h.dir)
	if len(ls) != 2 || len(ls[1].Args) != 0 {
		t.Fatalf("launches = %+v; the relaunch must not reuse the original argv", ls)
	}
}

func TestQuickRestartLoopGivesUp(t *testing.T) {
	h := newHarness(t, "exit:75")
	h.install("v1", true)
	h.current("v1")
	h.sup.MaxQuickRestarts = 3
	if code := h.run(); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if n := len(readLaunches(t, h.dir)); n != 4 {
		t.Errorf("%d launches, want 4", n)
	}
	if !strings.Contains(h.stderr.String(), "giving up") {
		t.Errorf("notice:\n%s", h.stderr)
	}
}

func TestUsageErrorIsNotACrash(t *testing.T) {
	h := newHarness(t, "usage")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v2")
	h.lastGood("v1")
	for range 3 {
		h.stderr.Reset()
		if code := h.run("--bogus"); code != ExitUsage {
			t.Fatalf("exit code %d", code)
		}
	}
	if got := h.stderr.String(); got != "mantle-ui: unknown option --bogus\n" {
		t.Errorf("stderr = %q", got)
	}
	if st := LoadProbation(h.l, "v2"); st.Failures != 0 {
		t.Errorf("usage errors counted as failures: %+v", st)
	}
	if h.store.CurrentID() != "v2" || h.term.restores != 0 {
		t.Error("usage error triggered rollback or terminal restore")
	}
}

func TestSignalsForwardedAndSIGINTSwallowed(t *testing.T) {
	h := newHarness(t, "wait")
	h.install("v1", false)
	h.current("v1")
	ready := filepath.Join(h.dir, "ready")
	h.sup.OnStart = func(int) {
		go func() {
			waitFor(t, ready)
			h.sigs <- syscall.SIGINT
			time.Sleep(200 * time.Millisecond)
			h.sigs <- syscall.SIGTERM
		}()
	}
	if code := h.run(); code != 143 {
		t.Fatalf("exit code %d, stderr:\n%s", code, h.stderr)
	}
	sigs, _ := os.ReadFile(filepath.Join(h.dir, "signals"))
	if got := strings.TrimSpace(string(sigs)); got != "terminated" {
		t.Errorf("child saw signals %q, want only SIGTERM", got)
	}
	if st := LoadProbation(h.l, "v1"); st.Failures != 0 {
		t.Errorf("forwarded SIGTERM counted as a failure: %+v", st)
	}
	if h.term.restores != 1 {
		t.Errorf("restores = %d, want 1", h.term.restores)
	}
}

func waitFor(t *testing.T, path string) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %s", path)
}

func TestCrashKillsEngineGroup(t *testing.T) {
	for _, mode := range []string{"engine:3", "enginerecord:3"} {
		t.Run(mode, func(t *testing.T) { testCrashKillsEngineGroup(t, mode) })
	}
}

func testCrashKillsEngineGroup(t *testing.T, mode string) {
	h := newHarness(t, mode)
	h.install("v1", true)
	h.current("v1")
	// An engine record of another live UI must survive.
	other := RunFile{PID: os.Getpid(), PGID: os.Getpid(), UIPID: 1, EngineID: "main"}
	h.must(WriteRunFile(h.l.RunFile(os.Getpid()), other))
	if code := h.run(); code != 3 {
		t.Fatalf("exit code %d, stderr:\n%s", code, h.stderr)
	}
	runs, _ := h.l.RunFiles()
	if len(runs) != 1 || runs[0].PID != os.Getpid() {
		t.Errorf("run files after reap = %+v; want only the other UI's engine record", runs)
	}
	data, err := os.ReadFile(filepath.Join(h.dir, "engine.pgid"))
	if err != nil {
		t.Fatal(err)
	}
	pg, _ := strconv.Atoi(string(data))
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(-pg, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			syscall.Kill(-pg, syscall.SIGKILL)
			t.Fatalf("engine group %d still alive (kill: %v)", pg, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSafeModeRunsLastGood(t *testing.T) {
	h := newHarness(t, "exit:4")
	h.install("v1", true)
	h.install("v2", false)
	h.current("v2")
	h.lastGood("v1")
	if code := h.sup.Run(LaunchOptions{Safe: true, Args: []string{"x"}}); code != 4 {
		t.Fatalf("exit code %d", code)
	}
	r := readLaunches(t, h.dir)[0]
	if r.Env[EnvBuildID] != "v1" || r.Env[EnvSafe] != "1" || r.Env[EnvProbation] != "" {
		t.Errorf("env = %v", r.Env)
	}
	if st := LoadProbation(h.l, "v2"); st.Failures != 0 {
		t.Error("safe mode touched probation of current")
	}
}

func TestUIBinOverride(t *testing.T) {
	h := newHarness(t, "exit:0")
	if code := h.sup.Run(LaunchOptions{UIBin: h.exe}); code != 0 {
		t.Fatalf("exit code %d, stderr:\n%s", code, h.stderr)
	}
	if r := readLaunches(t, h.dir)[0]; r.Env[EnvBuildID] != "dev" {
		t.Errorf("build id = %q", r.Env[EnvBuildID])
	}
}

func TestNotInstalled(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(h.stderr.String(), "make install") {
		t.Errorf("notice:\n%s", h.stderr)
	}
}

func TestStartFailureCountsAsCrash(t *testing.T) {
	h := newHarness(t)
	bad := filepath.Join(t.TempDir(), "not-a-binary")
	os.WriteFile(bad, []byte("garbage\x00\x01"), 0o755)
	if _, err := h.store.Install("v1", bad, Manifest{}); err != nil {
		t.Fatal(err)
	}
	h.current("v1")
	if code := h.run(); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if st := LoadProbation(h.l, "v1"); st.Failures != 1 {
		t.Errorf("state = %+v", st)
	}
	if !strings.Contains(h.stderr.String(), "failed to start") {
		t.Errorf("notice:\n%s", h.stderr)
	}
}

func TestLogExcerptTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	var b strings.Builder
	for i := range 30 {
		b.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	b.WriteString("\n\n")
	os.WriteFile(p, []byte(b.String()), 0o644)
	got := logExcerpt(p)
	if len(got) != 15 || got[0] != "line 15" || got[14] != "line 29" {
		t.Errorf("excerpt = %q", got)
	}
	if logExcerpt(filepath.Join(t.TempDir(), "missing")) != nil {
		t.Error("excerpt of a missing file")
	}
}

func TestPruneLogs(t *testing.T) {
	h := newHarness(t)
	h.must(h.l.EnsureDirs())
	for i := range 5 {
		p := filepath.Join(h.l.Logs(), strconv.Itoa(i)+".log")
		os.WriteFile(p, []byte("x"), 0o644)
		mt := time.Now().Add(time.Duration(i) * time.Minute)
		os.Chtimes(p, mt, mt)
	}
	h.sup.KeepLogs = 2
	h.sup.pruneLogs()
	left, _ := filepath.Glob(filepath.Join(h.l.Logs(), "*.log"))
	slices.Sort(left)
	if len(left) != 2 || filepath.Base(left[0]) != "3.log" || filepath.Base(left[1]) != "4.log" {
		t.Errorf("left = %v", left)
	}
}

func TestSanitizeLine(t *testing.T) {
	cases := map[string]string{
		"plain":                         "plain",
		"a\x1b[31mred\x1b[0m b":         "ared b",
		"t\x1b]9;4;3\x07x":              "tx",
		"t\x1b]0;title\x1b\\x":          "tx",
		"tab\there":                     "tab    here",
		"bell\x07 nul\x00 esc\x1bc end": "bell nul esc end",
	}
	for in, want := range cases {
		if got := sanitizeLine(in); got != want {
			t.Errorf("sanitizeLine(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeLine(strings.Repeat("x", 1000)); len(got) > 310 {
		t.Errorf("long line not truncated: %d", len(got))
	}
}

func TestKillableGroup(t *testing.T) {
	for _, pg := range []int{-5, 0, 1, syscall.Getpgrp(), os.Getpid()} {
		if killableGroup(pg) {
			t.Errorf("killableGroup(%d) = true", pg)
		}
	}
}
