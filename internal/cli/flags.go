package cli

// Arity says how many arguments a flag takes. The semantics are commander.js's, which
// is what claude parses its command line with.
type Arity uint8

const (
	// ArityNone is a switch: --flag.
	ArityNone Arity = iota
	// ArityRequired takes the next argument whatever it looks like (--flag <v>), or the
	// text after "=" (--flag=<v>).
	ArityRequired
	// ArityOptional takes the next argument only when it doesn't start with "-"
	// (--flag [v]), or the text after "=".
	ArityOptional
	// ArityVariadic takes the next argument, then every following argument up to the
	// next one that starts with "-" (--flag <v...>). A prompt written after it is
	// swallowed, exactly as claude does.
	ArityVariadic
	// ArityRepeatable parses like ArityRequired; every occurrence is kept.
	ArityRepeatable
)

func (a Arity) String() string {
	switch a {
	case ArityNone:
		return "none"
	case ArityRequired:
		return "required"
	case ArityOptional:
		return "optional"
	case ArityVariadic:
		return "variadic"
	case ArityRepeatable:
		return "repeatable"
	}
	return "unknown"
}

// TakesValue reports whether the flag accepts a value at all.
func (a Arity) TakesValue() bool { return a != ArityNone }

// Class says what mantle does with a flag.
type Class uint8

const (
	// Forward passes the flag to the engine exactly as written. mantle may also read
	// its value (MantleOpts lists the ones it reads).
	Forward Class = iota
	// Consume means mantle handles the flag and does not forward it as written. Resume,
	// continue, session ID and name are resolved by mantle and re-emitted to the engine
	// in the form it needs.
	Consume
	// PrintOnly flags only mean something with -p. mantle runs its engine with its own
	// stream flags, so without -p they stop mantle with an error.
	PrintOnly
	// Exec hands the whole command line to claude unchanged (interactive-only or cloud
	// features mantle doesn't rebuild).
	Exec
	// Warn forwards the flag after a one-line warning.
	Warn
)

func (c Class) String() string {
	switch c {
	case Forward:
		return "forward"
	case Consume:
		return "consume"
	case PrintOnly:
		return "print-only"
	case Exec:
		return "exec"
	case Warn:
		return "warn"
	}
	return "unknown"
}

// Flag is one command-line flag. Names are stored with their dashes.
type Flag struct {
	Long    string   // "--model"
	Short   string   // "-n"; claude also has a multi-letter one ("-d2e"), matched whole
	Aliases []string // other spellings of Long ("--allowed-tools")
	Arity   Arity
	// EqValue marks a switch that also accepts --flag=value. claude rewrites
	// --tmux=classic before its parser sees it.
	EqValue bool
	Class   Class
	Hidden  bool   // claude accepts it but --help doesn't list it
	Mantle  bool   // mantle's own flag; claude doesn't know it
	Parity  string // PARITY.md row
}

// Names returns every spelling of the flag: long, short, then aliases.
func (f *Flag) Names() []string {
	n := []string{f.Long}
	if f.Short != "" {
		n = append(n, f.Short)
	}
	return append(n, f.Aliases...)
}

