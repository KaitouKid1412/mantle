package claudecli

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Subcommand is a closed set of claude subcommands mantle may run. Each value maps
// to a fixed argv prefix, an allowlist of flags and a fixed number of operand
// slots, so no call can put free text where the CLI would read it as a prompt.
type Subcommand int

const (
	invalidSubcommand Subcommand = iota

	Version  // claude --version
	RootHelp // claude --help

	MCPList                // claude mcp list
	MCPGet                 // claude mcp get <name>
	MCPAdd                 // claude mcp add [flags] -- <name> <commandOrUrl> [args...]
	MCPAddJSON             // claude mcp add-json [flags] -- <name> <json>
	MCPAddFromDesktop      // claude mcp add-from-claude-desktop (interactive picker)
	MCPRemove              // claude mcp remove [--scope] <name>
	MCPLogin               // claude mcp login [--no-browser] <name> (interactive)
	MCPLogout              // claude mcp logout <name>
	MCPResetProjectChoices // claude mcp reset-project-choices

	PluginList        // claude plugin list
	PluginDetails     // claude plugin details <name>
	PluginInstall     // claude plugin install <plugin>
	PluginUninstall   // claude plugin uninstall <plugin>
	PluginEnable      // claude plugin enable <plugin>
	PluginDisable     // claude plugin disable [plugin]
	PluginUpdate      // claude plugin update <plugin>
	PluginConfigure   // claude plugin configure <plugin>
	PluginPrune       // claude plugin prune
	MarketplaceList   // claude plugin marketplace list
	MarketplaceAdd    // claude plugin marketplace add <source>
	MarketplaceRemove // claude plugin marketplace remove <name>
	MarketplaceUpdate // claude plugin marketplace update [name]

	AuthStatus // claude auth status
	AuthLogin  // claude auth login (interactive)
	AuthLogout // claude auth logout

	AgentsList // claude agents --json
	AgentsView // claude agents (interactive agent view)
	Attach     // claude attach <id> (interactive)
	Logs       // claude logs <id>
	Stop       // claude stop <id>

	Import           // claude import [source]
	AutoModeDefaults // claude auto-mode defaults
	AutoModeConfig   // claude auto-mode config
	Doctor           // claude doctor (interactive)
	Update           // claude update (interactive)

	numSubcommands
)

// mode says how a subcommand may be started.
type mode uint8

const (
	modeRun  mode = 1 << iota // captured output, stdin /dev/null unless declared
	modeExec                  // handed to tea.ExecProcess with the terminal attached
)

type flagKind uint8

const (
	flagBool     flagKind = iota // --json
	flagValue                    // --scope=user
	flagOptional                 // --yes or --yes=<digest>; never consumes the next token
)

// flagSpec is one allowed flag. Flags are always emitted in their long form, with
// values attached by "=" so a value can never be read as an operand or a flag.
type flagSpec struct {
	long   string // without the leading "--"
	short  byte   // 0 when there is no short alias
	kind   flagKind
	repeat bool
	enum   []string // allowed values, if restricted
}

// slot is one operand position.
type slot struct {
	name string
	re   *regexp.Regexp // nil: any non-empty single-line text (requires dashdash)
	enum []string
}

// spec is the table entry behind a Subcommand.
type spec struct {
	name     string   // "mcp get"
	prefix   []string // fixed argv prefix
	fixed    []string // flags always appended after prefix
	flags    []flagSpec
	slots    []slot
	minSlots int
	rest     bool // extra operands after the slots are allowed (mcp add's command args)
	// dashdash puts "--" before the operands. Commander-based subcommands accept it;
	// a few subcommands parse argv themselves and reject it, so their operands must
	// match a strict pattern instead.
	dashdash bool
	stdin    bool     // the call may pass stdin
	env      []string // extra environment variable names the call may set
	modes    mode
	timeout  time.Duration
}

