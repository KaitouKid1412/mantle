package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

// renderTable prints one line per flag: names, arity, class and attributes. The golden
// copy makes every change to a flag's treatment show up in review.
func renderTable(table []Flag) string {
	var b strings.Builder
	for _, f := range table {
		names := strings.Join(f.Names(), ",")
		var attrs []string
		if f.EqValue {
			attrs = append(attrs, "eq")
		}
		if f.Hidden {
			attrs = append(attrs, "hidden")
		}
		if f.Mantle {
			attrs = append(attrs, "mantle")
		}
		line := fmt.Sprintf("%-46s %-10s %-10s %-7s %s", names, f.Arity, f.Class, f.Parity, strings.Join(attrs, ","))
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/cli -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs; review and run go test ./internal/cli -update\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}

func TestFlagTableGolden(t *testing.T) {
	golden(t, "flags.golden", renderTable(Flags))
}

func TestFlagTableWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, f := range Flags {
		if !strings.HasPrefix(f.Long, "--") {
			t.Errorf("%s: long name must start with --", f.Long)
		}
		if f.Short != "" && (!strings.HasPrefix(f.Short, "-") || strings.HasPrefix(f.Short, "--")) {
			t.Errorf("%s: bad short name %q", f.Long, f.Short)
		}
		for _, a := range f.Aliases {
			if !strings.HasPrefix(a, "--") {
				t.Errorf("%s: alias %q must start with --", f.Long, a)
			}
		}
		for _, n := range f.Names() {
			if prev, ok := seen[n]; ok {
				t.Errorf("%s is used by both %s and %s", n, prev, f.Long)
			}
			seen[n] = f.Long
		}
		if f.Parity == "" {
			t.Errorf("%s: no PARITY.md row", f.Long)
		}
		if f.EqValue && f.Arity != ArityNone {
			t.Errorf("%s: EqValue only makes sense on a switch", f.Long)
		}
		if f.Mantle && f.Class != Consume {
			t.Errorf("%s: a mantle-only flag must never reach claude", f.Long)
		}
	}
	if Lookup("--allowed-tools") == nil || Lookup("--allowed-tools").Long != "--allowedTools" {
		t.Error("alias lookup broken")
	}
}

// The 2.1.288 flags that `claude --help` lists. Every one must be in the table and not
// marked hidden; every other table entry must be hidden or mantle's own.
var visible2_1_288 = strings.Fields(`
--add-dir --agent --agents --allow-dangerously-skip-permissions --allowedTools
--append-system-prompt --autocompact --ax-screen-reader --bg --bare --betas --brief
--chrome --cloud --continue --dangerously-skip-permissions --debug --debug-file
--desktop --disable-slash-commands --disallowedTools --effort --environment
--exclude-dynamic-system-prompt-sections --fallback-model --file --fork-session
--forward-subagent-text --from-pr --help --ide --include-hook-events
--include-partial-messages --input-format --json-schema --max-budget-usd --mcp-config
--model --name --no-chrome --no-session-persistence --output-format --permission-mode
--permission-prompts --plugin-dir --plugin-url --print --prompt-suggestions
--remote-control --remote-control-session-name-prefix --replay-user-messages
--restricted --resume --safe-mode --session-id --setting-sources --settings
--strict-mcp-config --system-prompt --system-prompt-snapshot --teleport --tmux --tools
--verbose --version --worktree`)

func TestVisibleFlags(t *testing.T) {
	if len(visible2_1_288) != 66 {
		t.Fatalf("visible list has %d flags, want 66", len(visible2_1_288))
	}
	for _, n := range visible2_1_288 {
		f := Lookup(n)
		if f == nil {
			t.Errorf("%s missing from the table", n)
			continue
		}
		if f.Hidden || f.Mantle {
			t.Errorf("%s is listed by claude --help but marked hidden/mantle", n)
		}
	}
	for _, f := range Flags {
		if !f.Hidden && !f.Mantle && !slices.Contains(visible2_1_288, f.Long) {
			t.Errorf("%s is neither in claude --help nor marked hidden", f.Long)
		}
	}
}

