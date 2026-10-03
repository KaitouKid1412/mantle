package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	type tc struct {
		name   string
		argv   []string
		mode   Mode
		engine []string // nil means none
		prompt string
		err    string // substring; "" means no error
		warn   string // substring of a warning; "" means no warnings
		check  func(t *testing.T, p Parsed)
	}
	sw := []string{}
	cases := []tc{
		{name: "empty", argv: nil, mode: ModeUI},
		{name: "prompt", argv: []string{"fix the bug"}, mode: ModeUI, prompt: "fix the bug"},
		{name: "required value", argv: []string{"--model", "sonnet", "hi"}, mode: ModeUI, engine: []string{"--model", "sonnet"}, prompt: "hi",
			check: func(t *testing.T, p Parsed) { eq(t, p.Mantle.Model, "sonnet") }},
		{name: "equals form", argv: []string{"--model=sonnet", "hi"}, mode: ModeUI, engine: []string{"--model=sonnet"}, prompt: "hi",
			check: func(t *testing.T, p Parsed) { eq(t, p.Mantle.Model, "sonnet") }},
		{name: "required takes a dash value", argv: []string{"--model", "--verbose"}, mode: ModeUI, engine: []string{"--model", "--verbose"},
			check: func(t *testing.T, p Parsed) {
				eq(t, p.Mantle.Model, "--verbose")
				if p.Mantle.Verbose {
					t.Error("--verbose was a value, not a flag")
				}
			}},
		{name: "missing value", argv: []string{"--model"}, err: "--model needs a value"},
		{name: "repeated flag", argv: []string{"--model", "a", "--model", "b"}, mode: ModeUI, engine: []string{"--model", "a", "--model", "b"},
			check: func(t *testing.T, p Parsed) { eq(t, p.Mantle.Model, "b") }},

		// Optional values.
		{name: "resume picker", argv: []string{"-r"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { resume(t, p, true, "") }},
		{name: "resume id", argv: []string{"-r", "abc", "hi"}, mode: ModeUI, prompt: "hi",
			check: func(t *testing.T, p Parsed) { resume(t, p, true, "abc") }},
		{name: "resume before a flag", argv: []string{"-r", "--model", "x"}, mode: ModeUI, engine: []string{"--model", "x"},
			check: func(t *testing.T, p Parsed) { resume(t, p, true, "") }},
		{name: "resume attached", argv: []string{"-rabc"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { resume(t, p, true, "abc") }},
		{name: "resume equals", argv: []string{"--resume=my search"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { resume(t, p, true, "my search") }},
		{name: "debug filter", argv: []string{"-d", "api,hooks", "hi"}, mode: ModeUI, engine: []string{"-d", "api,hooks"}, prompt: "hi"},
		{name: "debug without filter", argv: []string{"-d", "-c"}, mode: ModeUI, engine: []string{"-d"},
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Continue || !p.Mantle.Debug {
					t.Error("want continue and debug")
				}
			}},
		{name: "worktree name", argv: []string{"-w", "feat"}, mode: ModeUI, engine: []string{"-w", "feat"},
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Worktree || p.Mantle.WorktreeName != "feat" {
					t.Errorf("worktree %v %q", p.Mantle.Worktree, p.Mantle.WorktreeName)
				}
			}},
		{name: "prompt suggestions preset", argv: []string{"--prompt-suggestions"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { boolPtr(t, p.Mantle.PromptSuggestions, true) }},
		{name: "prompt suggestions off", argv: []string{"--prompt-suggestions", "off"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { boolPtr(t, p.Mantle.PromptSuggestions, false) }},
		{name: "prompt suggestions bad", argv: []string{"--prompt-suggestions", "maybe"}, err: "not a boolean"},

		// Short clusters.
		{name: "cluster of switches", argv: []string{"-cd"}, mode: ModeUI, engine: []string{"-d"},
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Continue {
					t.Error("want continue")
				}
			}},
		{name: "cluster value", argv: []string{"-dc"}, mode: ModeUI, engine: []string{"-dc"},
			check: func(t *testing.T, p Parsed) {
				if p.Mantle.Continue {
					t.Error("c is -d's filter, not -c")
				}
			}},
		{name: "cluster with print", argv: []string{"-cp", "hi"}, mode: ModeExecClaude},
		{name: "name attached", argv: []string{"-nwork"}, mode: ModeUI,
			check: func(t *testing.T, p Parsed) { eq(t, p.Mantle.Name, "work") }},
		{name: "multi-letter short", argv: []string{"-d2e"}, mode: ModeUI, engine: []string{"-d2e"},
			check: func(t *testing.T, p Parsed) {
				if v, ok := p.Value("--debug"); ok {
					t.Errorf("-d2e parsed as -d %q", v)
				}
			}},

		// Variadic values.
		{name: "variadic swallows a word", argv: []string{"--add-dir", "a", "b", "hi"}, mode: ModeUI, engine: []string{"--add-dir", "a", "b", "hi"},
			check: func(t *testing.T, p Parsed) { slice(t, p.Mantle.AddDirs, []string{"a", "b", "hi"}) }},
		{name: "variadic swallows a sentence", argv: []string{"--add-dir", "../x", "fix the bug"}, mode: ModeUI,
			engine: []string{"--add-dir", "../x", "fix the bug"}, warn: "not as the prompt"},
		{name: "variadic ends at a flag", argv: []string{"--add-dir", "a", "--model", "m", "hi"}, mode: ModeUI,
			engine: []string{"--add-dir", "a", "--model", "m"}, prompt: "hi"},
		{name: "variadic equals form takes one", argv: []string{"--add-dir=a", "hi"}, mode: ModeUI, engine: []string{"--add-dir=a"}, prompt: "hi"},
		{name: "prompt before variadic", argv: []string{"hi", "--add-dir", "a"}, mode: ModeUI, engine: []string{"--add-dir", "a"}, prompt: "hi"},
		{name: "variadic ended by --", argv: []string{"--add-dir", "a", "--", "--x"}, mode: ModeUI, engine: []string{"--add-dir", "a"}, prompt: "--x"},
		{name: "variadic first value may start with a dash", argv: []string{"--tools", "-x", "y"}, mode: ModeUI, engine: []string{"--tools", "-x", "y"}},
		{name: "alias spelling kept", argv: []string{"--allowed-tools", "Bash(git *)", "Edit", "--verbose"}, mode: ModeUI,
			engine: []string{"--allowed-tools", "Bash(git *)", "Edit", "--verbose"}},
		{name: "repeatable", argv: []string{"--plugin-dir", "a", "--plugin-dir", "b", "hi"}, mode: ModeUI,
			engine: []string{"--plugin-dir", "a", "--plugin-dir", "b"}, prompt: "hi",
			check: func(t *testing.T, p Parsed) { slice(t, p.Values("--plugin-dir"), []string{"a", "b"}) }},

		// -- and positional arguments.
		{name: "double dash", argv: []string{"--", "-p"}, mode: ModeUI, prompt: "-p"},
		{name: "lone dash is positional", argv: []string{"-"}, mode: ModeUI, prompt: "-"},
		{name: "only double dash", argv: []string{"--"}, mode: ModeUI},
		{name: "extra arguments", argv: []string{"hello", "world"}, mode: ModeUI, prompt: "hello", warn: "extra arguments",
			check: func(t *testing.T, p Parsed) { slice(t, p.ExtraArgs, []string{"world"}) }},

		// Unknown flags.
		{name: "unknown switch then prompt", argv: []string{"--new-thing", "hi"}, mode: ModeUI, engine: []string{"--new-thing"}, prompt: "hi",
			warn: "--new-thing=<value>", check: func(t *testing.T, p Parsed) { slice(t, p.Unknown, []string{"--new-thing"}) }},
		{name: "unknown with equals", argv: []string{"--new-thing=3", "hi"}, mode: ModeUI, engine: []string{"--new-thing=3"}, prompt: "hi"},
		{name: "unknown short", argv: []string{"-x", "--model", "m"}, mode: ModeUI, engine: []string{"-x", "--model", "m"}},
		{name: "known switch with equals is unknown", argv: []string{"--chrome=yes"}, mode: ModeUI, engine: []string{"--chrome=yes"},
			check: func(t *testing.T, p Parsed) { slice(t, p.Unknown, []string{"--chrome=yes"}) }},

		// Subcommands and exec.
		{name: "subcommand", argv: []string{"mcp", "list"}, mode: ModeExecClaude, check: sub("mcp")},
		{name: "subcommand after flags", argv: []string{"--model", "x", "mcp", "list"}, mode: ModeExecClaude, check: sub("mcp")},
		{name: "subcommand alias", argv: []string{"plugins", "list"}, mode: ModeExecClaude, check: sub("plugins")},
		{name: "hidden subcommand", argv: []string{"rc"}, mode: ModeExecClaude, check: sub("rc")},
		{name: "subcommand after --", argv: []string{"--", "mcp"}, mode: ModeExecClaude, check: sub("mcp")},
		{name: "subcommand word as second positional", argv: []string{"hi", "mcp"}, mode: ModeUI, prompt: "hi", warn: "extra arguments"},
		{name: "subcommand word swallowed by variadic", argv: []string{"--add-dir", "a", "mcp"}, mode: ModeUI, engine: []string{"--add-dir", "a", "mcp"}},
		{name: "subcommand after unknown flag", argv: []string{"--new-thing", "mcp"}, mode: ModeExecClaude},
		{name: "subcommand flags are claude's", argv: []string{"mcp", "add", "--model"}, mode: ModeExecClaude},
		{name: "print anywhere", argv: []string{"hi", "--model", "m", "-p"}, mode: ModeExecClaude},
		{name: "print as a value", argv: []string{"--append-system-prompt", "-p"}, mode: ModeUI, engine: []string{"--append-system-prompt", "-p"}},
		{name: "print with print-only flags", argv: []string{"-p", "--output-format", "json", "hi"}, mode: ModeExecClaude},
		{name: "exec wins over version", argv: []string{"--version", "-p"}, mode: ModeExecClaude},
		{name: "exec wins over errors", argv: []string{"-p", "--model"}, mode: ModeExecClaude},
		{name: "tmux equals", argv: []string{"-w", "--tmux=classic"}, mode: ModeExecClaude},

		// Launcher commands: first argument only.
		{name: "launcher versions", argv: []string{"versions"}, mode: ModeLauncher, check: sub("versions")},
		{name: "launcher doctor", argv: []string{"doctor"}, mode: ModeLauncher, check: sub("doctor")},
		{name: "versions later is a prompt", argv: []string{"--verbose", "versions"}, mode: ModeUI, engine: []string{"--verbose"}, prompt: "versions"},
		{name: "doctor later is claude's", argv: []string{"--verbose", "doctor"}, mode: ModeExecClaude, check: sub("doctor")},

		// Version, help and errors.
		{name: "version", argv: []string{"-v"}, mode: ModeVersion},
		{name: "version ignores a later missing value", argv: []string{"--version", "--model"}, mode: ModeVersion},
		{name: "version as a value", argv: []string{"--model", "--version"}, mode: ModeUI, engine: []string{"--model", "--version"}},
		{name: "help", argv: []string{"--help", "hi"}, mode: ModeHelp},
		{name: "help ignores print-only", argv: []string{"-h", "--output-format", "json"}, mode: ModeHelp},
		{name: "help after missing value", argv: []string{"--help", "--model"}, err: "needs a value"},
		{name: "print-only without print", argv: []string{"--output-format", "json"}, err: "--output-format only works with -p"},
		{name: "print-only switch", argv: []string{"--include-partial-messages"}, err: "only works with -p"},

		// Consumed and observed flags.
		{name: "session flags", argv: []string{"-c", "--fork-session", "--session-id", "u-1", "-n", "work", "hi"}, mode: ModeUI, engine: sw, prompt: "hi",
			check: func(t *testing.T, p Parsed) {
				m := p.Mantle
				if !m.Continue || !m.ForkSession || m.SessionID != "u-1" || m.Name != "work" {
					t.Errorf("mantle opts %+v", m)
				}
			}},
		{name: "verbose is read and forwarded", argv: []string{"--verbose"}, mode: ModeUI, engine: []string{"--verbose"},
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Verbose {
					t.Error("want verbose")
				}
			}},
		{name: "bypass gate inputs", argv: []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--permission-mode", "plan"}, mode: ModeUI,
			engine: []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--permission-mode", "plan"},
			check: func(t *testing.T, p Parsed) {
				m := p.Mantle
				if !m.SkipPermissions || !m.AllowSkipPermissions || m.PermissionMode != "plan" {
					t.Errorf("mantle opts %+v", m)
				}
			}},
		{name: "bare warns", argv: []string{"--bare"}, mode: ModeUI, engine: []string{"--bare"}, warn: "skips hooks",
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Bare {
					t.Error("want bare")
				}
			}},
		{name: "settings in order", argv: []string{"--settings", "a.json", "--setting-sources", "user", "--settings", "{}"}, mode: ModeUI,
			engine: []string{"--settings", "a.json", "--setting-sources", "user", "--settings", "{}"},
			check: func(t *testing.T, p Parsed) {
				slice(t, p.Mantle.Settings, []string{"a.json", "{}"})
				eq(t, p.Mantle.SettingSources, "user")
			}},
		{name: "screen reader", argv: []string{"--ax-screen-reader"}, mode: ModeUI, engine: sw,
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.ScreenReader {
					t.Error("want screen reader")
				}
			}},
		{name: "safe", argv: []string{"--safe", "hi"}, mode: ModeUI, engine: sw, prompt: "hi",
			check: func(t *testing.T, p Parsed) {
				if !p.Mantle.Safe {
					t.Error("want safe")
				}
			}},
		{name: "prefill", argv: []string{"--prefill-b64", "aGVsbG8="}, mode: ModeUI, engine: sw,
			check: func(t *testing.T, p Parsed) { eq(t, p.Mantle.Prefill, "hello") }},
		{name: "prefill bad", argv: []string{"--prefill-b64", "%%%"}, err: "not valid base64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Parse(c.argv)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if p.Mode != c.mode {
				t.Fatalf("mode = %v, want %v", p.Mode, c.mode)
			}
			if c.mode == ModeExecClaude && !slices.Equal(p.Exec, nonNil(c.argv)) {
				t.Errorf("exec = %q, want argv unchanged %q", p.Exec, c.argv)
			}
			if c.mode == ModeUI {
				if len(c.engine) == 0 && len(p.EngineArgs) != 0 || len(c.engine) > 0 && !slices.Equal(p.EngineArgs, c.engine) {
					t.Errorf("engine args = %q, want %q", p.EngineArgs, c.engine)
				}
				eq(t, p.Prompt, c.prompt)
				joined := strings.Join(p.Warnings, "\n")
				if c.warn == "" && joined != "" {
					t.Errorf("unexpected warnings %q", p.Warnings)
				}
				if c.warn != "" && !strings.Contains(joined, c.warn) {
					t.Errorf("warnings %q, want one containing %q", p.Warnings, c.warn)
				}
			}
			if c.check != nil {
				c.check(t, p)
			}
		})
	}
}

