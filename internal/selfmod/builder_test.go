package selfmod

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBuilderSpawnOpts(t *testing.T) {
	o := BuilderSpawnOpts(BuilderOptions{RequestID: "demo", Worktree: "/w/demo", RulesFile: "/b/rules.md", Model: "opus", MaxBudgetUSD: 2.5})
	if o.Cwd != "/w/demo" || o.PermissionMode != "acceptEdits" || o.Model != "opus" {
		t.Errorf("opts = %+v", o)
	}
	args := o.ExtraArgs
	idx := func(flag string) int { return slices.Index(args, flag) }
	if idx("--allowedTools") != 0 || idx("--disallowedTools") <= 0 {
		t.Fatalf("args = %q", args)
	}
	allowed := args[1:idx("--disallowedTools")]
	for _, want := range []string{"Bash(go test:*)", "Bash(gofmt:*)", "Bash(./bin/mantle-ui story:*)", "Bash(git status)"} {
		if !slices.Contains(allowed, want) {
			t.Errorf("allowed tools missing %q", want)
		}
	}
	for _, a := range allowed {
		if strings.HasPrefix(a, "--") {
			t.Errorf("flag inside the allowed list: %q", a)
		}
	}
	disallowed := args[idx("--disallowedTools")+1 : idx("--append-system-prompt-file")]
	for _, want := range []string{"Bash(git commit:*)", "Bash(go get:*)", "Edit(internal/launcher/**)", "Write(cmd/mantle/**)", "Edit(.mcp.json)"} {
		if !slices.Contains(disallowed, want) {
			t.Errorf("disallowed tools missing %q: %q", want, disallowed)
		}
	}
	if args[idx("--append-system-prompt-file")+1] != "/b/rules.md" || args[idx("--max-budget-usd")+1] != "2.5" {
		t.Errorf("args = %q", args)
	}
	if slices.Contains(args, "--system-prompt") {
		t.Error("the builder must keep the default system prompt")
	}
	if d := BuilderSpawnOpts(BuilderOptions{}); d.ExtraArgs[len(d.ExtraArgs)-1] != "5" || slices.Contains(d.ExtraArgs, "--append-system-prompt-file") {
		t.Errorf("defaults = %q", d.ExtraArgs)
	}
	if BuilderEngineID("demo") != "builder-demo" {
		t.Error("engine id")
	}
}

func TestWriteRules(t *testing.T) {
	p, err := WriteRules(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	for _, want := range []string{ProposalStart, ProposalEnd, "Never commit", "mods/<mod-id>/", "internal/launcher/", "go get"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rules missing %q", want)
		}
	}
}

func TestPrompts(t *testing.T) {
	r := &Request{ID: "spinner-blue", Request: "make the spinner blue", Kind: RequestMod, Round: 2}
	p := BuildPrompt(r)
	if !strings.Contains(p, `"spinner-blue"`) || !strings.Contains(p, "make the spinner blue") || !strings.Contains(p, "mods/spinner-blue/") {
		t.Errorf("prompt:\n%s", p)
	}
	rp := RetryPrompt(r, "## step vet failed\n  x.go:1: bad\n")
	if !strings.Contains(rp, "Round 2 of 3") || !strings.Contains(rp, "x.go:1: bad") {
		t.Errorf("retry prompt:\n%s", rp)
	}
	cp := ConflictPrompt("demo", "add demo", []string{"a.go", "b.go"})
	if !strings.Contains(cp, "a.go\n  b.go") || !strings.Contains(cp, "add demo") || !strings.Contains(cp, "Do not run git add") {
		t.Errorf("conflict prompt:\n%s", cp)
	}
}

func TestParseConfigProposal(t *testing.T) {
	reply := "This is a settings change.\n\nMANTLE_CONFIG_PROPOSAL\n```json\n{\"summary\": \"Blue spinner\", \"scope\": \"mantle\", \"key\": \"spinner.color\", \"value\": \"blue\"}\n```\nEND_MANTLE_CONFIG_PROPOSAL\n"
	p, ok, err := ParseConfigProposal(reply)
	if err != nil || !ok || p.Scope != "mantle" || p.Key != "spinner.color" || p.Summary != "Blue spinner" {
		t.Fatalf("p=%+v ok=%v err=%v", p, ok, err)
	}
	if v, err := p.DecodedValue(); err != nil || v != "blue" {
		t.Errorf("value = %v, %v", v, err)
	}
	if _, ok, _ := ParseConfigProposal("I changed mods/x/x.go. Done."); ok {
		t.Error("found a proposal in a normal reply")
	}
	if _, ok, _ := ParseConfigProposal("see END_MANTLE_CONFIG_PROPOSAL only"); ok {
		t.Error("the end marker alone is not a proposal")
	}
	if _, ok, err := ParseConfigProposal("MANTLE_CONFIG_PROPOSAL\n{not json}\nEND_MANTLE_CONFIG_PROPOSAL"); !ok || err == nil {
		t.Errorf("malformed: ok=%v err=%v", ok, err)
	}
	if _, _, err := ParseConfigProposal(`MANTLE_CONFIG_PROPOSAL {"key": "x", "value": 1, "scope": "global"} END_MANTLE_CONFIG_PROPOSAL`); err == nil {
		t.Error("unknown scope accepted")
	}
	p, _, err = ParseConfigProposal(`MANTLE_CONFIG_PROPOSAL {"key": "spinnerVerbs", "value": {"mode": "append", "verbs": ["Brewing"]}} END_MANTLE_CONFIG_PROPOSAL`)
	if err != nil || p.Scope != "claude" {
		t.Errorf("default scope: %+v %v", p, err)
	}
}
