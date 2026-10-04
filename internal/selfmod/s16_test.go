package selfmod

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/KaitouKid1412/mantle/internal/archtest"
)

// TestSpikeS16LauncherJobControl is spike S16 for the launcher: an
// interactive shell in a pty runs the real mantle launcher supervising a
// small Bubble Tea program (internal/selfmod/testdata/s16ui).
//
//   - ctrl+z suspends: launcher and UI share a process group, so both stop and
//     the shell reports the job stopped; fg resumes both.
//   - ctrl+c while the UI runs an editor-like child (tea.ExecProcess) does not
//     kill the launcher.
//   - a panic in raw mode is restored: the launcher prints its notice and the
//     shell's terminal echoes input again.
func TestSpikeS16LauncherJobControl(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and drives a shell")
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("no /bin/bash")
	}
	wd, _ := os.Getwd()
	root, err := archtest.ModuleRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for pkg, out := range map[string]string{"./cmd/mantle": "mantle", "./internal/selfmod/testdata/s16ui": "s16ui"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, out), pkg)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, b)
		}
	}
	sh := exec.Command("/bin/bash", "--norc", "--noprofile", "-i")
	sh.Dir = dir
	sh.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TERM=xterm-256color", "PS1=$ ",
		"MANTLE_HOME=" + filepath.Join(dir, "home"), "MANTLE_UI_BIN=" + filepath.Join(dir, "s16ui"),
		"BASH_SILENCE_DEPRECATION_WARNING=1",
	}
	f, err := pty.StartWithSize(sh, &pty.Winsize{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		syscall.Kill(-sh.Process.Pid, syscall.SIGKILL)
		f.Close()
		sh.Wait()
	}()
	out := &screenBuffer{}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				out.write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	expect := func(re string) []string {
		t.Helper()
		rx := regexp.MustCompile(re)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			out.mu.Lock()
			text := out.text.String()[out.pos:]
			loc := rx.FindStringSubmatchIndex(text)
			if loc != nil {
				var groups []string
				for i := 0; i+1 < len(loc); i += 2 {
					groups = append(groups, text[loc[i]:loc[i+1]])
				}
				out.pos += loc[1]
				out.mu.Unlock()
				return groups
			}
			out.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %q; screen tail:\n%s", re, out.tail(2000))
		return nil
	}
	send := func(s string) {
		t.Helper()
		if _, err := f.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	state := func(pid int) string {
		b, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		return strings.TrimSpace(string(b))
	}
	waitState := func(pid int, stopped bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if strings.HasPrefix(state(pid), "T") == stopped {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("pid %d state %q, want stopped=%v", pid, state(pid), stopped)
	}

	expect(`\$ `)
	send("./mantle\r")
	uiPID, _ := strconv.Atoi(expect(`s16 ready pid=(\d+) status=none`)[1])
	ppid, _ := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(uiPID)).Output()
	launcherPID, _ := strconv.Atoi(strings.TrimSpace(string(ppid)))
	if launcherPID <= 1 {
		t.Fatalf("launcher pid %d", launcherPID)
	}
	lpg, _ := syscall.Getpgid(launcherPID)
	upg, _ := syscall.Getpgid(uiPID)
	if lpg != upg {
		t.Fatalf("launcher pgid %d, ui pgid %d: they must share a process group", lpg, upg)
	}

	// ctrl+z: Bubble Tea suspends with kill(0, SIGTSTP); both stop.
	send("\x1a")
	expect(`(?i)stopped`)
	waitState(launcherPID, true)
	waitState(uiPID, true)
	// fg resumes both.
	send("fg\r")
	expect(`status=resumed`)
	waitState(launcherPID, false)
	waitState(uiPID, false)

	// ctrl+c inside an editor-like child: the launcher survives.
	send("e")
	expect(`editor-running`)
	send("\x03")
	time.Sleep(500 * time.Millisecond)
	if syscall.Kill(launcherPID, 0) != nil {
		t.Fatalf("the launcher died on ctrl+c inside the editor; screen:\n%s", out.tail(1500))
	}
	uiAlive := syscall.Kill(uiPID, 0) == nil
	t.Logf("after ctrl+c in the editor: UI alive=%v (state %q)", uiAlive, state(uiPID))
	if uiAlive {
		expect(`status=(editor-done|interrupt-msg)`)
		send("q")
	}
	expect(`\$ `) // don't type ahead: the UI would read it
	send("echo exit=$?\r")
	if uiAlive {
		expect(`exit=0`)
	} else {
		expect(`exit=\d+`)
	}

	// A panic in raw mode: the launcher restores the terminal and explains.
	send("./mantle\r")
	expect(`s16 ready pid=\d+ status=none`)
	send("p")
	expect(`mantle: mantle-ui exited unexpectedly`)
	expect(`\$ `)
	send("echo echoed-input\r")
	// With echo restored the typed command shows up before its output.
	expect(`echo echoed-input`)
	expect(`echoed-input`)
}
