package main

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixture = "../../testdata/fixtures/11/drift/snapshot-2.1.288.json"

// A commander-style help text, written for this test.
const sampleHelp = `Usage: tool [options] [command] [prompt]

Does things to files.

Arguments:
  prompt                         what to do

Options:
  -q, --quiet                    stay quiet
  --level <n>                    how much
  --tags <tags...>               labels to add
  --color [when]                 colourise output
  --colours, --colors <list...>
      a long spec sits on its own line, with the description below
  -h, --help                     display help for command

Commands:
  build [options]                make it
  remove|rm <id>                 delete one
                                 (continued description)
  Examples:
  help [command]                 display help for command
`

func TestParseHelp(t *testing.T) {
	flags, subs := parseHelp(sampleHelp, "")
	got := map[string]Item{}
	for _, f := range flags {
		got[f.Name] = f
	}
	want := map[string]map[string]string{
		"--quiet":   {"arity": "none", "short": "-q"},
		"--level":   {"arity": "required"},
		"--tags":    {"arity": "variadic"},
		"--color":   {"arity": "optional"},
		"--colours": {"arity": "variadic", "aliases": "--colors"},
		"--help":    {"arity": "none", "short": "-h"},
	}
	if len(got) != len(want) {
		t.Errorf("flags %v", flags)
	}
	for name, attrs := range want {
		it, ok := got[name]
		if !ok {
			t.Errorf("%s missing", name)
			continue
		}
		for k, v := range attrs {
			if it.Attrs[k] != v {
				t.Errorf("%s %s = %q, want %q", name, k, it.Attrs[k], v)
			}
		}
	}
	if len(subs) != 2 || subs[0].Name != "build" || subs[1].Name != "remove" || subs[1].Attrs["aliases"] != "rm" {
		t.Errorf("subs %v", subs)
	}

	scoped, _ := parseHelp(sampleHelp, "build")
	if scoped[0].Scope != "build" || scoped[0].Key() != "build --quiet" {
		t.Errorf("scope %+v", scoped[0])
	}
}

func TestHelpIsFor(t *testing.T) {
	for _, c := range []struct {
		text, sub string
		want      bool
	}{
		{"Usage: claude mcp [options] [command]\n", "mcp", true},
		{"Usage: claude plugin|plugins [options]\n", "plugin", true},
		{"Usage: claude stop <id>\n", "stop", true},
		{"Usage: claude [options] [command] [prompt]\n", "mcp", false},
		{"Usage: claude mcpx\n", "mcp", false},
		{"", "mcp", false},
	} {
		if got := helpIsFor(c.text, c.sub); got != c.want {
			t.Errorf("helpIsFor(%q, %q) = %v", c.text, c.sub, got)
		}
	}
}

