package launcher

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Supervisor runs mantle-ui as a child in the launcher's own process group,
// waits for it, and handles crashes, probation and restarts.
//
// The launcher and the UI share a process group because Bubble Tea's suspend
// (ctrl+z) sends SIGTSTP to the whole group: both stop, and the shell's fg
// resumes both. The launcher therefore never ignores or catches SIGTSTP.
type Supervisor struct {
	Layout Layout
	Stdin  *os.File
	Stdout *os.File
	// Stderr receives the launcher's notices.
	Stderr io.Writer
	// Env is the base environment for mantle-ui.
	Env      []string
	Terminal Terminal
	// Signals delivers signals sent to the launcher. If nil, Run subscribes to
	// SIGTERM, SIGHUP, SIGINT and SIGQUIT. SIGTERM and SIGHUP are forwarded;
	// SIGINT and SIGQUIT are swallowed (the UI reads ctrl+c and ctrl+\ as
	// keys, and the terminal already signals the whole group).
	Signals <-chan os.Signal
	// KillGrace is the time engine groups get between SIGTERM and SIGKILL.
	KillGrace time.Duration
	// MaxQuickRestarts bounds relaunch loops of builds that exit 75 within
	// QuickRestartWindow of starting.
	MaxQuickRestarts   int
	QuickRestartWindow time.Duration
	// KeepLogs is how many logs/<pid>.log files to keep.
	KeepLogs int
	// OnStart, if set, is called with the pid of every child started.
	OnStart func(pid int)
}

// LaunchOptions selects what Run starts.
type LaunchOptions struct {
	// Args are mantle-ui's arguments (argv without argv[0]).
	Args []string
	// Safe runs last-good with MANTLE_SAFE=1.
	Safe bool
	// UIBin runs this binary instead of the version store (no probation).
	UIBin string
	// Dir is the working directory ("" inherits the launcher's).
	Dir string
}

// NewSupervisor returns a Supervisor on the process's stdio and environment.
func NewSupervisor(l Layout) *Supervisor {
	return &Supervisor{
		Layout:   l,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Env:      os.Environ(),
		Terminal: NewTerminal(os.Stdin, os.Stdout),
	}
}

func (s *Supervisor) defaults() {
	if s.Stderr == nil {
		s.Stderr = io.Discard
	}
	if s.Terminal == nil {
		s.Terminal = NewTerminal(s.Stdin, s.Stdout)
	}
	if s.KillGrace == 0 {
		s.KillGrace = 2 * time.Second
	}
	if s.MaxQuickRestarts == 0 {
		s.MaxQuickRestarts = 5
	}
	if s.QuickRestartWindow == 0 {
		s.QuickRestartWindow = 2 * time.Second
	}
	if s.KeepLogs == 0 {
		s.KeepLogs = 50
	}
}

func (s *Supervisor) noticef(format string, args ...any) {
	fmt.Fprintf(s.Stderr, "mantle: "+format+"\n", args...)
}

// target is one resolved launch.
type target struct {
	bin       string
	buildID   string
	probation bool // apply the probation state machine
	prev      ProbationState
	safe      bool
}

// runResult is the record of one child run.
type runResult struct {
	pid        int
	status     ExitStatus
	forwarded  bool
	lastSignal os.Signal
	logPath    string
	runFile    RunFile
	hasRunFile bool
	uptime     time.Duration
}