// sampleValues returns argument tokens that satisfy a flag's arity.
func sampleValues(f *Flag) []string {
	switch f.Long {
	case "--prompt-suggestions":
		return []string{"false"}
	case "--prefill-b64":
		return []string{"aGk="}
	}
	switch f.Arity {
	case ArityRequired, ArityRepeatable, ArityOptional:
		return []string{"VAL"}
	case ArityVariadic:
		return []string{"V1", "V2"}
	}
	return nil
}

// TestEveryFlag runs each spelling of every flag through Parse, followed by a forwarded
// switch and a prompt, and checks what its class promises.
func TestEveryFlag(t *testing.T) {
	for i := range Flags {
		f := &Flags[i]
		for _, name := range f.Names() {
			use := append([]string{name}, sampleValues(f)...)
			argv := append(slices.Clone(use), "--verbose", "the prompt")
			t.Run(name, func(t *testing.T) {
				p, err := Parse(argv)
				switch {
				case f.Class == Exec:
					if err != nil || p.Mode != ModeExecClaude || !slices.Equal(p.Exec, argv) {
						t.Fatalf("want exec with argv unchanged, got mode %v exec %q err %v", p.Mode, p.Exec, err)
					}
				case f.Class == PrintOnly:
					if err == nil || !strings.Contains(err.Error(), name) {
						t.Fatalf("want a print-only error naming %s, got %v", name, err)
					}
				case f.Long == "--version":
					if err != nil || p.Mode != ModeVersion {
						t.Fatalf("want version mode, got %v %v", p.Mode, err)
					}
				case f.Long == "--help":
					if err != nil || p.Mode != ModeHelp {
						t.Fatalf("want help mode, got %v %v", p.Mode, err)
					}
				default:
					if err != nil || p.Mode != ModeUI {
						t.Fatalf("want ui mode, got %v %v", p.Mode, err)
					}
					if p.Prompt != "the prompt" {
						t.Errorf("prompt = %q", p.Prompt)
					}
					want := []string{"--verbose"}
					if f.Class == Forward || f.Class == Warn {
						want = append(slices.Clone(use), "--verbose")
					}
					if !slices.Equal(p.EngineArgs, want) {
						t.Errorf("engine args = %q, want %q", p.EngineArgs, want)
					}
					if f.Class == Warn && len(p.Warnings) != 1 {
						t.Errorf("want one warning, got %q", p.Warnings)
					}
					if f.Class != Warn && len(p.Warnings) != 0 {
						t.Errorf("unexpected warnings %q", p.Warnings)
					}
					if len(p.Unknown) != 0 {
						t.Errorf("unknown = %q", p.Unknown)
					}
				}
			})
		}
	}
}

// TestInteractiveOnlyDecisions pins plan 11's B2 decisions, one row each.
func TestInteractiveOnlyDecisions(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		mode Mode
		why  string
	}{
		{[]string{"-w"}, ModeUI, "worktree: the engine creates it and runs there"},
		{[]string{"-w", "feature", "--tmux"}, ModeExecClaude, "tmux/iTerm2 panes are claude's own UI"},
		{[]string{"--bg", "do it"}, ModeExecClaude, "background sessions are claude's; watch with claude agents"},
		{[]string{"--remote-control"}, ModeExecClaude, "Remote Control is a hand-off (H)"},
		{[]string{"--teleport", "abc"}, ModeExecClaude, "teleport is a hand-off (H)"},
		{[]string{"--cloud", "fix it"}, ModeExecClaude, "cloud sessions are a hand-off (H)"},
		{[]string{"--environment", "env_1"}, ModeExecClaude, "cloud sessions are a hand-off (H)"},
		{[]string{"--desktop"}, ModeExecClaude, "Claude Desktop is a hand-off (H)"},
		{[]string{"--from-pr", "123"}, ModeExecClaude, "PR-linked picker is claude's until plan 06 supports it"},
	} {
		p, err := Parse(tc.argv)
		if err != nil || p.Mode != tc.mode {
			t.Errorf("%q: mode %v err %v, want %v (%s)", tc.argv, p.Mode, err, tc.mode, tc.why)
		}
	}
}
