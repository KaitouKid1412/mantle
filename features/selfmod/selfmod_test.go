package selfmod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// stubGo stands in for the go command: vet fails while a file named BROKEN
// exists in the worktree, build copies the fake UI, everything else passes.
const stubGo = `#!/bin/sh
sub="$1"
if [ "$sub" = vet ] && [ -n "$(find . -name BROKEN -not -path './.git/*' 2>/dev/null)" ]; then
  echo "mods/demo/demo.go:3:1: undefined: broken"; exit 1
fi
if [ "$sub" = build ]; then
  out=""; prev=""
  for a in "$@"; do [ "$prev" = "-o" ] && out="$a"; prev="$a"; done
  [ -n "$out" ] && cp "$STUB_DIR/fake-ui" "$out" && chmod +x "$out"
fi
exit 0
`

const fakeUI = `#!/bin/sh
[ "$1" = selftest ] && { echo ok; exit 0; }
echo "mantle ready"
`

const stubGofmt = "#!/bin/sh\nexit 0\n"

type flow struct {
	t       *testing.T
	ctx     *exttest.Ctx
	reg     *exttest.Registrar
	ctl     *controller
	env     *env
	l       launcher.Layout
	dev     string
	stubDir string

	starts  []ext.EngineStartMsg
	stops   []string
	prompts []string
	exits   []ext.ExitMsg
	ticks   int
	// builder plays the builder engine: it gets each prompt (and its round)
	// and returns the events of the turn.
	builder func(f *flow, round int, prompt string) []proto.Event
	round   int
	env2    map[string]string
}

type fakeEngine struct {
	id string
	f  *flow
}

func (e *fakeEngine) Send(p ext.Prompt) tea.Cmd {
	text := ""
	if len(p.Blocks) > 0 {
		text = p.Blocks[0].Text
	}
	e.f.prompts = append(e.f.prompts, text)
	e.f.round++
	evs := e.f.builder(e.f, e.f.round, text)
	return func() tea.Msg {
		var cmds []tea.Cmd
		for _, ev := range evs {
			cmds = append(cmds, ext.Msg(ext.EngineEventMsg{EngineID: e.id, Event: ev}))
		}
		return tea.BatchMsg(cmds)
	}
}
func (e *fakeEngine) Interrupt(bool) tea.Cmd        { return nil }
func (e *fakeEngine) Control(string, any) tea.Cmd   { return nil }
func (e *fakeEngine) Supports(string) bool          { return true }
func (e *fakeEngine) Restart(ext.SpawnOpts) tea.Cmd { return nil }

