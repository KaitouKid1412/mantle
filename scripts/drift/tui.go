package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

// KindDefault lists claude's UI defaults as a fresh-config claude shows them
// (Name "tui", Attrs["value"] "default" | "fullscreen").
const KindDefault = "engine-default"

var (
	altScreenOn = []byte("\x1b[?1049h")
	// Mouse tracking: only the fullscreen renderer captures the mouse.
	mouseOn = [][]byte{[]byte("\x1b[?1000h"), []byte("\x1b[?1002h"), []byte("\x1b[?1003h")}
	// The prompt is drawn: the renderer has made its choice.
	promptGlyphs = [][]byte{[]byte("❯"), []byte("? for shortcuts")}
)

// classifyTUI reads a renderer from terminal output: "fullscreen" when claude switched
// to the alternate screen or turned on mouse tracking, "default" once the prompt is
// drawn without either, "" while undecided.
func classifyTUI(out []byte) string {
	if bytes.Contains(out, altScreenOn) {
		return "fullscreen"
	}
	for _, m := range mouseOn {
		if bytes.Contains(out, m) {
			return "fullscreen"
		}
	}
	for _, g := range promptGlyphs {
		if bytes.Contains(out, g) {
			return "default"
		}
	}
	return ""
}

// probeTUI starts the interactive claude in a pty with a fresh config (onboarding,
// API-key approval and workspace trust pre-seeded so no first-run dialog decides the
// screen; no settings.json, so tui is unset) and an API at a closed local port, and
// reports which renderer it starts in. Nothing is sent to the model; it quits as soon
// as it has decided.
func probeTUI(ctx context.Context, bin string, timeout time.Duration) (string, error) {
	return probeTUIWith(ctx, bin, timeout, "")
}

// probeTUIWith is probeTUI with a user settings.json (for the live test that checks
// both renderers are recognised).
func probeTUIWith(ctx context.Context, bin string, timeout time.Duration, settings string) (string, error) {
	tmp, err := os.MkdirTemp("", "mantle-drift-tui-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	cfg, home, work := filepath.Join(tmp, "config"), filepath.Join(tmp, "home"), filepath.Join(tmp, "work")
	for _, d := range []string{cfg, home, work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	if err := fakeapi.SeedConfig(cfg, fakeapi.FakeAPIKey, work); err != nil {
		return "", err
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(settings), 0o600); err != nil {
			return "", err
		}
	}
	api, err := closedURL()
	if err != nil {
		return "", err
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(bin)
	cmd.Dir = work
	cmd.Env = fakeapi.Env([]string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TERM=xterm-256color", "COLORTERM=truecolor",
		"LANG=en_US.UTF-8", "TMPDIR=" + os.TempDir(), "NO_UPDATE_NOTIFIER=1",
	}, api, cfg, fakeapi.FakeAPIKey)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = f.Write([]byte{3, 3}) // ctrl+c twice
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // pty.Start makes a session
		_ = cmd.Wait()
		f.Close()
	}()

	var mu sync.Mutex
	var out []byte
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			mu.Lock()
			out = append(out, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			n := len(out)
			mu.Unlock()
			if n == 0 {
				return "", errors.New("interactive claude printed nothing")
			}
			return "", errors.New("couldn't tell the renderer: no alternate screen and no prompt")
		case <-tick.C:
			mu.Lock()
			v := classifyTUI(out)
			mu.Unlock()
			if v != "" {
				return v, nil
			}
		}
	}
}

// closedURL returns a local URL nothing listens on.
func closedURL() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr, nil
}

// collectDefaults probes claude's fresh-config UI defaults.
func (c *Collector) collectDefaults(ctx context.Context) List {
	l := List{Kind: KindDefault, Source: "fresh-config interactive claude in a pty"}
	if c.NoEngine {
		l.Error = "skipped (-no-engine)"
		return l
	}
	if c.Claude == "" {
		l.Error = cli.ErrClaudeNotFound.Error()
		return l
	}
	probe := c.TUIProbe
	if probe == nil {
		probe = probeTUI
	}
	v, err := probe(ctx, c.Claude, 0)
	if err != nil {
		l.Error = "tui: " + err.Error()
		return l
	}
	l.Items = []Item{{Name: "tui", Attrs: map[string]string{"value": v}}}
	return l
}

// compareDefaults checks the probed defaults against internal/cli's DefaultsTable for
// the snapshot's claude version.
func compareDefaults(l List, version string) []Finding {
	want := cli.DefaultsFor(version)
	var fs []Finding
	for _, it := range l.Items {
		if it.Name == "tui" && it.Attrs["value"] != want.TUI {
			fs = append(fs, Finding{Kind: l.Kind, Name: "tui", Status: StatusMismatch,
				Detail: "claude " + version + " starts in " + it.Attrs["value"] + " with a fresh config; internal/cli's DefaultsTable says " +
					want.TUI + ". Add an entry so mantle starts in the same renderer"})
		}
	}
	return fs
}
