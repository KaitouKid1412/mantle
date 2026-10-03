package cli

import "slices"

// Subcommand is a claude subcommand. mantle never interprets them: they run claude
// with argv unchanged.
type Subcommand struct {
	Name    string
	Aliases []string
	Hidden  bool // claude accepts it but --help doesn't list it
	// Launcher marks a word the launcher (plan 10) answers first: `mantle doctor`
	// runs mantle's checks, then offers `claude doctor`.
	Launcher bool
	// NoHelpProbe marks a subcommand whose --help doesn't print help (2.1.288's
	// remote-control starts instead). Drift tooling must not run it.
	NoHelpProbe bool
	Parity      string
}

// Subcommands lists every root subcommand of claude 2.1.288. Hidden ones were confirmed
// with `claude <name> --help` printing that subcommand's usage.
var Subcommands = []Subcommand{
	{Name: "agents", Parity: "CL-10"},
	{Name: "attach", Parity: "CL-11"},
	{Name: "auth", Parity: "CLI-19"},
	{Name: "auto-mode", Parity: "CLI-19"},
	{Name: "doctor", Launcher: true, Parity: "CLI-19"},
	{Name: "gateway", Parity: "CLI-19"},
	{Name: "import", Parity: "CLI-19"},
	{Name: "install", Parity: "CLI-28"},
	{Name: "logs", Parity: "CL-11"},
	{Name: "mcp", Parity: "CLI-19"},
	{Name: "plugin", Aliases: []string{"plugins"}, Parity: "CLI-19"},
	{Name: "purge", Parity: "CLI-19"},
	{Name: "respawn", Parity: "CL-11"},
	{Name: "rm", Parity: "CL-11"},
	{Name: "setup-token", Parity: "CLI-19"},
	{Name: "stop", Aliases: []string{"kill"}, Parity: "CL-11"},
	{Name: "ultrareview", Parity: "CLI-19"},
	{Name: "update", Aliases: []string{"upgrade"}, Parity: "CLI-19"},

	{Name: "daemon", Hidden: true, Parity: "CLI-19"},
	{Name: "design-login", Hidden: true, Parity: "CLI-19"},
	{Name: "drop-worktree-registrations", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-chrome-settings", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-hook", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-memory-settings", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-permission-rules", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-sandbox-settings", Hidden: true, Parity: "CLI-19"},
	{Name: "edit-skill-overrides", Hidden: true, Parity: "CLI-19"},
	{Name: "import-conversations", Hidden: true, Parity: "CLI-19"},
	{Name: "project", Hidden: true, Parity: "CLI-19"},
	{Name: "remote-control", Aliases: []string{"rc"}, Hidden: true, NoHelpProbe: true, Parity: "CL-01"},
	{Name: "sandbox", Hidden: true, Parity: "CLI-19"},
	{Name: "self-hosted-runner", Hidden: true, Parity: "CLI-19"},
}

var subcommandIndex = func() map[string]*Subcommand {
	m := make(map[string]*Subcommand)
	for i := range Subcommands {
		s := &Subcommands[i]
		m[s.Name] = s
		for _, a := range s.Aliases {
			m[a] = s
		}
	}
	return m
}()

// LookupSubcommand returns the subcommand called name (or one of its aliases), or nil.
func LookupSubcommand(name string) *Subcommand { return subcommandIndex[name] }

// IsSubcommand reports whether word names a claude subcommand or alias.
func IsSubcommand(word string) bool { return subcommandIndex[word] != nil }

// PassthroughSubcommands returns every subcommand name and alias that execs claude
// directly, sorted: all of Subcommands except the launcher's (doctor). The launcher
// (plan 10) keeps a copy of this list; its test compares the two.
func PassthroughSubcommands() []string {
	var names []string
	for _, s := range Subcommands {
		if s.Launcher {
			continue
		}
		names = append(names, s.Name)
		names = append(names, s.Aliases...)
	}
	slices.Sort(names)
	return names
}

// IsPassthroughSubcommand reports whether argv (without the program name) runs a
// claude subcommand that the launcher should exec straight away: its first positional
// argument, after any root flags and their values, is in PassthroughSubcommands.
func IsPassthroughSubcommand(argv []string) bool {
	if len(argv) > 0 && slices.Contains(LauncherCommands, argv[0]) {
		return false
	}
	var p Parsed
	operands, _ := tokenize(&p, argv, defaultIndex)
	if len(operands) == 0 {
		return false
	}
	s := LookupSubcommand(operands[0])
	return s != nil && !s.Launcher
}