func gitEnv(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv(launcher.EnvClaudeBin, "/bin/echo")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newFlow installs a tiny repository with stub tools and returns a
// controller wired to an exttest Ctx and Registrar.
func newFlow(t *testing.T) *flow {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	gitEnv(t)
	root := t.TempDir()
	f := &flow{t: t, l: launcher.Layout{Root: filepath.Join(root, "home")}, dev: filepath.Join(root, "dev"), stubDir: filepath.Join(root, "stub")}
	write(t, f.stubDir, map[string]string{"go": stubGo, "fake-ui": fakeUI, "gofmt": stubGofmt})
	for _, n := range []string{"go", "fake-ui", "gofmt"} {
		os.Chmod(filepath.Join(f.stubDir, n), 0o755)
	}
	t.Setenv("STUB_DIR", f.stubDir)
	os.MkdirAll(f.dev, 0o755)
	run(t, f.dev, "git", "init", "-q", "-b", "main")
	write(t, f.dev, map[string]string{"go.mod": "module example.com/m\n\ngo 1.27\n", "mods/doc.go": "package mods\n", "cmd/mantle-ui/main.go": "package main\n"})
	run(t, f.dev, "git", "add", "-A")
	run(t, f.dev, "git", "commit", "-qm", "initial")
	var out bytes.Buffer
	if _, err := sm.Install(context.Background(), sm.InstallOptions{Layout: f.l, DevRepo: f.dev, Go: filepath.Join(f.stubDir, "go"), Out: &out}); err != nil {
		t.Fatalf("install: %v\n%s", err, out.String())
	}
	f.env2 = map[string]string{}
	f.env = &env{
		layout: f.l,
		getenv: func(k string) string { return f.env2[k] },
		pid:    4242,
		args:   []string{"--model", "opus", "fix the bug"},
		cwd:    f.dev,
		workspace: func(source string) *sm.Workspace {
			ws := sm.NewWorkspace(f.l)
			ws.Go = filepath.Join(f.stubDir, "go")
			ws.Pipeline = sm.Config{
				Go: ws.Go, Gofmt: filepath.Join(f.stubDir, "gofmt"),
				ArchCheck: func(context.Context, string) ([]string, error) { return nil, nil },
				Smoke:     sm.SmokeConfig{Script: []sm.SmokeAction{sm.Expect("mantle ready")}},
			}
			return ws
		},
		claudeSettingsPath: filepath.Join(root, "claude", "settings.json"),
	}
	f.ctl = newController(f.env)
	f.reg = exttest.NewRegistrar()
	f.reg.Feature = FeatureID
	if err := f.ctl.setup(f.reg); err != nil {
		t.Fatal(err)
	}
	f.ctx = exttest.NewCtx()
	f.ctx.SessionValue.Model = "sonnet"
	f.ctx.Renderers[KeyBuild] = RenderBuild
	f.builder = func(*flow, int, string) []proto.Event { return []proto.Event{result("done", 0.1)} }
	return f
}

func result(text string, cost float64) *proto.Result {
	r := &proto.Result{Result: text, TotalCostUSD: cost}
	r.Type, r.Subtype, r.SessionID = "result", "success", "builder-session"
	return r
}

func toolUse(name, path string) *proto.Assistant {
	a := &proto.Assistant{}
	a.Type = "assistant"
	in, _ := json.Marshal(map[string]string{"file_path": path})
	a.Message.Content = []proto.ContentBlock{{Type: proto.BlockToolUse, Name: name, Input: in}}
	return a
}

// run executes cmd and everything it leads to, playing the host: engine
// start and stop requests are answered, other messages go to the
// registrar's subscribers.
func (f *flow) run(cmd tea.Cmd) {
	f.t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 10000 {
			f.t.Fatal("message loop does not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch m := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, m...)
		case ext.EngineStartMsg:
			f.starts = append(f.starts, m)
			eng := &fakeEngine{id: m.EngineID, f: f}
			f.ctx.Engines[m.EngineID] = eng
			queue = append(queue, ext.Msg(ext.EngineAttachMsg{EngineID: m.EngineID, Engine: eng}))
		case ext.EngineStopMsg:
			f.stops = append(f.stops, m.EngineID)
			delete(f.ctx.Engines, m.EngineID)
			queue = append(queue, ext.Msg(ext.EngineExitedMsg{EngineID: m.EngineID}))
		case tickMsg:
			f.ticks++ // the exttest clock fires at once; don't loop on it
		case ext.ExitMsg:
			f.exits = append(f.exits, m)
			queue = append(queue, f.reg.Dispatch(f.ctx, m))
		default:
			queue = append(queue, f.reg.Dispatch(f.ctx, m))
		}
	}
}

func (f *flow) command(args string) {
	f.t.Helper()
	cmd, ok := f.reg.Command("mantle")
	if !ok {
		f.t.Fatal("no /mantle command")
	}
	f.run(cmd.Run(f.ctx, args))
}

func (f *flow) notices() string {
	var b strings.Builder
	for _, n := range f.ctx.Notices {
		b.WriteString(n.Text + "\n")
	}
	return b.String()
}

func (f *flow) build(id string) *build {
	b := f.ctl.builds[id]
	if b == nil {
		f.t.Fatalf("no build %q; notices:\n%s", id, f.notices())
	}
	return b
}

func TestRegistration(t *testing.T) {
	reg, err := exttest.Setup(feature(&env{layout: launcher.Layout{Root: t.TempDir()}, getenv: func(string) string { return "" }, workspace: func(string) *sm.Workspace { return nil }}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Command("mantle"); !ok {
		t.Error("no /mantle command")
	}
	if reg.Renderers[KeyBuild] == nil || len(reg.Components) != 1 || reg.Dialogs[PreviewDialogID] == nil {
		t.Errorf("renderers %v components %d dialogs %v", reg.Renderers, len(reg.Components), reg.Dialogs)
	}
	var keys []string
	for _, s := range reg.Settings {
		keys = append(keys, s.Key)
	}
	if !slices.Equal(keys, []string{SettingConfirm, SettingMaxBudget, SettingModel, SettingSource}) {
		t.Errorf("settings = %v", keys)
	}
	if len(reg.Stories) < 8 {
		t.Errorf("%d stories", len(reg.Stories))
	}
	f := Feature()
	if f.ID != FeatureID || f.Order >= ext.ModOrder || !slices.Contains(f.Parity, "MT-16") || !slices.Contains(f.Parity, "MT-35") {
		t.Errorf("feature = %+v", f)
	}
}

func TestParseCommand(t *testing.T) {
	cases := []struct{ in, name, id, rest string }{
		{"", "", "", ""},
		{"make the spinner blue", "", "", "make the spinner blue"},
		{"list", "list", "", ""},
		{"list the tools in the footer", "", "", "list the tools in the footer"},
		{"show demo", "show", "demo", ""},
		{"show me how it works", "", "", "show me how it works"},
		{"undo spinner-blue", "undo", "spinner-blue", ""},
		{"retry", "retry", "", ""},
		{"retry demo", "retry", "demo", ""},
		{"edit demo make it red", "edit", "demo", "make it red"},
		{"edit demo", "", "", "edit demo"},
		{"status", "status", "", ""},
		{"restart", "restart", "", ""},
		{"update", "update", "", ""},
		{"update the footer colors", "", "", "update the footer colors"},
		{"upstream demo", "upstream", "demo", ""},
		{"apply Bad_ID", "", "", "apply Bad_ID"},
	}
	for _, c := range cases {
		name, id, rest := parseCommand(c.in)
		if name != c.name || id != c.id || rest != c.rest {
			t.Errorf("parseCommand(%q) = %q %q %q, want %q %q %q", c.in, name, id, rest, c.name, c.id, c.rest)
		}
	}
}

func TestRequestBuildsAndPromotes(t *testing.T) {
	f := newFlow(t)
	first := (launcher.Store{L: f.l}).CurrentID()
	f.ctx.SettingsV.MantleM[SettingMaxBudget] = 2.5
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		write(f.t, f.l.Worktree("demo-command"), map[string]string{"mods/demo/demo.go": "package demo\n"})
		return []proto.Event{toolUse("Write", "mods/demo/demo.go"), result("Added mods/demo.", 0.42)}
	}
	f.command("add a demo command")

	b := f.build("demo-command")
	if b.phase != PhaseDone {
		t.Fatalf("phase = %s, err %q, detail %q\nnotices:\n%s", b.phase, b.err, b.detail, f.notices())
	}
	if len(f.starts) != 1 {
		t.Fatalf("starts = %+v", f.starts)
	}
	st := f.starts[0]
	if st.EngineID != "builder-demo-command" || st.Opts.Cwd != f.l.Worktree("demo-command") || st.Opts.PermissionMode != "acceptEdits" || st.Opts.Model != "sonnet" {
		t.Errorf("start = %+v", st)
	}
	if i := slices.Index(st.Opts.ExtraArgs, "--max-budget-usd"); i < 0 || st.Opts.ExtraArgs[i+1] != "2.5" {
		t.Errorf("budget args = %q", st.Opts.ExtraArgs)
	}
	if i := slices.Index(st.Opts.ExtraArgs, "--append-system-prompt-file"); i < 0 || !fileExists(st.Opts.ExtraArgs[i+1]) {
		t.Errorf("rules file args = %q", st.Opts.ExtraArgs)
	}
	if len(f.prompts) != 1 || !strings.Contains(f.prompts[0], "add a demo command") {
		t.Errorf("prompts = %q", f.prompts)
	}
	if b.lastTool != "Write mods/demo/demo.go" || b.cost != 0.42 || b.sessionID != "builder-session" {
		t.Errorf("progress: tool %q cost %v session %q", b.lastTool, b.cost, b.sessionID)
	}
	if !slices.Equal(f.stops, []string{"builder-demo-command"}) {
		t.Errorf("stops = %v", f.stops)
	}
	cur := (launcher.Store{L: f.l}).CurrentID()
	if cur == first || cur != b.version {
		t.Errorf("current = %s (was %s), build version %s", cur, first, b.version)
	}
	if !strings.Contains(f.notices(), "demo-command is ready; active next launch") {
		t.Errorf("notices:\n%s", f.notices())
	}
	if strings.Contains(f.notices(), "/mantle restart") {
		t.Error("restart hint without the launcher")
	}
	if len(f.ctx.Printed) != 1 || !strings.Contains(ansi.Strip(f.ctx.Printed[0]), "/mantle demo-command") || !strings.Contains(ansi.Strip(f.ctx.Printed[0]), "active next launch") {
		t.Errorf("printed = %q", f.ctx.Printed)
	}
	if len(f.ctl.activeBuilds()) != 0 {
		t.Error("build still active")
	}

	// list, show and status print.
	f.ctx.Printed = nil
	f.command("list")
	f.command("show demo-command")
	f.command("status")
	all := ansi.Strip(strings.Join(f.ctx.Printed, "\n"))
	for _, want := range []string{"demo-command", "add a demo command", "mods/demo/demo.go", "checks: protected ok", "Current build " + cur} {
		if !strings.Contains(all, want) {
			t.Errorf("output missing %q:\n%s", want, all)
		}
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestFixLoopThenGiveUp(t *testing.T) {
	f := newFlow(t)
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		wt := f.l.Worktree("broken-thing")
		write(f.t, wt, map[string]string{"mods/demo/demo.go": "package demo\n", "mods/demo/BROKEN": "x"})
		if round == 2 && strings.Contains(prompt, "Round 1 of 3") && strings.Contains(prompt, "undefined: broken") {
			os.Remove(filepath.Join(wt, "mods/demo/BROKEN"))
		}
		return []proto.Event{result("tried", float64(round))}
	}
	f.command("broken thing")
	b := f.build("broken-thing")
	if b.phase != PhaseDone || len(f.prompts) != 2 {
		t.Fatalf("phase %s after %d prompts: %q\n%s", b.phase, len(f.prompts), f.prompts, f.notices())
	}

	// A builder that never fixes it stops after three rounds; the worktree stays.
	g := newFlow(t)
	g.builder = func(f *flow, round int, prompt string) []proto.Event {
		write(f.t, f.l.Worktree("hopeless"), map[string]string{"mods/demo/BROKEN": "x"})
		return []proto.Event{result("tried", 1)}
	}
	g.command("hopeless")
	hb := g.build("hopeless")
	if hb.phase != PhaseFailed || len(g.prompts) != 3 || !strings.Contains(hb.err, "after 3 rounds") {
		t.Fatalf("phase %s, %d prompts, err %q", hb.phase, len(g.prompts), hb.err)
	}
	if !fileExists(g.l.Worktree("hopeless")) {
		t.Error("worktree removed after failure")
	}
	if r, _ := g.env.workspace("").Load("hopeless"); r.State != sm.StateFailed {
		t.Errorf("saved state %s", r.State)
	}

	// /mantle retry gives the builder the failure and three more rounds.
	g.prompts = nil
	g.round = 0
	g.builder = func(f *flow, round int, prompt string) []proto.Event {
		os.Remove(filepath.Join(f.l.Worktree("hopeless"), "mods/demo/BROKEN"))
		write(f.t, f.l.Worktree("hopeless"), map[string]string{"mods/demo/demo.go": "package demo\n"})
		return []proto.Event{result("fixed", 1)}
	}
	g.command("retry hopeless")
	if hb := g.build("hopeless"); hb.phase != PhaseDone {
		t.Fatalf("retry phase %s err %q\n%s", hb.phase, hb.err, g.notices())
	}
	if len(g.prompts) != 1 || !strings.Contains(g.prompts[0], "A previous attempt") || !strings.Contains(g.prompts[0], "undefined: broken") {
		t.Errorf("retry prompt = %q", g.prompts)
	}
}

func TestBuilderErrorsAndBudget(t *testing.T) {
	f := newFlow(t)
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		r := result("", 5)
		r.Subtype, r.IsError = "error_max_budget_usd", true
		return []proto.Event{r}
	}
	f.command("expensive thing")
	b := f.build("expensive-thing")
	if b.phase != PhaseFailed || !strings.Contains(b.err, "$5.00 budget") {
		t.Errorf("phase %s err %q", b.phase, b.err)
	}

	// The builder engine dies mid-turn.
	g := newFlow(t)
	g.builder = func(f *flow, round int, prompt string) []proto.Event { return nil }
	g.command("crashy thing")
	g.run(ext.Msg(ext.EngineExitedMsg{EngineID: "builder-crashy-thing", Err: fmt.Errorf("exit status 1"), Stderr: "boom\n"}))
	cb := g.build("crashy-thing")
	if cb.phase != PhaseFailed || !strings.Contains(cb.err, "exit status 1") || !slices.Contains(cb.detail, "boom") {
		t.Errorf("phase %s err %q detail %q", cb.phase, cb.err, cb.detail)
	}
}

func TestConfigProposal(t *testing.T) {
	f := newFlow(t)
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		return []proto.Event{result("No code needed.\nMANTLE_CONFIG_PROPOSAL\n"+
			`{"summary": "Pirate verbs", "scope": "claude", "key": "spinnerVerbs", "value": {"mode": "replace", "verbs": ["Plundering"]}}`+
			"\nEND_MANTLE_CONFIG_PROPOSAL", 0.05)}
	}
	f.command("pirate spinner verbs")
	b := f.build("pirate-spinner-verbs")
	if b.phase != PhaseConfig || b.proposal == nil || b.proposal.Key != "spinnerVerbs" {
		t.Fatalf("phase %s proposal %+v\n%s", b.phase, b.proposal, f.notices())
	}
	if fileExists(f.l.Worktree("pirate-spinner-verbs")) {
		t.Error("worktree kept for a config proposal")
	}
	if !strings.Contains(f.notices(), "/mantle apply pirate-spinner-verbs") {
		t.Errorf("notices:\n%s", f.notices())
	}
	f.command("apply pirate-spinner-verbs")
	got := f.ctx.SettingsV.Scopes[ext.ScopeUser]["spinnerVerbs"]
	if m, ok := got.(map[string]any); !ok || m["mode"] != "replace" {
		t.Errorf("claude user settings = %#v", f.ctx.SettingsV.Scopes)
	}

	// A mantle-scope proposal goes through SetMantle; from disk after a restart.
	os.WriteFile(filepath.Join(f.l.Build("other"), "proposal.json"), nil, 0o644)
	os.MkdirAll(f.l.Build("mantle-one"), 0o755)
	os.WriteFile(filepath.Join(f.l.Build("mantle-one"), "proposal.json"), []byte(`{"summary":"x","scope":"mantle","key":"chrome.compact","value":true}`), 0o644)
	f.command("apply mantle-one")
	if f.ctx.SettingsV.MantleM["chrome.compact"] != true {
		t.Errorf("mantle settings = %v", f.ctx.SettingsV.MantleM)
	}
}

func TestMergeJSONSetting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "settings.json")
	if err := MergeJSONSetting(p, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, []byte(`{"model": "opus", "theme": "light"}`), 0o600)
	os.Chmod(p, 0o600)
	if err := MergeJSONSetting(p, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	data, _ := os.ReadFile(p)
	json.Unmarshal(data, &m)
	if m["model"] != "opus" || m["theme"] != "dark" {
		t.Errorf("merged = %v", m)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if bak, _ := os.ReadFile(p + ".mantle-bak"); !strings.Contains(string(bak), "light") {
		t.Errorf("backup = %s", bak)
	}
	os.WriteFile(p, []byte("not json"), 0o644)
	if err := MergeJSONSetting(p, "theme", "dark"); err == nil {
		t.Error("overwrote invalid JSON")
	}
	if data, _ := os.ReadFile(p); string(data) != "not json" {
		t.Error("invalid file changed")
	}
}

func TestUndoCommand(t *testing.T) {
	f := newFlow(t)
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		write(f.t, f.l.Worktree("removable"), map[string]string{"mods/removable/r.go": "package removable\n"})
		return []proto.Event{result("ok", 0.1)}
	}
	f.command("removable")
	if f.build("removable").phase != PhaseDone {
		t.Fatal(f.notices())
	}
	starts := len(f.starts)
	f.command("undo removable")
	ub := f.build("undo-removable")
	if ub.phase != PhaseDone {
		t.Fatalf("undo phase %s err %q\n%s", ub.phase, ub.err, f.notices())
	}
	if len(f.starts) != starts {
		t.Error("undo started a builder")
	}
	mods, _ := f.env.workspace("").Mods()
	if len(mods) != 1 || mods[0].State != sm.ModUndone {
		t.Errorf("mods = %+v", mods)
	}
	f.command("rollback")
	if !strings.Contains(ansi.Strip(strings.Join(f.ctx.Printed, "\n")), "current build:") {
		t.Errorf("printed = %q", f.ctx.Printed)
	}
}