func TestParseDoesNotAliasArgv(t *testing.T) {
	argv := []string{"-cd", "hi"}
	p, err := Parse(argv)
	if err != nil {
		t.Fatal(err)
	}
	p.EngineArgs[0] = "changed"
	if argv[0] != "-cd" {
		t.Fatal("Parse modified its input")
	}
	argv2 := []string{"mcp", "list"}
	p2, _ := Parse(argv2)
	p2.Exec[0] = "changed"
	if argv2[0] != "mcp" {
		t.Fatal("Exec aliases argv")
	}
}

func TestParseWithExtraFlag(t *testing.T) {
	// An engine flag learnt at runtime (drift) fixes the tokenizing of its value.
	table := append(slices.Clone(Flags), Flag{Long: "--new-level", Arity: ArityRequired, Class: Forward})
	p, err := ParseWith([]string{"--new-level", "3", "hi"}, table)
	if err != nil {
		t.Fatal(err)
	}
	slice(t, p.EngineArgs, []string{"--new-level", "3"})
	eq(t, p.Prompt, "hi")
	if len(p.Unknown) != 0 || len(p.Warnings) != 0 {
		t.Errorf("unknown %q warnings %q", p.Unknown, p.Warnings)
	}
}

func TestIsPassthroughSubcommand(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"mcp", "list"}, true},
		{[]string{"--model", "x", "plugin", "list"}, true},
		{[]string{"kill", "abc"}, true},
		{[]string{"remote-control"}, true},
		{[]string{"doctor"}, false},
		{[]string{"--verbose", "doctor"}, false},
		{[]string{"versions"}, false},
		{[]string{"fix mcp"}, false},
		{[]string{"hi", "mcp"}, false},
		{[]string{"--add-dir", "a", "mcp"}, false},
		{[]string{"-p", "mcp"}, true},
		{nil, false},
	} {
		if got := IsPassthroughSubcommand(c.argv); got != c.want {
			t.Errorf("IsPassthroughSubcommand(%q) = %v, want %v", c.argv, got, c.want)
		}
	}
}

