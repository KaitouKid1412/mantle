package launch

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/internal/launcher"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func newTest(t *testing.T, vars map[string]string) (*controller, *exttest.Ctx, launcher.Layout) {
	t.Helper()
	cli.ClearCurrent()
	l := launcher.Layout{Root: t.TempDir()}
	if err := l.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	e := &env{layout: l, getenv: func(k string) string { return vars[k] }, pid: 4242,
		args: []string{"--model", "opus", "hello"}, cwd: t.TempDir()}
	return &controller{env: e}, exttest.NewCtx(), l
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestRunFileAndHealthyMarker(t *testing.T) {
	const build = "20261003-120000-abcdef1"
	c, ctx, l := newTest(t, map[string]string{launcher.EnvLauncherPID: "777", launcher.EnvBuildID: build})
	if c.onStart(ctx) == nil {
		t.Fatal("no probation clock for an unhealthy build")
	}
	rf, err := launcher.ReadRunFile(l.RunFile(4242))
	if err != nil || rf.LauncherPID != 777 || rf.Version != build || !slices.Equal(rf.Argv, c.env.args) {
		t.Fatalf("run file = %+v, %v", rf, err)
	}
	c.onHealthyTick(ctx, healthyTickMsg{})
	if launcher.IsHealthy(l, build) {
		t.Fatal("healthy before the first frame and engine init")
	}
	c.onSession(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "sess-1"}})
	rf, _ = launcher.ReadRunFile(l.RunFile(4242))
	if rf.SessionID != "sess-1" || !slices.Equal(rf.HandoffArgs, []string{"--model=opus", "--resume=sess-1"}) {
		t.Errorf("run file after session = %+v", rf)
	}
	c.onFirstFrame(ctx, ext.FirstFrameMsg{})
	c.onControlResult(ctx, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: "initialize"})
	if !launcher.IsHealthy(l, build) {
		t.Fatal("not healthy after first frame, engine init and 20 s")
	}
	c.onExit(ctx, ext.ExitMsg{Code: ext.ExitRestart})
	if !exists(l.RunFile(4242)) {
		t.Error("run file removed on restart")
	}
	c.onExit(ctx, ext.ExitMsg{Code: 0})
	if exists(l.RunFile(4242)) {
		t.Error("run file kept after a clean exit")
	}
}

func TestNoRunFileWithoutLauncher(t *testing.T) {
	c, ctx, l := newTest(t, nil)
	c.onStart(ctx)
	c.onSession(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "s"}})
	if exists(l.RunFile(4242)) {
		t.Error("run file written without a launcher")
	}
}

func TestHandoffArgs(t *testing.T) {
	cli.ClearCurrent()
	s := func(id string) ext.SessionInfo { return ext.SessionInfo{SessionID: id} }
	cases := []struct {
		argv []string
		live ext.SessionInfo
		want []string
	}{
		{[]string{"fix the bug"}, s("s"), []string{"--resume=s"}},
		{[]string{"--model", "opus", "-c", "hello"}, s("s"), []string{"--model=opus", "--resume=s"}},
		{[]string{"--permission-mode", "plan"}, ext.SessionInfo{SessionID: "s", Model: "sonnet", PermissionMode: "acceptEdits"},
			[]string{"--model=sonnet", "--permission-mode=acceptEdits", "--resume=s"}},
		{[]string{"--ax-screen-reader", "hi"}, s("s"), []string{"--ax-screen-reader", "--resume=s"}},
		{[]string{"-n", "my session", "--resume", "old"}, ext.SessionInfo{SessionID: "s", Title: "Remember a word for me"}, []string{"--name=my session", "--resume=s"}},
	}
	for _, c := range cases {
		if got := handoffArgs(c.argv, t.TempDir(), c.live); !slices.Equal(got, c.want) {
			t.Errorf("handoffArgs(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
	// -w keeps the worktree (by name) so the session resumes where it lives.
	got := handoffArgs([]string{"-w", "feature", "hi"}, t.TempDir(), s("s"))
	if !slices.ContainsFunc(got, func(a string) bool { return strings.Contains(a, "feature") }) || got[len(got)-1] != "--resume=s" {
		t.Errorf("-w: %q", got)
	}
	if handoffArgs([]string{"x"}, t.TempDir(), ext.SessionInfo{}) != nil {
		t.Error("handoff without a session")
	}
	// mantle-ui's recorded startup wins over argv.
	p, _ := cli.Parse([]string{"--model", "haiku"})
	st, err := p.Startup(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cli.SetCurrent(st)
	defer cli.ClearCurrent()
	if got := handoffArgs([]string{"--model", "ignored"}, t.TempDir(), s("s")); !slices.Equal(got, []string{"--model=haiku", "--resume=s"}) {
		t.Errorf("with cli.Current: %q", got)
	}
}