func TestRunFileAndHealthyMarker(t *testing.T) {
	f := newFlow(t)
	cur := (launcher.Store{L: f.l}).CurrentID()
	if launcher.IsHealthy(f.l, cur) {
		os.Remove(f.l.HealthyMarker(cur))
	}
	f.env2[launcher.EnvLauncherPID] = "777"
	f.env2[launcher.EnvBuildID] = cur
	f.env2[launcher.EnvProbation] = "1"

	var start tea.Cmd
	for _, s := range f.reg.Starts {
		start = s.Value.(func(ext.Ctx) tea.Cmd)(f.ctx)
	}
	rf, err := launcher.ReadRunFile(f.l.RunFile(4242))
	if err != nil || rf.LauncherPID != 777 || rf.Version != cur || rf.Cwd != f.dev || !slices.Equal(rf.Argv, f.env.args) {
		t.Fatalf("run file = %+v, %v", rf, err)
	}
	// The 20 s tick alone does not mark the build healthy.
	f.run(start)
	if launcher.IsHealthy(f.l, cur) {
		t.Fatal("healthy before the first frame and engine init")
	}
	f.run(ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{SessionID: "sess-1"}}))
	rf, _ = launcher.ReadRunFile(f.l.RunFile(4242))
	if rf.SessionID != "sess-1" || !slices.Equal(rf.HandoffArgs, []string{"--model", "opus", "--resume", "sess-1"}) {
		t.Errorf("run file after session = %+v", rf)
	}
	f.run(ext.Msg(ext.FirstFrameMsg{}))
	f.run(ext.Msg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: "initialize"}))
	if !launcher.IsHealthy(f.l, cur) {
		t.Fatal("not healthy after first frame, engine init and 20 s")
	}

	// Restart: refused while a turn runs, then exit 75 with the run file kept.
	running := &proto.SessionStateChanged{State: proto.StateRunning}
	f.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: running}))
	f.command("restart")
	if len(f.exits) != 0 || !strings.Contains(f.notices(), "a turn is running") {
		t.Fatalf("exits %v notices:\n%s", f.exits, f.notices())
	}
	f.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{{TaskID: "t"}}}}))
	f.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.SessionStateChanged{State: proto.StateIdle}}))
	f.command("restart")
	if len(f.exits) != 0 || !strings.Contains(f.notices(), "background tasks") {
		t.Fatalf("exits %v notices:\n%s", f.exits, f.notices())
	}
	f.run(ext.Msg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{{TaskID: "a", Ambient: true}}}}))
	f.command("restart")
	if len(f.exits) != 1 || f.exits[0].Code != ext.ExitRestart {
		t.Fatalf("exits = %v\n%s", f.exits, f.notices())
	}
	if !fileExists(f.l.RunFile(4242)) {
		t.Error("run file removed on restart")
	}
	// A clean exit removes it.
	f.run(ext.Msg(ext.ExitMsg{Code: 0}))
	if fileExists(f.l.RunFile(4242)) {
		t.Error("run file kept after a clean exit")
	}
}

