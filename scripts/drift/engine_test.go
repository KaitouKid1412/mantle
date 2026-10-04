package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A fake engine that speaks just enough stream-json: it answers initialize, emits
// system/init after the first user message, and records its argv and environment.
const fakeEngine = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_DIR/argv"
env > "$FAKE_DIR/env"
pwd > "$FAKE_DIR/cwd"
while IFS= read -r line; do
  case "$line" in
  *'"subtype":"initialize"'*)
    echo '{"type":"system","subtype":"status"}'
    echo '{"type":"control_response","response":{"subtype":"success","request_id":"other","response":{}}}'
    echo '{"type":"control_response","response":{"subtype":"success","request_id":"drift_init","response":{"commands":[{"name":"compact","aliases":[]},{"name":"clear","aliases":["reset","new"]},{"name":"frob"}],"models":[{"value":"default"},{"value":"opus"}],"available_output_styles":["default","Explanatory"]}}}'
    ;;
  *'"type":"user"'*)
    echo 'not json'
    echo '{"type":"system","subtype":"init","tools":["Bash","Read","Zap","mcp__gh__create"]}'
    echo '{"type":"result","subtype":"success","is_error":true}'
    ;;
  *end_session*) exit 0 ;;
  esac
done
`

func writeFakeEngine(t *testing.T, script string) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	bin = filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DIR", dir)
	return bin, dir
}

func TestEngineSession(t *testing.T) {
	bin, dir := writeFakeEngine(t, fakeEngine)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "secret")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("CLAUDECODE", "1")
	c := &Collector{Claude: bin, EngineTimeout: 20 * time.Second}
	rep, err := c.engineSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := func(items []Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Name)
		}
		return out
	}
	if got := names(rep.Commands); !slices.Equal(got, []string{"compact", "clear", "frob"}) || rep.Commands[1].Attrs["aliases"] != "reset,new" {
		t.Errorf("commands %+v", rep.Commands)
	}
	if got := names(rep.Tools); !slices.Equal(got, []string{"Bash", "Read", "Zap", "mcp__gh__create"}) {
		t.Errorf("tools %q", got)
	}
	if !slices.Equal(names(rep.Models), []string{"default", "opus"}) || !slices.Equal(names(rep.OutputStyles), []string{"default", "Explanatory"}) {
		t.Errorf("models %+v styles %+v", rep.Models, rep.OutputStyles)
	}

	argv, _ := os.ReadFile(filepath.Join(dir, "argv"))
	if strings.Fields(string(argv))[0] != "--output-format" || strings.Contains(string(argv), "-p\n") {
		t.Errorf("argv %q", argv)
	}
	env, _ := os.ReadFile(filepath.Join(dir, "env"))
	for _, bad := range []string{"ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "CLAUDECODE="} {
		if strings.Contains(string(env), bad) {
			t.Errorf("%s reached the engine", bad)
		}
	}
	for _, want := range []string{"CLAUDE_CONFIG_DIR=", "ANTHROPIC_BASE_URL=http://127.0.0.1:", "CLAUDE_CODE_MAX_RETRIES=0"} {
		if !strings.Contains(string(env), want) {
			t.Errorf("%s missing", want)
		}
	}
	cwd, _ := os.ReadFile(filepath.Join(dir, "cwd"))
	if !strings.Contains(string(cwd), "mantle-drift-") {
		t.Errorf("engine ran in %q, not an empty temp dir", cwd)
	}
}

func TestEngineSessionFailures(t *testing.T) {
	bin, _ := writeFakeEngine(t, "#!/bin/sh\nexit 3\n")
	c := &Collector{Claude: bin, EngineTimeout: 10 * time.Second}
	if _, err := c.engineSession(context.Background()); err == nil {
		t.Error("want an error when the engine exits")
	}

	bin, _ = writeFakeEngine(t, "#!/bin/sh\nsleep 30\n")
	c = &Collector{Claude: bin, EngineTimeout: 300 * time.Millisecond}
	start := time.Now()
	if _, err := c.engineSession(context.Background()); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s", time.Since(start))
	}

	lists := (&Collector{Claude: bin, NoEngine: true}).collectEngine(context.Background())
	if len(lists) != 4 || lists[0].Available() {
		t.Errorf("no-engine lists %+v", lists)
	}
}

func TestCatalog(t *testing.T) {
	c, err := parseCatalog([]byte(`[
		{"kind":"command","id":"cmd.resume"},{"kind":"command","id":"cmd.gone","removed":true},
		{"kind":"renderer","id":"render.tool.Bash"},{"kind":"renderer","id":"render.tool.mcp.*"},
		{"kind":"renderer","id":"render.tool.*"},{"kind":"renderer","id":"render.default"},
		{"kind":"feature","id":"x"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Commands["resume"] || c.Commands["gone"] {
		t.Errorf("commands %v", c.Commands)
	}
	for tool, want := range map[string]bool{"Bash": true, "mcp__gh__create": true, "Read": false, "Zap": false} {
		if got := c.HasToolRenderer(tool); got != want {
			t.Errorf("HasToolRenderer(%s) = %v", tool, got)
		}
	}
	if _, err := parseCatalog([]byte("not json")); err == nil {
		t.Error("want an error")
	}
	if c, err := loadCatalog(context.Background(), "none"); c != nil || err != nil {
		t.Errorf("none: %v %v", c, err)
	}
}

func TestCompareEngineLists(t *testing.T) {
	cmds := List{Kind: KindSlash, Items: []Item{
		{Name: "compact"}, {Name: "clear", Attrs: map[string]string{"aliases": "reset,new"}}, {Name: "resume"}, {Name: "frob"}, {Name: "kept"},
	}}
	tools := List{Kind: KindTool, Items: []Item{{Name: "Bash"}, {Name: "Read"}, {Name: "Zap"}, {Name: "mcp__gh__create"}}}
	base := Snapshot{Lists: []List{
		{Kind: KindSlash, Items: []Item{{Name: "kept"}, {Name: "dropped"}}},
		{Kind: KindTool, Items: []Item{{Name: "Read"}}},
	}}
	k := Known{
		Parity:   ParityIndex{Slash: map[string]bool{"compact": true, "new": true}},
		Baseline: base,
		Catalog:  &Catalog{Commands: map[string]bool{"resume": true}, Renderers: map[string]bool{"tool.Bash": true, "tool.mcp.*": true}},
	}
	got := map[string]string{}
	for _, f := range append(compareSlash(cmds, k), compareTools(tools, k)...) {
		got[f.Kind+" "+f.Name] = f.Status
	}
	want := map[string]string{
		"slash-command frob": StatusUnclassified, "slash-command dropped": StatusGone, "tool Zap": StatusUnclassified,
	}
	if len(got) != len(want) {
		t.Errorf("findings %v", got)
	}
	for key, st := range want {
		if got[key] != st {
			t.Errorf("%s = %q, want %q", key, got[key], st)
		}
	}

	k.Catalog = nil
	fs := compareTools(tools, k)
	if len(fs) != 3 || !strings.Contains(fs[0].Detail, "catalog unavailable") {
		t.Errorf("without catalog %+v", fs)
	}
}

func TestParseSDKDiff(t *testing.T) {
	items, err := parseSDKDiff([]byte(`["system/foo","control/bar"]`))
	if err != nil || len(items) != 2 || items[0].Name != "system/foo" {
		t.Errorf("names: %+v %v", items, err)
	}
	items, err = parseSDKDiff([]byte(`[{"name":"set_cwd","kind":"control"},{"kind":"x"}]`))
	if err != nil || len(items) != 1 || items[0].Scope != "control" {
		t.Errorf("objects: %+v %v", items, err)
	}
	if _, err := parseSDKDiff([]byte(`{"x":1}`)); err == nil {
		t.Error("want an error")
	}
}
