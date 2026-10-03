package parity

import (
	"context"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

// Checkpoint is a captured frame.
type Checkpoint struct {
	Name  string
	Frame Frame
	At    time.Duration // since start
}

// Result is one scenario run against one target.
type Result struct {
	Target      string
	Scenario    string
	Width       int
	Height      int
	Checkpoints []Checkpoint
	Err         error // the first failing step, or a start failure
	FailedLine  int   // scenario line of the failing step
	Final       Frame // the frame when the run ended
	Requests    int   // main-conversation API requests served
	Unmatched   []string
	Workspace   Workspace
	Duration    time.Duration
}

// Checkpoint returns a checkpoint by name.
func (r *Result) Checkpoint(name string) (Checkpoint, bool) {
	for _, c := range r.Checkpoints {
		if c.Name == name {
			return c, true
		}
	}
	return Checkpoint{}, false
}

// RunOptions tunes a run.
type RunOptions struct {
	// Timeout bounds the whole scenario (default 2 minutes).
	Timeout time.Duration
	// KeepWorkspace leaves the temp workspace on disk (for debugging).
	KeepWorkspace bool
	// TempDir is where workspaces are created ("" = os.TempDir()).
	TempDir string
	// Logf receives progress lines.
	Logf func(format string, args ...any)
}

// Run executes sc against tg in a fresh isolated workspace and returns the captured
// checkpoints. A failing step stops the run; the result keeps the checkpoints taken so
// far, the error and the final frame.
func Run(ctx context.Context, tg Target, sc *Scenario, o RunOptions) *Result {
	if o.Timeout <= 0 {
		o.Timeout = 2 * time.Minute
	}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	res := &Result{Target: tg.Name(), Scenario: sc.Name, Width: sc.Width, Height: sc.Height}
	start := time.Now()
	defer func() { res.Duration = time.Since(start) }()

	ws, cleanup, err := newWorkspace(sc, o.TempDir)
	if err != nil {
		res.Err = err
		return res
	}
	if !o.KeepWorkspace {
		defer cleanup()
	}
	script := &fakeapi.Script{DefaultReply: "OK."}
	if sc.Script != "" {
		if script, err = fakeapi.ParseFile(sc.Script); err != nil {
			res.Err = err
			return res
		}
	}
	api := fakeapi.New(script)
	srv := httptest.NewServer(api)
	defer srv.Close()
	ws.APIURL, ws.APIKey = srv.URL, fakeapi.FakeAPIKey
	if err := fakeapi.SeedConfig(ws.ConfigDir, ws.APIKey, ws.WorkDir); err != nil {
		res.Err = err
		return res
	}
	res.Workspace = ws

	cmd, err := tg.Command(ws, sc)
	if err != nil {
		res.Err = fmt.Errorf("parity: %s: %w", tg.Name(), err)
		return res
	}
	t, err := StartTerm(cmd, sc.Width, sc.Height)
	if err != nil {
		res.Err = err
		return res
	}
	defer func() {
		tg.Quit(t)
		_ = t.Close()
		res.Requests = api.Consumed()
		res.Unmatched = api.Unmatched()
	}()
	logf("%s/%s: started %s", sc.Name, tg.Name(), strings.Join(cmd.Args, " "))

	for _, st := range sc.Steps {
		if err := ctx.Err(); err != nil {
			res.Err, res.FailedLine = fmt.Errorf("scenario timeout: %w", err), st.Line
			break
		}
		if err := runStep(ctx, tg, t, st, res, start); err != nil {
			res.Err, res.FailedLine = fmt.Errorf("line %d (%s): %w", st.Line, st.Kind, err), st.Line
			logf("%s/%s: %v", sc.Name, tg.Name(), res.Err)
			break
		}
	}
	res.Final = t.Snapshot()
	return res
}

func runStep(ctx context.Context, tg Target, t *Term, st Step, res *Result, start time.Time) error {
	timeout := st.Timeout
	if timeout <= 0 {
		timeout = DefaultWait
	}
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(dl))
	}
	switch st.Kind {
	case StepReady:
		if err := t.WaitFor(tg.Ready, timeout); err != nil {
			return fmt.Errorf("prompt never became ready: %w", err)
		}
		t.Settle(150*time.Millisecond, time.Second)
	case StepType:
		t.Input(st.Text)
	case StepKeys:
		for _, k := range st.Keys {
			seq, ok := testkit.KeySeq(k)
			if !ok {
				return fmt.Errorf("unknown key %q", k)
			}
			t.Input(seq)
			time.Sleep(30 * time.Millisecond)
		}
	case StepPaste:
		t.Paste(st.Text)
	case StepWaitFor:
		err := t.WaitFor(func(f Frame) bool { return strings.Contains(strings.Join(f.Screen, "\n"), st.Text) }, timeout)
		if err != nil {
			return fmt.Errorf("%q never appeared: %w", st.Text, err)
		}
	case StepWaitGone:
		err := t.WaitFor(func(f Frame) bool { return !strings.Contains(strings.Join(f.Screen, "\n"), st.Text) }, timeout)
		if err != nil {
			return fmt.Errorf("%q never went away: %w", st.Text, err)
		}
	case StepSettle:
		t.Settle(300*time.Millisecond, timeout)
	case StepCheckpoint:
		t.Settle(200*time.Millisecond, 2*time.Second)
		res.Checkpoints = append(res.Checkpoints, Checkpoint{Name: st.Text, Frame: t.Snapshot(), At: time.Since(start)})
	case StepResize:
		if err := t.Resize(st.Width, st.Height); err != nil {
			return err
		}
		res.Width, res.Height = st.Width, st.Height
		t.Settle(200*time.Millisecond, 2*time.Second)
	case StepSleep:
		select {
		case <-time.After(st.Dur):
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return fmt.Errorf("unknown step")
	}
	return nil
}

// newWorkspace creates the isolated directories and copies the scenario's fixtures.
func newWorkspace(sc *Scenario, tmp string) (Workspace, func(), error) {
	root, err := os.MkdirTemp(tmp, "parity-"+sanitizeName(sc.Name)+"-")
	if err != nil {
		return Workspace{}, nil, err
	}
	// Resolve symlinks (/var → /private/var on macOS) so every program sees one path.
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	ws := Workspace{
		Root:      root,
		WorkDir:   filepath.Join(root, "work"),
		ConfigDir: filepath.Join(root, "config"),
		HomeDir:   filepath.Join(root, "home"),
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	for _, d := range []string{ws.WorkDir, ws.ConfigDir, ws.HomeDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			cleanup()
			return Workspace{}, nil, err
		}
	}
	if sc.Files != "" {
		if err := copyTree(sc.Files, ws.WorkDir); err != nil {
			cleanup()
			return Workspace{}, nil, fmt.Errorf("parity: copy fixtures: %w", err)
		}
	}
	return ws, cleanup, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, _ := d.Info()
		mode := os.FileMode(0o644)
		if info != nil {
			mode = info.Mode().Perm()
		}
		return os.WriteFile(target, data, mode)
	})
}

func sanitizeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, s)
}
