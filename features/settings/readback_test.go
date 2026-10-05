package settings

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/features/settings/options"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/perms"
	"github.com/KaitouKid1412/mantle/features/settings/themes"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
)

// TestReadBack is B11: every way mantle writes Claude Code settings must produce
// values the real engine reads back. Each write path runs against temp settings
// files, then the real claude (talking to an in-process fake API with an isolated
// CLAUDE_CONFIG_DIR, so no network and no tokens) answers initialize and
// get_settings. A key the engine's strict schema rejects would make it ignore the
// whole file, so every value is compared per source.
func TestReadBack(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real claude binary")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not on PATH")
	}
	home := t.TempDir()
	cfg := filepath.Join(home, ".claude")
	work, _ := filepath.EvalSymlinks(t.TempDir())
	if err := fakeapi.SeedConfig(cfg, fakeapi.FakeAPIKey, work); err != nil {
		t.Fatal(err)
	}
	seeded, _ := os.ReadFile(filepath.Join(cfg, ".claude.json"))
	env := patch.Env{Home: home, ConfigDir: cfg, ProjectRoot: work, Managed: filepath.Join(home, "none.json")}

	write := func(label string, p patch.Patch) {
		t.Helper()
		if _, err := writeFile(env, p); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}

	// 1. /config: every option written through the config writer, set to a value
	// other than its default.
	for _, o := range options.Table {
		if o.Write != options.WriteConfigWriter {
			continue
		}
		effects, err := options.Plan(o, readbackValue(o), true)
		if err != nil {
			t.Fatalf("%s: %v", o.Key, err)
		}
		for _, e := range effects {
			if e.Patch != nil {
				write("/config "+o.Key, *e.Patch)
			}
		}
	}
	// 2. /model with a per-model effort.
	rows := model.Rows(sampleModels(), nil, model.Current{}, model.EffortInputs{})
	opus, _ := model.FindRow(rows, "opus")
	for _, e := range model.Plan(model.Choice{Row: opus, Effort: model.XHigh}) {
		if e.Patch != nil {
			write("/model", *e.Patch)
		}
	}
	// 3. /theme confirm with the syntax toggle.
	pk := themes.NewPicker(themes.List(filepath.Join(cfg, "themes")), "dark", false)
	pk.Move(1)
	pk.ToggleSyntax()
	_, themeEffects := pk.Confirm()
	for _, e := range themeEffects {
		write("/theme", *e.Patch)
	}
	// 4. /permissions edits in every writable scope.
	for _, f := range []func() (patch.Patch, error){
		func() (patch.Patch, error) { return perms.AddRule(patch.Local, perms.Allow, "Bash(make test)") },
		func() (patch.Patch, error) { return perms.AddRule(patch.Project, perms.Deny, "Read(./secrets/**)") },
		func() (patch.Patch, error) {
			return perms.AddRule(patch.User, perms.Ask, "WebFetch(domain:example.com)")
		},
		func() (patch.Patch, error) { return perms.AddDirectory(patch.User, filepath.Join(home, "shared")) },
		func() (patch.Patch, error) { return perms.SetDefaultMode(patch.Local, "plan") },
		func() (patch.Patch, error) {
			return perms.AddAutoModeRule(patch.User, perms.AutoAllow, "Running the test suite")
		},
	} {
		p, err := f()
		if err != nil {
			t.Fatal(err)
		}
		write("/permissions", p)
	}
	// 5. Small commands and toggles that write files.
	write("/sandbox", patch.Patch{Scope: patch.Local, Ops: []patch.Op{
		{Kind: patch.Set, Path: []string{"sandbox", "enabled"}, Value: true},
		{Kind: patch.Set, Path: []string{"sandbox", "autoAllowBashIfSandboxed"}, Value: false}}})
	write("/effort ultracode", patch.SetKey(patch.User, true, "ultracode"))
	write("/fast", patch.SetKey(patch.User, true, "fastMode"))
	write("/tui", patch.SetKey(patch.User, "fullscreen", "tui"))

	// Expected per-source contents: exactly what is now in each file.
	want := map[string]map[string]any{}
	for scope, path := range map[string]string{
		"userSettings":    filepath.Join(cfg, "settings.json"),
		"projectSettings": filepath.Join(work, ".claude", "settings.json"),
		"localSettings":   filepath.Join(work, ".claude", "settings.local.json"),
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		want[scope] = doc
	}

	// 6. The engine itself, through the write paths that are control requests.
	srv := httptest.NewServer(fakeapi.New(&fakeapi.Script{}))
	defer srv.Close()
	c := startEngine(t, bin, srv.URL, cfg, work)
	c.control("initialize", nil)
	c.control("update_settings", map[string]any{"source": "localSettings", "settings": map[string]any{"outputStyle": "Explanatory"}})
	c.control("update_settings", map[string]any{"source": "userSettings", "settings": map[string]any{"effortLevel": "medium"}})
	c.control("apply_flag_settings", map[string]any{"settings": map[string]any{"fastMode": true, "alwaysThinkingEnabled": false}})
	want["localSettings"]["outputStyle"] = "Explanatory"
	// update_settings stores effortLevel on the session model's modelSettings entry
	// (verified against 2.1.288), replacing the xhigh the picker saved.
	want["userSettings"]["modelSettings"].(map[string]any)["claude-opus-5-5"].(map[string]any)["effortLevel"] = "medium"
	want["flagSettings"] = map[string]any{"fastMode": true, "alwaysThinkingEnabled": false}

	resp := c.control("get_settings", nil)
	var got struct {
		Effective map[string]any `json:"effective"`
		Sources   []struct {
			Source   string         `json:"source"`
			Settings map[string]any `json:"settings"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatalf("get_settings: %v\n%s", err, resp)
	}
	sources := map[string]map[string]any{}
	for _, s := range got.Sources {
		sources[s.Source] = s.Settings
	}
	for _, scope := range []string{"userSettings", "projectSettings", "localSettings", "flagSettings"} {
		for _, l := range leaves(want[scope], nil) {
			v, ok := patch.Get(sources[scope], l.path...)
			if !ok || !sameSetting(l.path, v, l.value) {
				t.Errorf("%s %s: engine read %v (present=%v), mantle wrote %v", scope, strings.Join(l.path, "."), v, ok, l.value)
			}
		}
	}
	// A few effective values, where precedence decides: local beats user, flag beats all.
	for path, wantV := range map[string]any{
		"permissions.defaultMode": "plan",
		"outputStyle":             "Explanatory",
		"model":                   "opus",
		"fastMode":                true,
		"alwaysThinkingEnabled":   false,
	} {
		v, ok := patch.Get(got.Effective, strings.Split(path, ".")...)
		if !ok || !sameSetting(strings.Split(path, "."), v, wantV) {
			t.Errorf("effective %s = %v (present=%v), want %v", path, v, ok, wantV)
		}
	}
	if t.Failed() {
		b, _ := os.ReadFile(filepath.Join(cfg, "settings.json"))
		t.Logf("user settings after the run:\n%s", b)
	}

	// 7. Global-config keys: mantle sends "/config <row>=<value>" and the engine
	// writes its own ~/.claude.json. Rows the panel shows only in some situations
	// (fullscreen, an IDE) may be refused headless; they are logged, not required.
	replies := map[string]string{}
	for _, o := range options.Table {
		if o.Store == options.StoreGlobal {
			replies[o.Key] = c.prompt(options.EngineCommand(o, readbackValue(o)))
		}
	}
	c.stop()
	b, _ := os.ReadFile(filepath.Join(cfg, ".claude.json"))
	var global map[string]any
	_ = json.Unmarshal(b, &global)
	for _, o := range options.Table {
		if o.Store != options.StoreGlobal {
			continue
		}
		v, res := readbackValue(o), replies[o.Key]
		got, ok := patch.Get(global, o.Path()...)
		switch {
		case ok && patch.Equal(got, v):
		case o.When != options.Always:
			t.Logf("/config %s (shown only in some situations) was not applied headless: %q", o.Row, res)
		default:
			t.Errorf("/config %s=%v: global config has %v (present=%v); engine said %q", o.Row, options.Format(v), got, ok, res)
		}
	}
	// mantle never touched the global config.
	if after, _ := os.ReadFile(filepath.Join(cfg, ".claude.json")); len(after) == 0 || !strings.Contains(string(after), `"hasCompletedOnboarding"`) {
		t.Errorf("global config changed or lost: %q (seeded %d bytes)", after, len(seeded))
	}
}

// sameSetting compares a value the engine read with the one mantle wrote. The engine
// migrates a bare model alias to its long-context variant on startup ("opus" becomes
// "opus[1m]" in 2.1.288), so for "model" the "[1m]" suffix is ignored.
func sameSetting(path []string, got, wrote any) bool {
	if len(path) == 1 && path[0] == "model" {
		g, _ := got.(string)
		w, _ := wrote.(string)
		return strings.TrimSuffix(g, "[1m]") == strings.TrimSuffix(w, "[1m]")
	}
	return patch.Equal(got, wrote)
}

// readbackValue picks a non-default value for an option.
func readbackValue(o options.Option) any {
	switch o.Type {
	case options.Bool:
		return !o.Default.(bool)
	case options.Enum:
		for _, v := range o.Values {
			if v != o.Default {
				return v
			}
		}
	}
	return "Japanese"
}

type leaf struct {
	path  []string
	value any
}

// leaves lists every non-object value in doc with its path (arrays are values).
func leaves(doc map[string]any, prefix []string) []leaf {
	var out []leaf
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := append(append([]string(nil), prefix...), k)
		if m, ok := doc[k].(map[string]any); ok {
			out = append(out, leaves(m, p)...)
			continue
		}
		out = append(out, leaf{p, doc[k]})
	}
	return out
}

// engine is a minimal stream-json client for the read-back test.
type engine struct {
	t     *testing.T
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan map[string]any
	errb  *strings.Builder
	n     int
	once  sync.Once
}

// stop closes stdin and waits for claude to exit (it flushes its global config then).
func (e *engine) stop() {
	e.once.Do(func() {
		_ = e.stdin.Close()
		done := make(chan struct{})
		go func() { _ = e.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = e.cmd.Process.Kill()
		}
	})
}

func startEngine(t *testing.T, bin, apiURL, cfg, work string) *engine {
	t.Helper()
	cmd := exec.Command(bin, "--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--permission-prompt-tool", "stdio")
	cmd.Dir = work
	cmd.Env = fakeapi.Env(os.Environ(), apiURL, cfg, fakeapi.FakeAPIKey)
	e := &engine{t: t, cmd: cmd, lines: make(chan map[string]any, 1024), errb: &strings.Builder{}}
	cmd.Stderr = e.errb
	e.stdin, _ = cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.stop)
	go func() {
		defer close(e.lines)
		br := bufio.NewReaderSize(out, 1<<20)
		for {
			l, err := br.ReadBytes('\n')
			var m map[string]any
			if len(l) > 0 && l[0] == '{' && json.Unmarshal(l, &m) == nil {
				e.lines <- m
			}
			if err != nil {
				return
			}
		}
	}()
	return e
}

// prompt sends a user message (a slash command here) and returns the result text.
func (e *engine) prompt(text string) string {
	e.t.Helper()
	e.n++
	b, _ := json.Marshal(map[string]any{"type": "user", "session_id": "", "parent_tool_use_id": nil,
		"uuid":    fmt.Sprintf("00000000-0000-4000-8000-%012d", e.n),
		"message": map[string]any{"role": "user", "content": text}, "origin": map[string]any{"kind": "human"}})
	if _, err := e.stdin.Write(append(b, '\n')); err != nil {
		e.t.Fatal(err)
	}
	timeout := time.After(60 * time.Second)
	for {
		select {
		case m, ok := <-e.lines:
			if !ok {
				e.t.Fatalf("claude exited during %q; stderr:\n%s", text, e.errb.String())
			}
			if m["type"] == "result" {
				s, _ := m["result"].(string)
				return s
			}
		case <-timeout:
			e.t.Fatalf("timeout waiting for %q; stderr:\n%s", text, e.errb.String())
		}
	}
}

// control sends a control request and returns its success payload.
func (e *engine) control(subtype string, fields map[string]any) json.RawMessage {
	e.t.Helper()
	e.n++
	id := fmt.Sprintf("readback-%d", e.n)
	req := map[string]any{"subtype": subtype}
	for k, v := range fields {
		req[k] = v
	}
	b, _ := json.Marshal(map[string]any{"type": "control_request", "request_id": id, "request": req})
	if _, err := e.stdin.Write(append(b, '\n')); err != nil {
		e.t.Fatal(err)
	}
	timeout := time.After(60 * time.Second)
	for {
		select {
		case m, ok := <-e.lines:
			if !ok {
				e.t.Fatalf("claude exited during %s; stderr:\n%s", subtype, e.errb.String())
			}
			r, _ := m["response"].(map[string]any)
			if m["type"] != "control_response" || r["request_id"] != id {
				continue
			}
			if r["subtype"] != "success" {
				e.t.Fatalf("%s failed: %v", subtype, r["error"])
			}
			raw, _ := json.Marshal(r["response"])
			return raw
		case <-timeout:
			e.t.Fatalf("timeout waiting for %s; stderr:\n%s", subtype, e.errb.String())
		}
	}
}