var (
	reID      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	rePlugin  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@/:+-]{0,255}$`)
	reSHA256  = regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)
	reDigest  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	scopes3   = []string{"local", "user", "project"}
	scopesMgd = []string{"local", "user", "project", "managed"}
)

const (
	timeoutShort   = 30 * time.Second
	timeoutHealth  = 90 * time.Second // mcp list/get health-check every server
	timeoutNetwork = 5 * time.Minute  // git clones and downloads
)

var (
	fJSON  = flagSpec{long: "json"}
	fYes   = flagSpec{long: "yes", short: 'y'}
	fScope = flagSpec{long: "scope", short: 's', kind: flagValue, enum: scopes3}
)

var specs = [numSubcommands]spec{
	Version:  {name: "--version", prefix: []string{"--version"}, modes: modeRun, timeout: timeoutShort},
	RootHelp: {name: "--help", prefix: []string{"--help"}, modes: modeRun, timeout: timeoutShort},

	MCPList: {name: "mcp list", prefix: []string{"mcp", "list"}, modes: modeRun, timeout: timeoutHealth},
	MCPGet: {name: "mcp get", prefix: []string{"mcp", "get"},
		slots: []slot{{name: "name"}}, minSlots: 1, dashdash: true, modes: modeRun, timeout: timeoutHealth},
	MCPAdd: {name: "mcp add", prefix: []string{"mcp", "add"},
		flags: []flagSpec{
			fScope,
			{long: "transport", short: 't', kind: flagValue, enum: []string{"stdio", "sse", "http"}},
			{long: "env", short: 'e', kind: flagValue, repeat: true},
			{long: "header", short: 'H', kind: flagValue, repeat: true},
			{long: "client-id", kind: flagValue},
			{long: "client-secret"},
			{long: "callback-port", kind: flagValue},
		},
		slots: []slot{{name: "name"}, {name: "commandOrUrl"}}, minSlots: 2, rest: true, dashdash: true,
		env: []string{"MCP_CLIENT_SECRET"}, modes: modeRun, timeout: timeoutShort},
	MCPAddJSON: {name: "mcp add-json", prefix: []string{"mcp", "add-json"},
		flags: []flagSpec{fScope, {long: "client-secret"}},
		slots: []slot{{name: "name"}, {name: "json"}}, minSlots: 2, dashdash: true,
		env: []string{"MCP_CLIENT_SECRET"}, modes: modeRun, timeout: timeoutShort},
	MCPAddFromDesktop: {name: "mcp add-from-claude-desktop", prefix: []string{"mcp", "add-from-claude-desktop"},
		flags: []flagSpec{fScope}, modes: modeExec},
	MCPRemove: {name: "mcp remove", prefix: []string{"mcp", "remove"},
		flags: []flagSpec{fScope}, slots: []slot{{name: "name"}}, minSlots: 1, dashdash: true,
		modes: modeRun, timeout: timeoutShort},
	MCPLogin: {name: "mcp login", prefix: []string{"mcp", "login"},
		flags: []flagSpec{{long: "no-browser"}}, slots: []slot{{name: "name"}}, minSlots: 1, dashdash: true,
		modes: modeExec},
	MCPLogout: {name: "mcp logout", prefix: []string{"mcp", "logout"},
		slots: []slot{{name: "name"}}, minSlots: 1, dashdash: true, modes: modeRun | modeExec, timeout: timeoutShort},
	MCPResetProjectChoices: {name: "mcp reset-project-choices", prefix: []string{"mcp", "reset-project-choices"},
		modes: modeRun, timeout: timeoutShort},

	PluginList: {name: "plugin list", prefix: []string{"plugin", "list"},
		flags: []flagSpec{fJSON, {long: "available"}}, modes: modeRun, timeout: 2 * time.Minute},
	PluginDetails: {name: "plugin details", prefix: []string{"plugin", "details"},
		slots: []slot{{name: "plugin", re: rePlugin}}, minSlots: 1, dashdash: true, modes: modeRun, timeout: time.Minute},
	PluginInstall: {name: "plugin install", prefix: []string{"plugin", "install"},
		flags: []flagSpec{fJSON, fYes, fScope,
			{long: "accept-command", kind: flagValue},
			{long: "config", kind: flagValue, repeat: true},
			{long: "registry", kind: flagValue},
		},
		slots: []slot{{name: "plugin", re: rePlugin}}, minSlots: 1, dashdash: true,
		modes: modeRun, timeout: timeoutNetwork},
	PluginUninstall: {name: "plugin uninstall", prefix: []string{"plugin", "uninstall"},
		flags: []flagSpec{fJSON, fYes, fScope, {long: "keep-data"}, {long: "prune"}},
		slots: []slot{{name: "plugin", re: rePlugin}}, minSlots: 1, dashdash: true,
		modes: modeRun, timeout: time.Minute},
	PluginEnable: {name: "plugin enable", prefix: []string{"plugin", "enable"},
		flags: []flagSpec{fJSON, fScope}, slots: []slot{{name: "plugin", re: rePlugin}}, minSlots: 1, dashdash: true,
		modes: modeRun, timeout: time.Minute},
	PluginDisable: {name: "plugin disable", prefix: []string{"plugin", "disable"},
		flags: []flagSpec{fJSON, fScope, {long: "all", short: 'a'}}, slots: []slot{{name: "plugin", re: rePlugin}},
		dashdash: true, modes: modeRun, timeout: time.Minute},
	PluginUpdate: {name: "plugin update", prefix: []string{"plugin", "update"},
		flags: []flagSpec{fJSON, fYes, {long: "scope", short: 's', kind: flagValue, enum: scopesMgd},
			{long: "accept-command", kind: flagValue}},
		slots: []slot{{name: "plugin", re: rePlugin}}, minSlots: 1, dashdash: true,
		modes: modeRun, timeout: timeoutNetwork},
	PluginConfigure: {name: "plugin configure", prefix: []string{"plugin", "configure"},
		flags: []flagSpec{fJSON, {long: "values-stdin"}}, slots: []slot{{name: "plugin", re: rePlugin}},
		minSlots: 1, dashdash: true, stdin: true, modes: modeRun, timeout: time.Minute},
	PluginPrune: {name: "plugin prune", prefix: []string{"plugin", "prune"},
		flags: []flagSpec{fYes, fScope, {long: "dry-run"}}, modes: modeRun, timeout: time.Minute},
	MarketplaceList: {name: "plugin marketplace list", prefix: []string{"plugin", "marketplace", "list"},
		flags: []flagSpec{fJSON}, modes: modeRun, timeout: timeoutShort},
	MarketplaceAdd: {name: "plugin marketplace add", prefix: []string{"plugin", "marketplace", "add"},
		flags: []flagSpec{fJSON, {long: "claudeai"}, {long: "scope", kind: flagValue, enum: scopes3},
			{long: "sparse", kind: flagValue, repeat: true}},
		slots: []slot{{name: "source"}}, minSlots: 1, dashdash: true, modes: modeRun, timeout: timeoutNetwork},
	MarketplaceRemove: {name: "plugin marketplace remove", prefix: []string{"plugin", "marketplace", "remove"},
		flags: []flagSpec{fJSON, {long: "scope", kind: flagValue, enum: scopes3}},
		slots: []slot{{name: "name", re: rePlugin}}, minSlots: 1, dashdash: true, modes: modeRun, timeout: time.Minute},
	MarketplaceUpdate: {name: "plugin marketplace update", prefix: []string{"plugin", "marketplace", "update"},
		flags: []flagSpec{fJSON}, slots: []slot{{name: "name", re: rePlugin}}, dashdash: true,
		modes: modeRun, timeout: timeoutNetwork},

	AuthStatus: {name: "auth status", prefix: []string{"auth", "status"},
		flags: []flagSpec{fJSON, {long: "text"}}, modes: modeRun, timeout: timeoutShort},
	AuthLogin: {name: "auth login", prefix: []string{"auth", "login"},
		flags: []flagSpec{{long: "claudeai"}, {long: "console"}, {long: "sso"}, {long: "email", kind: flagValue}},
		modes: modeExec},
	AuthLogout: {name: "auth logout", prefix: []string{"auth", "logout"}, modes: modeRun | modeExec, timeout: timeoutShort},

	AgentsList: {name: "agents --json", prefix: []string{"agents"}, fixed: []string{"--json"},
		flags: []flagSpec{{long: "all"}, {long: "cwd", kind: flagValue}}, modes: modeRun, timeout: timeoutShort},
	AgentsView: {name: "agents", prefix: []string{"agents"}, modes: modeExec},
	// attach, logs and stop parse argv themselves and reject "--": strict IDs only.
	Attach: {name: "attach", prefix: []string{"attach"}, slots: []slot{{name: "id", re: reID}}, minSlots: 1,
		modes: modeExec},
	Logs: {name: "logs", prefix: []string{"logs"}, slots: []slot{{name: "id", re: reID}}, minSlots: 1,
		modes: modeRun, timeout: timeoutShort},
	Stop: {name: "stop", prefix: []string{"stop"}, slots: []slot{{name: "id", re: reID}}, minSlots: 1,
		modes: modeRun, timeout: timeoutShort},

	// import parses argv itself and rejects "--": the source is an enum.
	Import: {name: "import", prefix: []string{"import"},
		flags: []flagSpec{{long: "dry-run"}, {long: "yes", kind: flagOptional}},
		slots: []slot{{name: "source", enum: []string{"codex", "gemini", "cursor"}}},
		modes: modeRun | modeExec, timeout: time.Minute},
	AutoModeDefaults: {name: "auto-mode defaults", prefix: []string{"auto-mode", "defaults"},
		flags: []flagSpec{{long: "label", kind: flagValue}}, modes: modeRun, timeout: timeoutShort},
	AutoModeConfig: {name: "auto-mode config", prefix: []string{"auto-mode", "config"},
		modes: modeRun, timeout: timeoutShort},
	Doctor: {name: "doctor", prefix: []string{"doctor"}, modes: modeExec},
	Update: {name: "update", prefix: []string{"update"}, modes: modeExec},
}

// Subcommands lists every valid Subcommand, for tests and introspection.
func Subcommands() []Subcommand {
	out := make([]Subcommand, 0, numSubcommands-1)
	for s := invalidSubcommand + 1; s < numSubcommands; s++ {
		out = append(out, s)
	}
	return out
}

func (s Subcommand) valid() bool { return s > invalidSubcommand && s < numSubcommands }

func (s Subcommand) spec() *spec { return &specs[s] }

// String returns the subcommand as typed after "claude", e.g. "mcp get".
func (s Subcommand) String() string {
	if !s.valid() {
		return fmt.Sprintf("Subcommand(%d)", int(s))
	}
	return specs[s].name
}

// Prefix returns a copy of the fixed argv prefix.
func (s Subcommand) Prefix() []string {
	if !s.valid() {
		return nil
	}
	return append([]string(nil), specs[s].prefix...)
}

// Interactive reports whether the subcommand may be started with the terminal attached.
func (s Subcommand) Interactive() bool { return s.valid() && specs[s].modes&modeExec != 0 }

// Captured reports whether the subcommand may be run with captured output.
func (s Subcommand) Captured() bool { return s.valid() && specs[s].modes&modeRun != 0 }

// ArgError reports arguments a Subcommand does not accept. Nothing was started.
type ArgError struct {
	Sub Subcommand
	Msg string
}

func (e *ArgError) Error() string { return "claude " + e.Sub.String() + ": " + e.Msg }

// buildArgv validates args against the subcommand's table entry and returns the
// complete argv (without the binary). args may mix flags ("--scope=user",
// "--scope", "user", "-s", "user", "--json") and operands; an explicit "--"
// marks everything after it as operands.
func buildArgv(sub Subcommand, args []string) ([]string, error) {
	if !sub.valid() {
		return nil, &ArgError{Sub: sub, Msg: "unknown subcommand"}
	}
	sp := sub.spec()
	bad := func(format string, a ...any) error {
		return &ArgError{Sub: sub, Msg: fmt.Sprintf(format, a...)}
	}

	var flags, operands []string
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.ContainsRune(a, 0) {
			return nil, bad("argument contains a NUL byte")
		}
		if a == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			operands = append(operands, a)
			continue
		}
		name, value, hasValue := a, "", false
		if j := strings.IndexByte(a, '='); j >= 0 {
			name, value, hasValue = a[:j], a[j+1:], true
		}
		f := sp.findFlag(name)
		if f == nil {
			return nil, bad("flag %q is not allowed", name)
		}
		if seen[f.long] && !f.repeat {
			return nil, bad("flag --%s given twice", f.long)
		}
		seen[f.long] = true
		switch f.kind {
		case flagBool:
			if hasValue {
				return nil, bad("flag --%s takes no value", f.long)
			}
			flags = append(flags, "--"+f.long)
		case flagOptional:
			if hasValue {
				if value == "" {
					return nil, bad("flag --%s has an empty value", f.long)
				}
				flags = append(flags, "--"+f.long+"="+value)
			} else {
				flags = append(flags, "--"+f.long)
			}
		case flagValue:
			if !hasValue {
				if i+1 >= len(args) {
					return nil, bad("flag --%s needs a value", f.long)
				}
				i++
				value = args[i]
			}
			if value == "" || strings.ContainsAny(value, "\x00\n\r") {
				return nil, bad("flag --%s has an empty or multi-line value", f.long)
			}
			if len(f.enum) > 0 && !contains(f.enum, value) {
				return nil, bad("flag --%s must be one of %s", f.long, strings.Join(f.enum, ", "))
			}
			flags = append(flags, "--"+f.long+"="+value)
		}
	}

	if len(operands) < sp.minSlots {
		return nil, bad("needs %d operand(s), got %d", sp.minSlots, len(operands))
	}
	if len(operands) > len(sp.slots) && !sp.rest {
		return nil, bad("takes at most %d operand(s), got %d", len(sp.slots), len(operands))
	}
	for i, op := range operands {
		if i >= len(sp.slots) {
			if op == "" {
				return nil, bad("empty argument")
			}
			continue // rest operands follow "--", so the CLI never reads them as flags
		}
		if err := sp.slots[i].check(op); err != nil {
			return nil, bad("%s: %v", sp.slots[i].name, err)
		}
	}

	argv := make([]string, 0, len(sp.prefix)+len(sp.fixed)+len(flags)+1+len(operands))
	argv = append(argv, sp.prefix...)
	argv = append(argv, sp.fixed...)
	argv = append(argv, flags...)
	if len(operands) > 0 && sp.dashdash {
		argv = append(argv, "--")
	}
	argv = append(argv, operands...)
	return argv, nil
}

func (sp *spec) findFlag(name string) *flagSpec {
	for i := range sp.flags {
		f := &sp.flags[i]
		if name == "--"+f.long || (f.short != 0 && name == "-"+string(f.short)) {
			return f
		}
	}
	return nil
}

func (s slot) check(v string) error {
	switch {
	case v == "":
		return fmt.Errorf("empty value")
	case len(s.enum) > 0:
		if !contains(s.enum, v) {
			return fmt.Errorf("must be one of %s", strings.Join(s.enum, ", "))
		}
	case s.re != nil:
		if !s.re.MatchString(v) {
			return fmt.Errorf("invalid value %q", v)
		}
	case s.name == "json":
		// JSON may span lines; it still follows "--".
	default:
		if strings.ContainsAny(v, "\n\r") {
			return fmt.Errorf("multi-line value")
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