// Flags is the table of every root flag of claude 2.1.288, hidden ones included (claude
// knows them, so mantle must know their arity to find the prompt), plus mantle's own.
// Unknown flags are still forwarded verbatim; see Parse.
//
// Decisions for interactive-only flags (plan 11, B2) are recorded in the Class column
// and tested in flags_test.go.
var Flags = []Flag{
	// Context and directories.
	{Long: "--add-dir", Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--worktree", Short: "-w", Arity: ArityOptional, Class: Forward, Parity: "CLI-15"},
	{Long: "--tmux", EqValue: true, Class: Exec, Parity: "CLI-16"},

	// Agents and models.
	{Long: "--agent", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--agents", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--model", Arity: ArityRequired, Class: Forward, Parity: "CLI-07"},
	{Long: "--fallback-model", Arity: ArityRequired, Class: Forward, Parity: "CLI-07"},
	{Long: "--effort", Arity: ArityRequired, Class: Forward, Parity: "CLI-07"},
	{Long: "--autocompact", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--betas", Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--advisor", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--thinking", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--thinking-display", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--max-thinking-tokens", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--task-budget", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--workload", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},

	// Permissions and tools.
	{Long: "--permission-mode", Arity: ArityRequired, Class: Forward, Parity: "CLI-07"},
	{Long: "--dangerously-skip-permissions", Class: Forward, Parity: "CLI-07"},
	{Long: "--allow-dangerously-skip-permissions", Class: Forward, Parity: "CLI-07"},
	{Long: "--allowedTools", Aliases: []string{"--allowed-tools"}, Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--disallowedTools", Aliases: []string{"--disallowed-tools"}, Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--tools", Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--restricted", Class: Forward, Parity: "CLI-12"},
	{Long: "--inherit-permission-mode", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-07"},
	{Long: "--enable-auto-mode", Class: Forward, Hidden: true, Parity: "CLI-07"},

	// System prompt.
	{Long: "--system-prompt", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--system-prompt-file", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--append-system-prompt", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--append-system-prompt-file", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--append-subagent-system-prompt", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--append-subagent-system-prompt-file", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--plan-mode-instructions", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--exclude-dynamic-system-prompt-sections", Class: Forward, Parity: "CLI-08"},
	{Long: "--system-prompt-snapshot", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},

	// Sessions.
	{Long: "--continue", Short: "-c", Class: Consume, Parity: "CLI-05"},
	{Long: "--resume", Short: "-r", Arity: ArityOptional, Class: Consume, Parity: "CLI-05"},
	{Long: "--fork-session", Class: Consume, Parity: "CLI-05"},
	{Long: "--session-id", Arity: ArityRequired, Class: Consume, Parity: "CLI-05"},
	{Long: "--name", Short: "-n", Arity: ArityRequired, Class: Consume, Parity: "CLI-05"},
	{Long: "--resume-session-at", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-05"},
	{Long: "--resume-drops-turn", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-05"},
	{Long: "--reply-on-resume", Class: Forward, Hidden: true, Parity: "CLI-05"},
	{Long: "--from-pr", Arity: ArityOptional, Class: Exec, Parity: "CLI-18"},
	{Long: "--bg", Aliases: []string{"--background"}, Class: Exec, Parity: "CLI-17"},

	// Print / SDK mode. mantle's engine gets its stream flags from plan 02, never from
	// the user's argv.
	{Long: "--print", Short: "-p", Class: Exec, Parity: "CLI-04"},
	{Long: "--output-format", Arity: ArityRequired, Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--input-format", Arity: ArityRequired, Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--json-schema", Arity: ArityRequired, Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--include-partial-messages", Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--include-hook-events", Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--replay-user-messages", Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--forward-subagent-text", Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--max-budget-usd", Arity: ArityRequired, Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--no-session-persistence", Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--permission-prompts", Arity: ArityRequired, Class: PrintOnly, Parity: "CLI-26"},
	{Long: "--permission-prompt-tool", Arity: ArityRequired, Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--max-turns", Arity: ArityRequired, Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--session-mirror", Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--await-claim", Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--await-initialize", Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--enable-auth-status", Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--rewind-files", Arity: ArityRequired, Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	{Long: "--sdk-url", Arity: ArityRequired, Class: PrintOnly, Hidden: true, Parity: "CLI-26"},
	// Interactive claude honours --prompt-suggestions too; mantle asks for them through
	// initialize.promptSuggestions instead of the flag.
	{Long: "--prompt-suggestions", Arity: ArityOptional, Class: Consume, Parity: "CLI-01"},

	// Config sources.
	{Long: "--settings", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--setting-sources", Arity: ArityRequired, Class: Forward, Parity: "CLI-08"},
	{Long: "--mcp-config", Arity: ArityVariadic, Class: Forward, Parity: "CLI-08"},
	{Long: "--strict-mcp-config", Class: Forward, Parity: "CLI-08"},
	{Long: "--plugin-dir", Arity: ArityRepeatable, Class: Forward, Parity: "CLI-08"},
	{Long: "--plugin-dir-no-mcp", Arity: ArityRepeatable, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--plugin-url", Arity: ArityRepeatable, Class: Forward, Parity: "CLI-08"},
	{Long: "--disable-slash-commands", Class: Forward, Parity: "CLI-08"},
	{Long: "--bare", Class: Warn, Parity: "CLI-12"},
	{Long: "--safe-mode", Class: Forward, Parity: "CLI-12"},
	{Long: "--managed-settings", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--project-config-root", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--client-data-url", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--channels", Arity: ArityVariadic, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--dangerously-load-development-channels", Arity: ArityVariadic, Class: Forward, Hidden: true, Parity: "CLI-08"},
	// Setup hooks run inside the engine, so these work headless.
	{Long: "--init", Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--maintenance", Class: Forward, Hidden: true, Parity: "CLI-08"},
	// Runs the Setup hooks and exits: nothing for mantle to show.
	{Long: "--init-only", Class: Exec, Hidden: true, Parity: "CLI-08"},

	// Integrations.
	{Long: "--ide", Class: Forward, Parity: "CLI-11"},
	{Long: "--chrome", Class: Forward, Parity: "CLI-11"},
	{Long: "--no-chrome", Class: Forward, Parity: "CLI-11"},
	{Long: "--file", Arity: ArityVariadic, Class: Forward, Parity: "CLI-18"},
	{Long: "--brief", Class: Forward, Parity: "CLI-18"},
	{Long: "--watch-artifact", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-18"},
	{Long: "--watch-artifact-no-autoreact", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-18"},
	{Long: "--messaging-socket-path", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-18"},
	{Long: "--desktop", Class: Exec, Parity: "CLI-18"},
	{Long: "--cloud", Arity: ArityOptional, Class: Exec, Parity: "CL-05"},
	{Long: "--remote", Arity: ArityOptional, Class: Exec, Hidden: true, Parity: "CL-05"},
	{Long: "--environment", Arity: ArityRequired, Class: Exec, Parity: "CLI-18"},
	{Long: "--pool", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--ref", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--on-branch", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--correlation-id", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--forward-home-settings", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--attach-serve", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--teleport", Arity: ArityOptional, Class: Exec, Parity: "CLI-18"},
	{Long: "--remote-control", Arity: ArityOptional, Class: Exec, Parity: "CLI-18"},
	{Long: "--rc", Arity: ArityOptional, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--remote-control-session-name-prefix", Arity: ArityRequired, Class: Exec, Parity: "CLI-18"},
	// Deep links (claude-cli:// URLs) open claude's own prompt flow.
	{Long: "--deep-link-origin", Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--deep-link-repo", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--deep-link-last-fetch", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	{Long: "--deep-link-cwd-b64", Arity: ArityRequired, Class: Exec, Hidden: true, Parity: "CLI-18"},
	// Text to put in the prompt box; mantle's editor shows it.
	{Long: "--prefill", Arity: ArityRequired, Class: Consume, Hidden: true, Parity: "CLI-18"},
	{Long: "--prefill-b64", Arity: ArityRequired, Class: Consume, Hidden: true, Parity: "CLI-18"},

	// Agent teams: claude passes these to the teammate processes it starts.
	{Long: "--agent-id", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--agent-name", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--agent-type", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--agent-color", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--team-name", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--parent-session-id", Arity: ArityRequired, Class: Forward, Hidden: true, Parity: "CLI-08"},
	{Long: "--plan-mode-required", Class: Forward, Hidden: true, Parity: "CLI-08"},
	// Teammate panes are a known gap (GAP-03).
	{Long: "--teammate-mode", Arity: ArityRequired, Class: Warn, Hidden: true, Parity: "CLI-08"},

	// Debug and display.
	{Long: "--debug", Short: "-d", Arity: ArityOptional, Class: Forward, Parity: "CLI-09"},
	{Long: "--debug-to-stderr", Short: "-d2e", Class: Forward, Hidden: true, Parity: "CLI-09"},
	{Long: "--debug-file", Arity: ArityRequired, Class: Forward, Parity: "CLI-09"},
	{Long: "--verbose", Class: Forward, Parity: "CLI-09"},
	{Long: "--ax-screen-reader", Class: Consume, Parity: "CLI-10"},
	{Long: "--version", Short: "-v", Class: Consume, Parity: "CLI-13"},
	{Long: "--help", Short: "-h", Class: Consume, Parity: "CLI-14"},

	// mantle's own. The launcher handles --safe first; mantle-ui accepts it too so it
	// never reaches the engine.
	{Long: "--safe", Class: Consume, Mantle: true, Parity: "CLI-01"},
	// The in-place restart (request 10-02): the old mantle-ui execs the new build with
	// the engines' inherited fds described in this file; the host adopts them.
	{Long: "--attach-engine-fds", Arity: ArityRequired, Class: Consume, Hidden: true, Mantle: true, Parity: "MT-32"},
}

// flagIndex maps every spelling to its table entry.
type flagIndex map[string]*Flag

func indexFlags(table []Flag) flagIndex {
	idx := make(flagIndex, len(table)*2)
	for i := range table {
		for _, n := range table[i].Names() {
			idx[n] = &table[i]
		}
	}
	return idx
}

var defaultIndex = indexFlags(Flags)

// Lookup returns the flag spelled name ("--allowed-tools", "-r"), or nil.
func Lookup(name string) *Flag { return defaultIndex[name] }