func TestNoRunFileWithoutLauncher(t *testing.T) {
	f := newFlow(t)
	for _, s := range f.reg.Starts {
		f.run(s.Value.(func(ext.Ctx) tea.Cmd)(f.ctx))
	}
	if fileExists(f.l.RunFile(4242)) {
		t.Error("run file written without a launcher")
	}
	f.command("restart")
	if len(f.exits) != 0 || !strings.Contains(f.notices(), "not started by the mantle launcher") {
		t.Errorf("exits %v notices:\n%s", f.exits, f.notices())
	}
}

func TestHandoffArgs(t *testing.T) {
	cases := []struct {
		argv []string
		want []string
	}{
		{[]string{"fix the bug"}, []string{"--resume", "s"}},
		{[]string{"--model", "opus", "-c", "hello"}, []string{"--model", "opus", "--resume", "s"}},
		{[]string{"-w", "feature", "--permission-mode", "plan"}, []string{"--worktree", "feature", "--permission-mode", "plan", "--resume", "s"}},
		{[]string{"--ax-screen-reader", "hi"}, []string{"--ax-screen-reader", "--resume", "s"}},
		{[]string{"-n", "my session", "--resume", "old"}, []string{"--name", "my session", "--resume", "s"}},
	}
	for _, c := range cases {
		if got := handoffArgs(c.argv, "s"); !slices.Equal(got, c.want) {
			t.Errorf("handoffArgs(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
	if handoffArgs([]string{"x"}, "") != nil {
		t.Error("handoff without a session")
	}
}

func TestRendererFitsWidth(t *testing.T) {
	ctx := exttest.NewCtx()
	reg, _ := exttest.Setup(feature(&env{layout: launcher.Layout{Root: t.TempDir()}, getenv: func(string) string { return "" }}))
	for _, s := range reg.Stories {
		for _, w := range []int{60, 100, 160} {
			out := s.Render(ctx, ext.Area{Width: w}).Text
			if out == "" {
				t.Errorf("%s at %d: empty", s.ID, w)
			}
			for _, ln := range strings.Split(out, "\n") {
				if ansi.StringWidth(ln) > w {
					t.Errorf("%s at %d: line too wide (%d): %q", s.ID, w, ansi.StringWidth(ln), ansi.Strip(ln))
				}
			}
		}
	}
	// The progress component shows only running builds.
	c := newController(&env{})
	c.add(&build{req: &sm.Request{ID: "a", Request: "x"}, phase: PhaseBuilding, start: exttest.Epoch})
	c.add(&build{req: &sm.Request{ID: "b"}, phase: PhaseDone})
	ctx.Renderers[KeyBuild] = RenderBuild
	view := ansi.Strip((&progressComp{c: c}).View(ctx, ext.Area{Width: 80}).Text)
	if !strings.Contains(view, "/mantle a · building") || strings.Contains(view, "/mantle b") {
		t.Errorf("view = %q", view)
	}
}

func TestPreviewDialog(t *testing.T) {
	c := newController(&env{})
	d, err := c.newPreviewDialog(nil, previewArgs{ID: "x", Diffs: []StoryDiff{{ID: "s", Width: 100, Before: "a", After: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := exttest.NewCtx()
	text := ansi.Strip(d.View(ctx, ext.Area{Width: 80, MaxHeight: 20}).Text)
	if !strings.Contains(text, "1 story looks different") || !strings.Contains(text, "a") || !strings.Contains(text, "y install") {
		t.Errorf("view:\n%s", text)
	}
	d.HandleKey(ctx, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if r := d.(ext.Resulter).Result().(previewResult); !r.Accept || r.ID != "x" {
		t.Errorf("result = %+v", r)
	}
	if len(ctx.Closed) != 1 {
		t.Errorf("closed = %v", ctx.Closed)
	}
	if _, err := c.newPreviewDialog(nil, 42); err == nil {
		t.Error("bad args accepted")
	}
}

func TestPreviewFlowKeepsBuild(t *testing.T) {
	f := newFlow(t)
	f.env.stories = func(ctx context.Context, before, after string) ([]StoryDiff, error) {
		return []StoryDiff{{ID: "chrome.footer/default", Width: 100, Before: "old", After: "new"}}, nil
	}
	f.builder = func(f *flow, round int, prompt string) []proto.Event {
		write(f.t, f.l.Worktree("footer-color"), map[string]string{"mods/footer/f.go": "package footer\n"})
		return []proto.Event{result("ok", 0.1)}
	}
	f.command("footer color")
	b := f.build("footer-color")
	if b.phase != PhasePreviewing || len(f.ctx.Opened) != 1 || f.ctx.Opened[0] != PreviewDialogID {
		t.Fatalf("phase %s opened %v\n%s", b.phase, f.ctx.Opened, f.notices())
	}
	f.run(ext.Msg(ext.DialogClosedMsg{ID: PreviewDialogID, Result: previewResult{ID: "footer-color", Accept: false}}))
	if b.phase != PhaseReady || !strings.Contains(f.notices(), "/mantle promote footer-color") {
		t.Fatalf("phase %s\n%s", b.phase, f.notices())
	}
	f.command("promote footer-color")
	if b.phase != PhaseDone {
		t.Errorf("phase after promote %s err %q", b.phase, b.err)
	}

	// selfmod.confirm=on-visual-change with no differences promotes directly.
	g := newFlow(t)
	g.env.stories = func(context.Context, string, string) ([]StoryDiff, error) { return nil, nil }
	g.builder = func(f *flow, round int, prompt string) []proto.Event {
		write(f.t, f.l.Worktree("quiet-change"), map[string]string{"mods/q/q.go": "package q\n"})
		return []proto.Event{result("ok", 0.1)}
	}
	g.command("quiet change")
	if g.build("quiet-change").phase != PhaseDone || len(g.ctx.Opened) != 0 {
		t.Errorf("phase %s opened %v", g.build("quiet-change").phase, g.ctx.Opened)
	}
}

func TestDiffStories(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, footer string) string {
		p := filepath.Join(dir, name)
		script := "#!/bin/sh\nif [ \"$1\" = catalog ]; then echo '[{\"kind\":\"story\",\"id\":\"a/x\"},{\"kind\":\"command\",\"id\":\"cmd.x\"},{\"kind\":\"story\",\"id\":\"b/y\"}]'; exit 0; fi\n" +
			"case \"$2\" in a/x) echo same;; b/y) echo " + footer + ";; *) exit 3;; esac\n"
		os.WriteFile(p, []byte(script), 0o755)
		return p
	}
	before, after := mk("before", "old"), mk("after", "new")
	diffs, err := DiffStories(context.Background(), before, after)
	if err != nil || len(diffs) != 1 || diffs[0].ID != "b/y" || diffs[0].Before != "old" || diffs[0].After != "new" {
		t.Fatalf("diffs = %+v, %v", diffs, err)
	}
	diffs, _ = DiffStories(context.Background(), "", after)
	if len(diffs) != 2 || !diffs[0].New {
		t.Errorf("without a current build: %+v", diffs)
	}
	// mantle-ui story --list is preferred over the catalog.
	lister := filepath.Join(dir, "lister")
	os.WriteFile(lister, []byte("#!/bin/sh\nif [ \"$1\" = story ] && [ \"$2\" = --list ]; then printf 'z/two\\n\\nz/one\\n'; exit 0; fi\necho \"render $2\"\n"), 0o755)
	diffs, err = DiffStories(context.Background(), "", lister)
	if err != nil || len(diffs) != 2 || diffs[0].ID != "z/one" || diffs[0].After != "render z/one" {
		t.Errorf("story --list diffs = %+v, %v", diffs, err)
	}
	var ids []string
	collectStories(map[string]any{"apiVersion": 1, "entries": []any{map[string]any{"kind": "story", "id": "e1"}, map[string]any{"kind": "command", "id": "cmd.x"}}}, false, &ids)
	if !slices.Equal(ids, []string{"e1"}) {
		t.Errorf("catalog entries = %v", ids)
	}
	ids = nil
	collectStories(map[string]any{"stories": []any{"s1", map[string]any{"id": "s2"}}, "commands": []any{map[string]any{"id": "c"}}}, false, &ids)
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"s1", "s2"}) {
		t.Errorf("ids = %v", ids)
	}
}

func TestToolSummary(t *testing.T) {
	in, _ := json.Marshal(map[string]string{"command": "go test   ./mods/demo/...\n"})
	if got := toolSummary("Bash", in); got != "Bash go test ./mods/demo/..." {
		t.Errorf("got %q", got)
	}
	if got := toolSummary("TodoWrite", nil); got != "TodoWrite" {
		t.Errorf("got %q", got)
	}
	long, _ := json.Marshal(map[string]string{"file_path": strings.Repeat("x", 100)})
	if got := toolSummary("Edit", long); len([]rune(got)) > 70 {
		t.Errorf("got %q", got)
	}
	_ = strconv.Itoa
}