// Run starts mantle-ui and supervises it until the launcher should exit. It
// returns the launcher's exit code.
func (s *Supervisor) Run(opts LaunchOptions) int {
	s.defaults()
	sigs := s.Signals
	if sigs == nil {
		ch := make(chan os.Signal, 8)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
		defer signal.Stop(ch)
		sigs = ch
	}
	l := s.Layout
	if err := l.EnsureDirs(); err != nil {
		s.noticef("cannot create %s: %v", l.Root, err)
		return 1
	}
	l.RemoveStaleRunFiles(nil)
	s.pruneLogs()
	s.Terminal.Save()

	store := Store{L: l}
	args, dir := opts.Args, opts.Dir
	quick := 0
	for {
		t, err := s.pickTarget(store, opts)
		if err != nil {
			s.noticef("%v", err)
			return 1
		}
		res := s.runOnce(t, args, dir, sigs)
		s.reapEngines(res.runFile.EnginePGIDs)
		out := Classify(res.status, res.forwarded)

		d := Decision{Abnormal: out == OutcomeCrash || out == OutcomeInterrupted}
		if out == OutcomeRestart {
			d.Action = ActionRelaunch
		}
		var lastGood Version
		if t.probation {
			lg, lgErr := store.LastGood()
			canRollback := lgErr == nil && lg.ID != t.buildID && isExecutable(lg.Binary())
			lastGood = lg
			d = Decide(t.prev, IsHealthy(l, t.buildID), out, canRollback)
			if err := SaveProbation(l, t.buildID, d.State); err != nil {
				s.noticef("cannot record probation state: %v", err)
			}
			if d.PassedProbation {
				if err := store.SetLastGood(t.buildID); err != nil {
					s.noticef("cannot update last-good: %v", err)
				}
			}
		}

		if d.Abnormal {
			s.Terminal.Restore()
			s.abnormalNotice(t, res, out, d)
		}
		if out == OutcomeUsage {
			s.copyLog(res.logPath)
		}
		s.cleanup(res, d.Abnormal)

		switch d.Action {
		case ActionExit:
			return res.status.LauncherCode()
		case ActionRelaunch:
			if res.uptime < s.QuickRestartWindow {
				quick++
			} else {
				quick = 0
			}
			if quick > s.MaxQuickRestarts {
				s.noticef("mantle-ui keeps asking for a restart right after starting; giving up (log: %s)", res.logPath)
				return 1
			}
		case ActionRollback:
			if err := store.SetCurrent(lastGood.ID); err != nil {
				s.noticef("rollback to %s failed: %v", lastGood.ID, err)
				return res.status.LauncherCode()
			}
			s.noticef("build %s failed %d launches on probation; rolled back to %s.", t.buildID, d.State.Failures, lastGood.ID)
		}
		args = res.runFile.RelaunchArgs()
		if res.runFile.Cwd != "" && isDir(res.runFile.Cwd) {
			dir = res.runFile.Cwd
		}
		if res.runFile.SessionID != "" {
			s.noticef("relaunching and resuming session %s", res.runFile.SessionID)
		}
	}
}

func (s *Supervisor) pickTarget(store Store, opts LaunchOptions) (target, error) {
	if opts.UIBin != "" {
		return target{bin: opts.UIBin, buildID: "dev", safe: opts.Safe}, nil
	}
	if opts.Safe {
		v, err := store.LastGood()
		if err != nil {
			v, err = store.Current()
		}
		if err != nil {
			return target{}, notInstalledError(s.Layout, err)
		}
		return target{bin: v.Binary(), buildID: v.ID, safe: true}, nil
	}
	v, err := store.Current()
	if err != nil {
		if errors.Is(err, ErrNotInstalled) {
			return target{}, notInstalledError(s.Layout, err)
		}
		return target{}, fmt.Errorf("the current build cannot be resolved (%v); try `mantle rollback` or `mantle --safe`", err)
	}
	return target{
		bin:       v.Binary(),
		buildID:   v.ID,
		probation: true,
		prev:      LoadProbation(s.Layout, v.ID),
	}, nil
}

func notInstalledError(l Layout, err error) error {
	return fmt.Errorf("no mantle-ui build is installed in %s (%v). Run `make install` in a mantle checkout", l.Root, err)
}

