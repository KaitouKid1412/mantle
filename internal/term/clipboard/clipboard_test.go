package clipboard

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// fakeBin writes an executable shell script called name into dir.
func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// pathCopier is a Copier whose tools come only from dir.
func pathCopier(dir, goos string, env terminal.Map) Copier {
	return Copier{
		Env:  env,
		GOOS: goos,
		// macOS scans freshly written executables on first run, which can take seconds.
		Timeout: 30 * time.Second,
		LookPath: func(name string) (string, error) {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err != nil {
				return "", exec.ErrNotFound
			}
			return p, nil
		},
		Run: func(ctx context.Context, name string, args []string, stdin []byte) error {
			cmd := exec.CommandContext(ctx, filepath.Join(dir, name), args...)
			cmd.Stdin = strings.NewReader(string(stdin))
			return cmd.Run()
		},
	}
}

func TestCopyDarwinPbcopy(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	fakeBin(t, dir, "pbcopy", "cat > "+out+"\n")
	res, err := pathCopier(dir, "darwin", terminal.Map{}).Copy(context.Background(), "héllo\nworld")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != Pbcopy || res.Sequence != nil {
		t.Errorf("res = %+v", res)
	}
	if got, _ := os.ReadFile(out); string(got) != "héllo\nworld" {
		t.Errorf("clipboard got %q", got)
	}
}

func TestCopyLinuxOrder(t *testing.T) {
	tests := []struct {
		name  string
		env   terminal.Map
		bins  map[string]string // name -> script
		want  Method
		fails bool
	}{
		{"wayland", terminal.Map{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
			map[string]string{"wl-copy": "cat >/dev/null", "xclip": "exit 1"}, WlCopy, false},
		{"x11 xclip", terminal.Map{"DISPLAY": ":0"},
			map[string]string{"xclip": `[ "$1 $2" = "-selection clipboard" ] || exit 3; cat >/dev/null`, "xsel": "exit 1"}, Xclip, false},
		{"x11 xsel when xclip missing", terminal.Map{"DISPLAY": ":0"},
			map[string]string{"xsel": `[ "$1 $2" = "--clipboard --input" ] || exit 3; cat >/dev/null`}, Xsel, false},
		{"xclip fails then xsel", terminal.Map{"DISPLAY": ":0"},
			map[string]string{"xclip": "exit 1", "xsel": "cat >/dev/null"}, Xsel, false},
		{"no display falls back to osc52", terminal.Map{},
			map[string]string{"xclip": "cat >/dev/null"}, OSC52, false},
		{"all fail falls back to osc52", terminal.Map{"DISPLAY": ":0"},
			map[string]string{"xclip": "exit 1", "xsel": "exit 1"}, OSC52, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, script := range tt.bins {
				fakeBin(t, dir, name, script+"\n")
			}
			res, err := pathCopier(dir, "linux", tt.env).Copy(context.Background(), "x")
			if err != nil {
				t.Fatal(err)
			}
			if res.Method != tt.want {
				t.Errorf("Method = %q, want %q", res.Method, tt.want)
			}
			if (res.Method == OSC52) != (res.Sequence != nil) {
				t.Errorf("Sequence presence wrong: %+v", res)
			}
			if (res.Fallback != nil) != tt.fails {
				t.Errorf("Fallback = %v", res.Fallback)
			}
		})
	}
}

func TestCopySSHUsesOSC52(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	fakeBin(t, dir, "pbcopy", "touch "+marker+"\n")
	res, err := pathCopier(dir, "darwin", terminal.Map{"SSH_TTY": "/dev/ttys003"}).Copy(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != OSC52 || string(res.Sequence) != "\x1b]52;c;aGk=\a" {
		t.Errorf("res = %+v (%q)", res, res.Sequence)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("local pbcopy must not run over ssh")
	}
}

func TestCopySSHInTmux(t *testing.T) {
	c := Copier{Env: terminal.Map{"SSH_CONNECTION": "1 2 3 4", "TMUX": "/tmp/t"}, GOOS: "linux"}
	res, err := c.Copy(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if want := "\x1bPtmux;\x1b\x1b]52;c;aGk=\a\x1b\\"; string(res.Sequence) != want {
		t.Errorf("Sequence = %q, want %q", res.Sequence, want)
	}
}

func TestCopyTooLarge(t *testing.T) {
	c := Copier{Env: terminal.Map{"SSH_TTY": "x"}, GOOS: "linux"}
	_, err := c.Copy(context.Background(), strings.Repeat("a", MaxOSC52))
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v", err)
	}
}

func TestSequence52(t *testing.T) {
	text := "multi\nline ✓"
	got := string(Sequence52(text, false))
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if got != want {
		t.Errorf("Sequence52 = %q, want %q", got, want)
	}
}

// TestDefaultRun exercises the real exec path with a fake tool on PATH.
func TestDefaultRun(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	fakeBin(t, dir, "xclip", "/bin/cat > "+out+"\n")
	fakeBin(t, dir, "xsel", "echo 'no display' >&2; exit 1\n")
	t.Setenv("PATH", dir)
	c := Copier{Env: terminal.Map{"DISPLAY": ":0"}, GOOS: "linux", Timeout: 30 * time.Second}
	res, err := c.Copy(context.Background(), "payload")
	if err != nil || res.Method != Xclip {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if got, _ := os.ReadFile(out); string(got) != "payload" {
		t.Errorf("got %q", got)
	}

	os.Remove(filepath.Join(dir, "xclip"))
	res, err = c.Copy(context.Background(), "payload")
	if err != nil || res.Method != OSC52 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if res.Fallback == nil || !strings.Contains(res.Fallback.Error(), "no display") {
		t.Errorf("Fallback should carry stderr: %v", res.Fallback)
	}
}
