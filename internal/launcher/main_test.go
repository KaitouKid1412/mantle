package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as the fake mantle-ui and as the mantle launcher:
// TestMain checks these variables before running any test.
const (
	envFakeUI   = "LAUNCHER_TEST_FAKE_UI"  // directory of the fake UI's script and records
	envFakeMain = "LAUNCHER_TEST_RUN_MAIN" // "1": run Main(os.Args[1:])
)

func TestMain(m *testing.M) {
	if dir := os.Getenv(envFakeUI); dir != "" {
		os.Exit(fakeUI(dir))
	}
	if os.Getenv(envFakeMain) == "1" {
		os.Exit(Main(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// launchRecord is one fake UI launch, appended to launches.jsonl.
type launchRecord struct {
	N    int               `json:"n"`
	PID  int               `json:"pid"`
	Args []string          `json:"args"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
	Mode string            `json:"mode"`
}

// fakeUI behaves according to line n of <dir>/modes for the n-th launch
// (the last line repeats). Modes:
//
//	exit:N           exit with code N
//	healthy:N        write the healthy marker, then exit N
//	panic            print a Go-style panic to stderr, exit 2
//	usage            print a usage error to stderr, exit 64
//	runfile:N        write a run file (session, handoff args, cwd), exit N
//	engine:N         start a process group, record it in the run file, exit N
//	wait             record SIGINT/SIGTERM in <dir>/signals; exit 143 on SIGTERM
//	signal:KILL      kill itself with SIGKILL
func fakeUI(dir string) int {
	launches, _ := os.ReadFile(filepath.Join(dir, "launches.jsonl"))
	n := strings.Count(string(launches), "\n")
	modesData, _ := os.ReadFile(filepath.Join(dir, "modes"))
	modes := strings.Fields(string(modesData))
	mode := "exit:0"
	if len(modes) > 0 {
		mode = modes[min(n, len(modes)-1)]
	}
	cwd, _ := os.Getwd()
	rec := launchRecord{N: n, PID: os.Getpid(), Args: os.Args[1:], Cwd: cwd, Mode: mode, Env: map[string]string{}}
	for _, k := range []string{EnvHome, EnvBuildID, EnvLauncherPID, EnvSafe, EnvProbation} {
		if v, ok := os.LookupEnv(k); ok {
			rec.Env[k] = v
		}
	}
	line, _ := json.Marshal(rec)
	f, _ := os.OpenFile(filepath.Join(dir, "launches.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	f.Write(append(line, '\n'))
	f.Close()

	l := Layout{Root: os.Getenv(EnvHome)}
	name, arg, _ := strings.Cut(mode, ":")
	code, _ := strconv.Atoi(arg)
	runFile := RunFile{
		PID:         os.Getpid(),
		Version:     os.Getenv(EnvBuildID),
		SessionID:   "sess-" + strconv.Itoa(n),
		Cwd:         dir,
		Argv:        os.Args[1:],
		HandoffArgs: []string{"--model", "opus", "--resume", "sess-" + strconv.Itoa(n)},
	}
	switch name {
	case "exit":
		return code
	case "healthy":
		MarkHealthy(l, os.Getenv(EnvBuildID))
		return code
	case "panic":
		WriteRunFile(l.RunFile(os.Getpid()), RunFile{PID: os.Getpid(), SessionID: "sess-crash"})
		fmt.Fprintln(os.Stderr, "some earlier noise")
		fmt.Fprintln(os.Stderr, "panic: boom \x1b[31mred\x1b[0m")
		fmt.Fprintln(os.Stderr, "\ngoroutine 1 [running]:\nmain.main()")
		return 2
	case "usage":
		fmt.Fprintln(os.Stderr, "mantle-ui: unknown option --bogus")
		return ExitUsage
	case "runfile":
		WriteRunFile(l.RunFile(os.Getpid()), runFile)
		return code
	case "engine":
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return 99
		}
		runFile.EnginePGIDs = map[string]int{"main": cmd.Process.Pid}
		WriteRunFile(l.RunFile(os.Getpid()), runFile)
		os.WriteFile(filepath.Join(dir, "engine.pgid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
		return code
	case "wait":
		ch := make(chan os.Signal, 4)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644)
		timeout := time.After(10 * time.Second)
		for {
			select {
			case sig := <-ch:
				sf, _ := os.OpenFile(filepath.Join(dir, "signals"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				fmt.Fprintln(sf, sig.(syscall.Signal).String())
				sf.Close()
				if sig == syscall.SIGTERM {
					return 143
				}
			case <-timeout:
				return 98
			}
		}
	case "signal":
		// SIGKILL: the Go runtime turns SIGSEGV and SIGABRT into exit 2.
		syscall.Kill(os.Getpid(), syscall.SIGKILL)
		time.Sleep(5 * time.Second)
		return 97
	}
	return 96
}

func readLaunches(t *testing.T, dir string) []launchRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "launches.jsonl"))
	if err != nil {
		return nil
	}
	var out []launchRecord
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r launchRecord
		if err := json.Unmarshal([]byte(ln), &r); err != nil {
			t.Fatalf("bad launch record %q: %v", ln, err)
		}
		out = append(out, r)
	}
	return out
}
