package parity

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

// Workspace is the isolated environment one run gets: a working directory (a copy of
// the scenario's fixtures), a seeded CLAUDE_CONFIG_DIR, a HOME, and the fakeapi URL.
// Nothing points at the user's real ~/.claude.
type Workspace struct {
	Root      string
	WorkDir   string
	ConfigDir string
	HomeDir   string
	APIURL    string
	APIKey    string
}

// Target is a program the harness drives.
type Target interface {
	Name() string
	// Command builds the process for a scenario in ws. The runner starts it in a pty.
	Command(ws Workspace, sc *Scenario) (*exec.Cmd, error)
	// Ready reports whether the program waits for input at its prompt.
	Ready(f Frame) bool
	// Quit asks the program to exit (the runner kills it if it doesn't).
	Quit(t *Term)
}

// baseEnv is a neutral terminal environment: the user's PATH and locale, xterm with
// true colour, and nothing that identifies the user's terminal or Claude setup.
func baseEnv(ws Workspace) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + ws.HomeDir,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"TZ=UTC",
		"USER=parity",
		"SHELL=/bin/sh",
		"TMPDIR=" + os.TempDir(),
		"NO_UPDATE_NOTIFIER=1",
	}
	return fakeapi.Env(env, ws.APIURL, ws.ConfigDir, ws.APIKey)
}

// ClaudeTarget runs the interactive Claude Code TUI.
type ClaudeTarget struct {
	Bin string // "" = claude on PATH
}

func (ClaudeTarget) Name() string { return "claude" }

func (c ClaudeTarget) Command(ws Workspace, sc *Scenario) (*exec.Cmd, error) {
	bin := c.Bin
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("claude"); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command(bin, sc.Args...)
	cmd.Dir = ws.WorkDir
	cmd.Env = baseEnv(ws)
	return cmd, nil
}

// claudePrompt matches the input line of Claude Code's prompt box.
var claudePrompt = regexp.MustCompile(`^\s*[│|]?\s*[>❯]\s`)

func (ClaudeTarget) Ready(f Frame) bool {
	for _, l := range f.Screen {
		if claudePrompt.MatchString(l) || strings.TrimSpace(l) == ">" || strings.TrimSpace(l) == "❯" {
			return true
		}
	}
	return false
}

func (ClaudeTarget) Quit(t *Term) { ctrlCTwice(t) }

func ctrlCTwice(t *Term) {
	t.Input("\x03")
	time.Sleep(150 * time.Millisecond)
	t.Input("\x03")
	select {
	case <-t.Exited():
	case <-time.After(3 * time.Second):
	}
}

// CommandTarget runs any command; used by the harness's own tests.
type CommandTarget struct {
	TargetName string
	Argv       []string
	ReadyText  string
	Env        []string
}

func (c CommandTarget) Name() string { return c.TargetName }

func (c CommandTarget) Command(ws Workspace, sc *Scenario) (*exec.Cmd, error) {
	cmd := exec.Command(c.Argv[0], append(c.Argv[1:], sc.Args...)...)
	cmd.Dir = ws.WorkDir
	cmd.Env = append(baseEnv(ws), c.Env...)
	return cmd, nil
}

func (c CommandTarget) Ready(f Frame) bool {
	return strings.Contains(strings.Join(f.Screen, "\n"), c.ReadyText)
}

func (CommandTarget) Quit(t *Term) { ctrlCTwice(t) }
