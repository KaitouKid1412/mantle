package parity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// MantleTarget runs mantle-ui directly (not through the launcher) with the same
// isolated workspace as claude: its engine is the installed claude, talking to the
// same fakeapi with the same seeded CLAUDE_CONFIG_DIR. mantle's own state (~/.mantle)
// lands in the workspace's HOME.
type MantleTarget struct {
	Bin string // mantle-ui binary; build one with BuildMantleUI
}

func (MantleTarget) Name() string { return "mantle" }

func (m MantleTarget) Command(ws Workspace, sc *Scenario) (*exec.Cmd, error) {
	if m.Bin == "" {
		return nil, fmt.Errorf("parity: MantleTarget needs a mantle-ui binary (BuildMantleUI)")
	}
	cmd := exec.Command(m.Bin, sc.Args...)
	cmd.Dir = ws.WorkDir
	cmd.Env = append(baseEnv(ws), "MANTLE_HOME="+filepath.Join(ws.HomeDir, ".mantle"))
	return cmd, nil
}

// Ready: mantle's prompt is "❯ " like claude's.
func (MantleTarget) Ready(f Frame) bool { return ClaudeTarget{}.Ready(f) }

func (MantleTarget) Quit(t *Term) { ctrlCTwice(t) }

var (
	buildMu    sync.Mutex
	buildCache = map[string]string{}
)

// BuildMantleUI builds ./cmd/mantle-ui of the module containing dir (or the current
// directory) into outDir and returns the binary path. Builds are cached per outDir.
func BuildMantleUI(outDir string) (string, error) {
	buildMu.Lock()
	defer buildMu.Unlock()
	outDir, err := filepath.Abs(outDir) // exec resolves relative paths against cmd.Dir
	if err != nil {
		return "", err
	}
	if p, ok := buildCache[outDir]; ok {
		return p, nil
	}
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("parity: go env GOMOD: %w", err)
	}
	root := filepath.Dir(strings.TrimSpace(string(gomod)))
	if root == "." || root == "" {
		return "", fmt.Errorf("parity: not inside the mantle module")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	bin := filepath.Join(outDir, "mantle-ui")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mantle-ui")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("parity: build mantle-ui: %w\n%s", err, out)
	}
	buildCache[outDir] = bin
	return bin, nil
}