func TestSafeInfoArgs(t *testing.T) {
	for _, ok := range [][]string{{"--help"}, {"--version"}, {"mcp", "--help"}, {"stop", "--help"}} {
		if err := safeInfoArgs(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		nil, {"help"}, {"auth login"}, {"mcp"}, {"mcp", "list"}, {"remote-control", "--help"},
		{"rc", "--help"}, {"kill", "--help"}, {"nonsense", "--help"}, {"--help", "x"}, {"-p", "--help"},
	} {
		if err := safeInfoArgs(bad); err == nil {
			t.Errorf("%q allowed", bad)
		}
	}
}

func TestScanBinary(t *testing.T) {
	data := []byte(`junk;var X=["app:quit","app:redraw","chat:send","chat:undo","pane:grow"];` +
		`var M=["node:fs","node:path","node:os","node:url","node:util"];` +
		`var Y=["messageSelector:up","messageSelector:down","messageSelector:top","messageSelector:bottom","diff:viewDetails"];` +
		`var C=["Global","Chat","Autocomplete","Confirmation","Help","Transcript","Task","Tabs","Footer","Select"];` +
		`var N=["Alpha","Beta","Gamma","Delta","Epsilon","Zeta","Eta","Theta","Iota","Kappa"];` +
		`{context:"Terminal",bindings:{}}` +
		`p.command("mcp");p.command("frobnicate [x]");q.command("list");`)
	subs := List{Items: []Item{{Name: "list", Scope: "mcp"}}}
	actions, contexts, candidates := scanBinary(data, subs)

	names := func(items []Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Name)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	if got := names(actions); !slices.Equal(got, []string{"app:quit", "app:redraw", "chat:send", "chat:undo", "diff:viewDetails",
		"messageSelector:bottom", "messageSelector:down", "messageSelector:top", "messageSelector:up", "pane:grow"}) {
		t.Errorf("actions %q", got)
	}
	if got := names(contexts); !slices.Contains(got, "Terminal") || slices.Contains(got, "Alpha") || len(got) != 11 {
		t.Errorf("contexts %q", got)
	}
	if got := names(candidates); !slices.Equal(got, []string{"frobnicate"}) {
		t.Errorf("candidates %q", got)
	}
}

const sampleSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "definitions": {"sl": {"type": "object", "properties": {"command": {}, "padding": {}}}},
  "properties": {
    "model": {"type": "string"},
    "statusLine": {"$ref": "#/definitions/sl"},
    "permissions": {"type": "object", "properties": {"allow": {}, "deny": {}}}
  }
}`

func TestSchemaKeys(t *testing.T) {
	keys, err := schemaKeys([]byte(sampleSchema))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"model", "permissions", "permissions.allow", "permissions.deny", "statusLine", "statusLine.command", "statusLine.padding"}
	if !slices.Equal(keys, want) {
		t.Errorf("keys %q", keys)
	}
	if _, err := schemaKeys([]byte(`{"type":"object"}`)); err == nil {
		t.Error("want an error for a schema without properties")
	}
}

func TestCollectSettingsCacheAndOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleSchema))
	}))
	defer srv.Close()
	cache := t.TempDir()
	c := &Collector{Cache: cache, HTTP: rewriteClient(srv.URL)}

	l := c.collectSettings(context.Background())
	if l.Error != "" || len(l.Items) != 7 || strings.Contains(l.Source, "cached") {
		t.Fatalf("online: %+v", l)
	}
	c.Offline = true
	l = c.collectSettings(context.Background())
	if l.Error != "" || len(l.Items) != 7 || !strings.Contains(l.Source, "cached") {
		t.Fatalf("offline: %+v", l)
	}
	c.Cache = t.TempDir()
	if l = c.collectSettings(context.Background()); l.Error == "" {
		t.Fatal("offline without a cache should be unavailable")
	}
}

// rewriteClient sends every request to base.
func rewriteClient(base string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		r2 := r.Clone(r.Context())
		u, _ := r.URL.Parse(base)
		r2.URL = u
		return http.DefaultTransport.RoundTrip(r2)
	})}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestCollectWithFakeClaude runs the help collectors against a fake claude that only
// answers --version and --help, and records every argv it was given.
func TestCollectWithFakeClaude(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	log := filepath.Join(dir, "calls.txt")
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
case "$*" in
--version) echo "9.9.9 (Claude Code)";;
--help) printf 'Usage: claude [options] [command] [prompt]\n\nOptions:\n  --model <model>  m\n  --zap  new\n\nCommands:\n  mcp  servers\n  zork  new\n';;
"mcp --help") printf 'Usage: claude mcp [options] [command]\n\nOptions:\n  -h, --help  help\n\nCommands:\n  list  l\n';;
*" --help") printf 'Usage: claude [options] [command] [prompt]\n';;
*) echo "UNSAFE CALL" ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Collector{Claude: bin, Binary: filepath.Join(dir, "nope"), Cache: dir, Offline: true, NoEngine: true}
	s := c.Collect(context.Background())
	if s.ClaudeVersion != "9.9.9" {
		t.Errorf("version %q", s.ClaudeVersion)
	}
	calls, _ := os.ReadFile(log)
	for _, line := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
		f := strings.Fields(line)
		if err := safeInfoArgs(f); err != nil {
			t.Errorf("unsafe call %q: %v", line, err)
		}
		if strings.Contains(line, "remote-control") {
			t.Error("probed remote-control")
		}
	}
	flags := s.List(KindFlag)
	if !slices.ContainsFunc(flags.Items, func(it Item) bool { return it.Name == "--zap" }) ||
		!slices.ContainsFunc(flags.Items, func(it Item) bool { return it.Key() == "mcp --help" }) {
		t.Errorf("flags %+v", flags.Items)
	}
	subs := s.List(KindSubcommand)
	if !slices.ContainsFunc(subs.Items, func(it Item) bool { return it.Key() == "mcp list" }) {
		t.Errorf("subs %+v", subs.Items)
	}
	if s.List(KindAction).Available() || s.List(KindSetting).Available() || s.List(KindTool).Available() {
		t.Error("lists without input should be unavailable")
	}

	r := Compare(s, Known{Flags: cli.Flags, Subcommands: cli.Subcommands})
	var unclassified []string
	for _, f := range r.Findings {
		if f.Status == StatusUnclassified {
			unclassified = append(unclassified, f.Key())
		}
	}
	if !slices.Equal(unclassified, []string{"flag --zap", "subcommand zork"}) {
		t.Errorf("unclassified %q", unclassified)
	}
}

func (f Finding) Key() string {
	if f.Scope != "" {
		return f.Kind + " " + f.Scope + " " + f.Name
	}
	return f.Kind + " " + f.Name
}

// TestBaselineIsClassified: the accepted baseline must be fully classified by mantle's
// flag and subcommand tables, so `make drift` only fails on real changes.
func TestBaselineIsClassified(t *testing.T) {
	b, err := loadSnapshot("baseline.json")
	if err != nil {
		t.Fatal(err)
	}
	r := Compare(b, Known{Flags: cli.Flags, Subcommands: cli.Subcommands, Baseline: b})
	for _, f := range r.Findings {
		if (f.Kind == KindFlag || f.Kind == KindSubcommand) && f.Status != StatusNew {
			t.Errorf("%s %s: %s %s", f.Kind, f.Key(), f.Status, f.Detail)
		}
	}
}

// TestCompareGolden diffs a simulated next claude release (the 2.1.288 fixture with
// changes applied) against tables derived from the fixture.
func TestCompareGolden(t *testing.T) {
	base, err := loadSnapshot(fixture)
	if err != nil {
		t.Fatal(err)
	}
	pf, err := os.Open("testdata/parity.md")
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Close()
	parity, err := parseParity(pf)
	if err != nil {
		t.Fatal(err)
	}
	k := Known{Flags: cli.Flags, Subcommands: cli.Subcommands, Parity: parity, Baseline: base}
	for _, it := range base.List(KindAction).Items {
		k.Actions = append(k.Actions, it.Name)
	}
	for _, it := range base.List(KindContext).Items {
		k.Contexts = append(k.Contexts, it.Name)
	}

	// Unchanged: nothing to report.
	if r := Compare(base, k); len(r.Findings) != 0 {
		t.Errorf("unchanged snapshot: %+v", r.Findings)
	}

	next := mutate(base)
	r := Compare(next, k)
	r.Generated = "2026-10-03"
	if !r.Failed() {
		t.Error("want a failing report")
	}
	var md bytes.Buffer
	if err := writeMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	golden(t, "next.golden.md", md.String())
}

// mutate returns a copy of s as a hypothetical next release would change it.
func mutate(s Snapshot) Snapshot {
	out := Snapshot{ClaudeVersion: "2.1.290"}
	for _, l := range s.Lists {
		nl := l
		nl.Items = nil
		for _, it := range l.Items {
			switch {
			case l.Kind == KindFlag && it.Key() == "--brief":
				continue // removed
			case l.Kind == KindFlag && it.Key() == "--effort":
				it.Attrs = map[string]string{"arity": "optional"}
			case l.Kind == KindSetting && it.Name == "model":
				continue // gone
			}
			nl.Items = append(nl.Items, it)
		}
		switch l.Kind {
		case KindFlag:
			nl.Items = append(nl.Items,
				Item{Name: "--frobnicate", Attrs: map[string]string{"arity": "required"}},
				Item{Name: "--zap-level", Scope: "mcp", Attrs: map[string]string{"arity": "required"}})
		case KindSubcommand:
			nl.Items = append(nl.Items, Item{Name: "frob"}, Item{Name: "prune", Scope: "mcp"})
		case KindAction:
			nl.Items = append(nl.Items, Item{Name: "chat:frobnicate"})
		case KindContext:
			nl.Items = append(nl.Items, Item{Name: "Frobber"})
		case KindSetting:
			nl.Items = append(nl.Items, Item{Name: "frobMode"}, Item{Name: "spinnerTipsEnabled"})
		}
		out.Put(nl)
	}
	return out
}

func TestParseParity(t *testing.T) {
	f, err := os.Open("testdata/parity.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := parseParity(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		set  map[string]bool
		name string
	}{
		{p.IDs, "CLI-01"}, {p.Slash, "clear"}, {p.Slash, "reset"}, {p.Slash, "new"},
		{p.Settings, "statusLine"}, {p.Settings, "statusLine.padding"}, {p.Settings, "copyFullResponse"},
		{p.Settings, "diffTool"}, {p.Contexts, "Chat"}, {p.Contexts, "AbovePromptInput"},
		{p.Flags, "-p"}, {p.Flags, "--print"}, {p.Flags, "--input-format"},
	} {
		if !c.set[c.name] {
			t.Errorf("%q missing", c.name)
		}
	}
	if p.Settings["padding"] {
		t.Error("nested key parsed as top-level")
	}
}

func TestRunFromSnapshot(t *testing.T) {
	dir := t.TempDir()
	md, js := filepath.Join(dir, "drift.md"), filepath.Join(dir, "drift.json")
	code := run([]string{"-from", fixture, "-baseline", fixture, "-parity", "testdata/parity.md", "-out", md, "-json", js, "-catalog", "none"})
	if code != 0 && code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, p := range []string{md, js} {
		if b, err := os.ReadFile(p); err != nil || len(b) == 0 {
			t.Errorf("%s: %v", p, err)
		}
	}
	if code := run([]string{"-from", filepath.Join(dir, "missing.json")}); code != 2 {
		t.Errorf("missing snapshot: exit %d", code)
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./scripts/drift -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs; review and run go test ./scripts/drift -update\n--- got\n%s", path, got)
	}
}