func TestPassthroughSubcommands(t *testing.T) {
	want := []string{
		"agents", "attach", "auth", "auto-mode", "daemon", "design-login",
		"drop-worktree-registrations", "edit-chrome-settings", "edit-hook",
		"edit-memory-settings", "edit-permission-rules", "edit-sandbox-settings",
		"edit-skill-overrides", "gateway", "import", "import-conversations", "install",
		"kill", "logs", "mcp", "plugin", "plugins", "project", "purge", "rc",
		"remote-control", "respawn", "rm", "sandbox", "self-hosted-runner", "setup-token",
		"stop", "ultrareview", "update", "upgrade",
	}
	slice(t, PassthroughSubcommands(), want)
	if slices.Contains(PassthroughSubcommands(), "doctor") {
		t.Error("doctor belongs to the launcher")
	}
}

func sub(name string) func(*testing.T, Parsed) {
	return func(t *testing.T, p Parsed) { eq(t, p.Subcommand, name) }
}

func resume(t *testing.T, p Parsed, on bool, query string) {
	t.Helper()
	if p.Mantle.Resume != on || p.Mantle.ResumeQuery != query {
		t.Errorf("resume = %v %q, want %v %q", p.Mantle.Resume, p.Mantle.ResumeQuery, on, query)
	}
	if len(p.EngineArgs) > 0 && slices.ContainsFunc(p.EngineArgs, func(s string) bool { return strings.HasPrefix(s, "-r") || strings.HasPrefix(s, "--resume") }) {
		t.Errorf("resume leaked into engine args %q", p.EngineArgs)
	}
}

func boolPtr(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func eq(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func slice(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
