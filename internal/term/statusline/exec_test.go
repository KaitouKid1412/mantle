package statusline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShellExec(t *testing.T) {
	ctx := context.Background()
	out, err := ShellExec(ctx, ExecRequest{
		Command: `printf '%s|%s|%s|' "$COLUMNS" "$LINES" "$(pwd)"; cat`,
		Dir:     t.TempDir(),
		Env:     []string{"COLUMNS=99", "LINES=7"},
		Stdin:   []byte(`{"a":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(out), "|")
	if parts[0] != "99" || parts[1] != "7" || parts[3] != `{"a":1}` {
		t.Errorf("out = %q", out)
	}
}

func TestShellExecFailure(t *testing.T) {
	_, err := ShellExec(context.Background(), ExecRequest{Command: "echo oops >&2; echo second >&2; exit 3"})
	if err == nil || !strings.Contains(err.Error(), "exit status 3: oops") || strings.Contains(err.Error(), "second") {
		t.Errorf("err = %v", err)
	}
	_, err = ShellExec(context.Background(), ExecRequest{Command: "exit 2"})
	if err == nil || err.Error() != "exit status 2" {
		t.Errorf("err = %v", err)
	}
}

// TestShellExecKillsGroup: cancelling kills children the command started, not just sh.
func TestShellExecKillsGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ShellExec(ctx, ExecRequest{Command: "(sleep 1; touch " + marker + ") & wait"})
	if err == nil {
		t.Fatal("expected an error from the killed command")
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Errorf("cancel took %v", time.Since(start))
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("child process survived cancellation")
	}
}

func TestShellExecOutputCap(t *testing.T) {
	out, err := ShellExec(context.Background(), ExecRequest{Command: "head -c 200000 /dev/zero | tr '\\0' a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != MaxOutput {
		t.Errorf("len = %d", len(out))
	}
}

// TestUserStyleScript runs a script shaped like a typical user status line (jq-free) end
// to end through the Runner with the real shell.
func TestUserStyleScript(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "sl.sh")
	body := "#!/bin/sh\ninput=$(cat)\nprintf '\\033[36m%s\\033[0m | %s cols\\n' \"$input\" \"$COLUMNS\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{Command: script, Debounce: time.Millisecond})
	defer r.Close()
	r.Resize(100, 40)
	r.Update([]byte("Opus"))
	select {
	case res := <-r.Results():
		if res.Err != nil || len(res.Lines) != 1 || res.Lines[0] != "\x1b[36mOpus\x1b[0m | 100 cols" {
			t.Errorf("res = %+v %q", res, res.Lines)
		}
	case <-time.After(30 * time.Second): // macOS scans fresh executables on first run
		t.Fatal("no result")
	}
}