func (s *Supervisor) childEnv(t target) []string {
	set := map[string]string{
		EnvHome:        s.Layout.Root,
		EnvBuildID:     t.buildID,
		EnvLauncherPID: strconv.Itoa(os.Getpid()),
	}
	if t.safe {
		set[EnvSafe] = "1"
	}
	if t.probation && !t.prev.Healthy {
		set[EnvProbation] = "1"
	}
	drop := []string{EnvHome, EnvBuildID, EnvLauncherPID, EnvSafe, EnvProbation}
	env := make([]string, 0, len(s.Env)+len(set))
	for _, kv := range s.Env {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(drop, k) {
			env = append(env, kv)
		}
	}
	for _, k := range drop {
		if v, ok := set[k]; ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (s *Supervisor) runOnce(t target, args []string, dir string, sigs <-chan os.Signal) runResult {
	var res runResult
	logf, err := os.CreateTemp(s.Layout.Logs(), "launch-*.log")
	if err == nil {
		res.logPath = logf.Name()
	}
	cmd := exec.Command(t.bin, args...)
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	if logf != nil {
		cmd.Stderr = logf
	} else {
		cmd.Stderr = s.Stderr
	}
	cmd.Env = s.childEnv(t)
	cmd.Dir = dir
	// No SysProcAttr: the child stays in our process group (see Supervisor).
	started := time.Now()
	if err := cmd.Start(); err != nil {
		if logf != nil {
			fmt.Fprintf(logf, "mantle: cannot start %s: %v\n", t.bin, err)
			logf.Close()
		}
		res.status = ExitStatus{Code: -1, StartErr: err}
		return res
	}
	res.pid = cmd.Process.Pid
	if logf != nil {
		if p := s.Layout.Log(res.pid); os.Rename(res.logPath, p) == nil {
			res.logPath = p
		}
	}
	if s.OnStart != nil {
		s.OnStart(res.pid)
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
wait:
	for {
		select {
		case <-done:
			break wait
		case sig := <-sigs:
			switch sig {
			case syscall.SIGTERM, syscall.SIGHUP:
				cmd.Process.Signal(sig)
				res.forwarded = true
				res.lastSignal = sig
			}
		}
	}
	res.uptime = time.Since(started)
	if logf != nil {
		logf.Close()
	}
	res.status = exitStatusOf(cmd.ProcessState)
	if rf, err := ReadRunFile(s.Layout.RunFile(res.pid)); err == nil {
		res.runFile, res.hasRunFile = rf, true
	}
	return res
}

func exitStatusOf(ps *os.ProcessState) ExitStatus {
	if ps == nil {
		return ExitStatus{Code: -1}
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return ExitStatus{Code: -1, Signaled: true, Signal: ws.Signal()}
	}
	return ExitStatus{Code: ps.ExitCode()}
}

// reapEngines kills engine process groups left behind by mantle-ui: SIGTERM,
// then SIGKILL after KillGrace. A healthy mantle-ui has already ended its
// engines, so this is normally a no-op. macOS has no PDEATHSIG, so without it
// a crashed UI would leave its engines running.
func (s *Supervisor) reapEngines(pgids map[string]int) {
	var live []int
	for _, pg := range pgids {
		if killableGroup(pg) && syscall.Kill(-pg, 0) == nil && !slices.Contains(live, pg) {
			live = append(live, pg)
		}
	}
	if len(live) == 0 {
		return
	}
	for _, pg := range live {
		syscall.Kill(-pg, syscall.SIGTERM)
	}
	deadline := time.Now().Add(s.KillGrace)
	for time.Now().Before(deadline) {
		live = slices.DeleteFunc(live, func(pg int) bool { return syscall.Kill(-pg, 0) != nil })
		if len(live) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, pg := range live {
		syscall.Kill(-pg, syscall.SIGKILL)
	}
}

// killableGroup refuses groups the launcher must never signal: invalid ids,
// init, and the launcher's own group.
func killableGroup(pg int) bool {
	return pg > 1 && pg != syscall.Getpgrp() && pg != os.Getpid()
}

func (s *Supervisor) abnormalNotice(t target, res runResult, out Outcome, d Decision) {
	if out == OutcomeInterrupted {
		if res.lastSignal == syscall.SIGHUP || (res.status.Signaled && res.status.Signal == syscall.SIGHUP) {
			return // the terminal is gone
		}
		s.noticef("mantle-ui stopped (%s).", res.status)
		return
	}
	s.noticef("mantle-ui exited unexpectedly (%s, build %s).", res.status, t.buildID)
	for _, line := range logExcerpt(res.logPath) {
		fmt.Fprintf(s.Stderr, "  %s\n", line)
	}
	if res.logPath != "" {
		s.noticef("log: %s", res.logPath)
	}
	if !t.probation || d.State.Healthy {
		return
	}
	switch d.Action {
	case ActionRollback:
		// The rollback notice follows once current is flipped.
	default:
		if d.State.Failures >= MaxProbationFailures {
			s.noticef("build %s failed probation and there is no other good build to roll back to; see `mantle versions` and `mantle doctor`.", t.buildID)
		} else {
			s.noticef("build %s is on probation (failed launch %d of %d). Run mantle again to retry, or `mantle --safe` to start the last good build.",
				t.buildID, d.State.Failures, MaxProbationFailures)
		}
	}
}

func (s *Supervisor) copyLog(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	if len(data) > 8<<10 {
		data = data[:8<<10]
	}
	s.Stderr.Write(data)
	if data[len(data)-1] != '\n' {
		io.WriteString(s.Stderr, "\n")
	}
}

// cleanup removes the run file of a finished child and its log if empty.
func (s *Supervisor) cleanup(res runResult, abnormal bool) {
	if res.pid > 0 {
		os.Remove(s.Layout.RunFile(res.pid))
	}
	if res.logPath != "" && !abnormal {
		if fi, err := os.Stat(res.logPath); err == nil && fi.Size() == 0 {
			os.Remove(res.logPath)
		}
	}
}

// pruneLogs keeps the newest KeepLogs files in logs/.
func (s *Supervisor) pruneLogs() {
	ents, err := os.ReadDir(s.Layout.Logs())
	if err != nil {
		return
	}
	type logFile struct {
		path string
		mod  time.Time
	}
	var logs []logFile
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			logs = append(logs, logFile{filepath.Join(s.Layout.Logs(), e.Name()), fi.ModTime()})
		}
	}
	if len(logs) <= s.KeepLogs {
		return
	}
	slices.SortFunc(logs, func(a, b logFile) int { return b.mod.Compare(a.mod) })
	for _, lf := range logs[s.KeepLogs:] {
		os.Remove(lf.path)
	}
}

// logExcerpt returns the interesting part of a crash log: from the first
// panic or fatal error line, else the last lines. Control characters are
// stripped so the excerpt cannot disturb the terminal.
func logExcerpt(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, sanitizeLine(sc.Text()))
	}
	const maxLines = 20
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "panic:") || strings.HasPrefix(ln, "fatal error:") {
			start = i
			break
		}
	}
	if start >= 0 {
		return lines[start:min(len(lines), start+maxLines)]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines[max(0, len(lines)-15):]
}

// sanitizeLine removes escape sequences and control characters.
func sanitizeLine(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == '[': // CSI: parameters, then a final byte
			i += 2
			for i < len(rs) && (rs[i] < 0x40 || rs[i] > 0x7e) {
				i++
			}
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == ']': // OSC: until BEL or ST
			i += 2
			for i < len(rs) && rs[i] != 0x07 && !(rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '\\') {
				i++
			}
			if i < len(rs) && rs[i] == 0x1b {
				i++
			}
		case r == 0x1b:
			i++ // two-character escape
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
		if b.Len() > 300 {
			b.WriteString("…")
			break
		}
	}
	return b.String()
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
