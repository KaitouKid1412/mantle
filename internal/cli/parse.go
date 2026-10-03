package cli

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
)

// Mode is what mantle-ui does with a command line.
type Mode uint8

const (
	// ModeUI starts mantle's UI with an engine.
	ModeUI Mode = iota
	// ModeExecClaude replaces mantle with claude, argv unchanged (Parsed.Exec): -p,
	// subcommands and the flags of class Exec.
	ModeExecClaude
	// ModeVersion prints mantle's and the engine's versions.
	ModeVersion
	// ModeHelp prints mantle's help followed by claude's.
	ModeHelp
	// ModeLauncher is a launcher command (Parsed.Subcommand: versions, rollback or
	// doctor) that reached mantle-ui directly. The launcher (plan 10) normally handles
	// these before mantle-ui starts.
	ModeLauncher
)

func (m Mode) String() string {
	switch m {
	case ModeUI:
		return "ui"
	case ModeExecClaude:
		return "exec-claude"
	case ModeVersion:
		return "version"
	case ModeHelp:
		return "help"
	case ModeLauncher:
		return "launcher"
	}
	return "unknown"
}

// Occurrence is one flag as it appeared on the command line.
type Occurrence struct {
	Flag *Flag // nil for a flag mantle doesn't know
	// Name is the spelling used ("--allowed-tools", "-r").
	Name string
	// Values holds the flag's values: none for a switch or an optional value that was
	// left out, one for required and optional values, one or more for variadic flags.
	Values []string
	// Tokens are the argv elements the occurrence came from. A short cluster such as
	// "-cd" is split, so its tokens are rebuilt ("-d").
	Tokens []string
}

// Value returns the occurrence's last value, or "".
func (o Occurrence) Value() string {
	if len(o.Values) == 0 {
		return ""
	}
	return o.Values[len(o.Values)-1]
}

// Parsed is a command line, split into what mantle handles and what goes to claude.
type Parsed struct {
	Mode   Mode
	Mantle MantleOpts
	// EngineArgs holds every forwarded token (class Forward and Warn, and unknown
	// flags) in its original order and spelling. Pass it to the engine as extra args.
	EngineArgs []string
	// Prompt is the positional prompt. mantle submits it as the first user message once
	// the startup gates pass.
	Prompt string
	// ExtraArgs are positional arguments after the prompt. claude ignores them.
	ExtraArgs []string
	// Exec is the argv (without the program name) to run claude with in
	// ModeExecClaude: the original command line, unchanged.
	Exec []string
	// Subcommand is the claude subcommand (ModeExecClaude) or launcher command
	// (ModeLauncher) that decided the mode, if any.
	Subcommand string
	// Flags lists every flag occurrence in order.
	Flags []Occurrence
	// Unknown lists the flags mantle doesn't know. They are forwarded verbatim; they
	// are probably newer engine flags. Log them.
	Unknown []string
	// Warnings are one-line notices to show before the session starts.
	Warnings []string
}

// MantleOpts holds the flags mantle acts on.
type MantleOpts struct {
	// Consumed: not forwarded as written. mantle re-emits what the engine needs.
	Continue    bool
	Resume      bool   // -r/--resume given
	ResumeQuery string // its value: a session ID or search term; empty opens the picker
	ForkSession bool
	SessionID   string
	Name        string
	// ScreenReader is --ax-screen-reader: flat output for screen readers.
	ScreenReader bool
	// PromptSuggestions is nil when --prompt-suggestions wasn't given.
	PromptSuggestions *bool
	// Prefill is text for the prompt box (--prefill, or --prefill-b64 decoded).
	Prefill string
	// Safe is mantle --safe: run without user mods.
	Safe bool

	// Observed: forwarded unchanged and read by mantle too.
	Verbose              bool
	Debug                bool
	Model                string
	Effort               string
	PermissionMode       string
	SkipPermissions      bool // --dangerously-skip-permissions: plan 05's bypass gate
	AllowSkipPermissions bool // --allow-dangerously-skip-permissions
	Bare                 bool
	SafeMode             bool // --safe-mode
	Restricted           bool
	DisableSlashCommands bool
	Brief                bool
	Settings             []string // every --settings value, in order
	SettingSources       string
	AddDirs              []string
	Worktree             bool
	WorktreeName         string
}

// LauncherCommands are the words the launcher (plan 10) handles when they are the
// first argument.
var LauncherCommands = []string{"versions", "rollback", "doctor"}

// Parse splits a command line (without the program name) using the default flag table.
func Parse(argv []string) (Parsed, error) {
	return ParseWith(argv, Flags)
}

// ParseWith is Parse with another flag table, for example the default table plus
// flags learnt from the installed engine.
//
// Tokenizing follows commander.js, as claude does: --flag value and --flag=value;
// optional values only when the next argument doesn't start with "-"; variadic values
// up to the next option; short clusters ("-cd", "-rID"); "--" ends options; the first
// positional argument is the prompt, or a subcommand when it names one.
//
// The mode is decided in this order: launcher command (first argument only), exec
// (a subcommand or any Exec flag), version, parse error, help, UI. In UI mode,
// print-only flags are an error.
func ParseWith(argv []string, table []Flag) (Parsed, error) {
	var p Parsed
	if len(argv) > 0 && slices.Contains(LauncherCommands, argv[0]) {
		p.Mode = ModeLauncher
		p.Subcommand = argv[0]
		p.ExtraArgs = slices.Clone(argv[1:])
		return p, nil
	}

	idx := defaultIndex
	if !sameTable(table, Flags) {
		idx = indexFlags(table)
	}
	operands, tokErr := tokenize(&p, argv, idx)

	if len(operands) > 0 && IsSubcommand(operands[0]) {
		p.Subcommand = operands[0]
	} else if len(operands) > 0 {
		p.Prompt = operands[0]
		p.ExtraArgs = operands[1:]
	}

	if p.Subcommand != "" || p.has(func(f *Flag) bool { return f.Class == Exec }) {
		p.Mode = ModeExecClaude
		p.Exec = slices.Clone(argv)
		if p.Exec == nil {
			p.Exec = []string{}
		}
		return p, nil
	}
	if p.has(func(f *Flag) bool { return f.Long == "--version" }) {
		p.Mode = ModeVersion
		return p, nil
	}
	if tokErr != nil {
		return p, tokErr
	}
	if p.has(func(f *Flag) bool { return f.Long == "--help" }) {
		p.Mode = ModeHelp
		return p, nil
	}

	p.Mode = ModeUI
	for _, o := range p.Flags {
		if o.Flag != nil && o.Flag.Class == PrintOnly {
			return p, fmt.Errorf("%s only works with -p/--print (mantle runs its engine with its own stream flags); run `mantle -p …` for print mode", o.Name)
		}
	}
	if err := p.collect(); err != nil {
		return p, err
	}
	if len(p.ExtraArgs) > 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf("ignoring extra arguments after the prompt: %s (quote the whole prompt)", strings.Join(p.ExtraArgs, " ")))
	}
	if w := swallowedPrompt(&p); w != "" {
		p.Warnings = append(p.Warnings, w)
	}
	return p, nil
}

// swallowedPrompt warns when a variadic flag probably took the prompt: there is no
// prompt, and the flag's last value reads like a sentence. claude parses it the same
// way; the warning only says how to avoid it.
func swallowedPrompt(p *Parsed) string {
	if p.Prompt != "" {
		return ""
	}
	for i := len(p.Flags) - 1; i >= 0; i-- {
		o := p.Flags[i]
		if o.Flag == nil || o.Flag.Arity != ArityVariadic || len(o.Values) < 2 {
			continue
		}
		last := o.Value()
		if strings.ContainsAny(last, " \t\n") {
			return fmt.Sprintf("%q was read as a value of %s, not as the prompt; put the prompt before %s, or after --", last, o.Name, o.Name)
		}
		return ""
	}
	return ""
}

func sameTable(a, b []Flag) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// maybeOption is commander's test for an argument that looks like an option.
func maybeOption(arg string) bool { return len(arg) > 1 && arg[0] == '-' }

// tokenize fills p.Flags and p.Unknown and returns the positional arguments. It stops
// at the first claude subcommand, whose arguments belong to claude, and at a missing
// required value, which it returns as an error.
func tokenize(p *Parsed, argv []string, idx flagIndex) (operands []string, err error) {
	args := slices.Clone(argv)
	variadic := -1 // index in p.Flags of the variadic flag still taking values
	sawUnknown := false

	add := func(o Occurrence) {
		p.Flags = append(p.Flags, o)
		if o.Flag != nil && o.Flag.Arity == ArityVariadic && len(o.Tokens) == 2 {
			variadic = len(p.Flags) - 1
		}
	}

	for len(args) > 0 {
		arg := args[0]
		args = args[1:]

		if arg == "--" {
			return append(operands, args...), nil
		}
		if variadic >= 0 && !maybeOption(arg) {
			o := &p.Flags[variadic]
			o.Values = append(o.Values, arg)
			o.Tokens = append(o.Tokens, arg)
			continue
		}
		variadic = -1

		// An exact flag: --model, -r, -d2e.
		if maybeOption(arg) {
			if f := idx[arg]; f != nil {
				o := Occurrence{Flag: f, Name: arg, Tokens: []string{arg}}
				switch f.Arity {
				case ArityRequired, ArityRepeatable, ArityVariadic:
					if len(args) == 0 {
						add(o)
						return operands, fmt.Errorf("option %s needs a value", arg)
					}
					o.Values = []string{args[0]}
					o.Tokens = append(o.Tokens, args[0])
					args = args[1:]
				case ArityOptional:
					if len(args) > 0 && !maybeOption(args[0]) {
						o.Values = []string{args[0]}
						o.Tokens = append(o.Tokens, args[0])
						args = args[1:]
					}
				}
				add(o)
				continue
			}
		}

		// A short cluster: "-cd" is -c then -d; "-rID" is -r with value ID.
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			if f := idx[arg[:2]]; f != nil {
				if f.Arity.TakesValue() {
					add(Occurrence{Flag: f, Name: arg[:2], Values: []string{arg[2:]}, Tokens: []string{arg}})
				} else {
					add(Occurrence{Flag: f, Name: arg[:2], Tokens: []string{arg[:2]}})
					args = append([]string{"-" + arg[2:]}, args...)
				}
				continue
			}
		}

		// --flag=value. The value never starts a variadic list.
		if strings.HasPrefix(arg, "--") {
			if eq := strings.IndexByte(arg, '='); eq > 2 {
				if f := idx[arg[:eq]]; f != nil && (f.Arity.TakesValue() || f.EqValue) {
					add(Occurrence{Flag: f, Name: arg[:eq], Values: []string{arg[eq+1:]}, Tokens: []string{arg}})
					continue
				}
			}
		}

		if maybeOption(arg) {
			// Unknown: probably a newer engine flag. Forward it alone. If a plain word
			// follows, it can't be told apart from a value, so it stays positional and
			// the user is told how to be explicit.
			sawUnknown = true
			p.Unknown = append(p.Unknown, arg)
			add(Occurrence{Name: arg, Tokens: []string{arg}})
			if !strings.Contains(arg, "=") && len(args) > 0 && !maybeOption(args[0]) {
				p.Warnings = append(p.Warnings, fmt.Sprintf("unknown flag %s: %q is read as an argument, not its value; write %s=<value> if it takes one", arg, args[0], arg))
			}
			continue
		}

		// A subcommand is only recognised as the first positional argument; claude
		// parses everything after it.
		if len(operands) == 0 && !sawUnknown && IsSubcommand(arg) {
			return append(operands, arg), nil
		}
		operands = append(operands, arg)
	}
	return operands, nil
}

// has reports whether any known flag matching pred occurred.
func (p *Parsed) has(pred func(*Flag) bool) bool {
	for _, o := range p.Flags {
		if o.Flag != nil && pred(o.Flag) {
			return true
		}
	}
	return false
}

// collect fills Mantle and EngineArgs from Flags (UI mode).
func (p *Parsed) collect() error {
	m := &p.Mantle
	for _, o := range p.Flags {
		f := o.Flag
		if f == nil {
			p.EngineArgs = append(p.EngineArgs, o.Tokens...)
			continue
		}
		switch f.Class {
		case Forward:
			p.EngineArgs = append(p.EngineArgs, o.Tokens...)
		case Warn:
			p.EngineArgs = append(p.EngineArgs, o.Tokens...)
			p.Warnings = append(p.Warnings, warnText(f, o.Name))
		}

		switch f.Long {
		case "--continue":
			m.Continue = true
		case "--resume":
			m.Resume = true
			m.ResumeQuery = o.Value()
		case "--fork-session":
			m.ForkSession = true
		case "--session-id":
			m.SessionID = o.Value()
		case "--name":
			m.Name = o.Value()
		case "--ax-screen-reader":
			m.ScreenReader = true
		case "--prompt-suggestions":
			v, err := parseBool(o)
			if err != nil {
				return err
			}
			m.PromptSuggestions = &v
		case "--prefill":
			m.Prefill = o.Value()
		case "--prefill-b64":
			b, err := base64.StdEncoding.DecodeString(o.Value())
			if err != nil {
				return fmt.Errorf("%s: not valid base64", o.Name)
			}
			m.Prefill = string(b)
		case "--safe":
			m.Safe = true
		case "--verbose":
			m.Verbose = true
		case "--debug", "--debug-file", "--debug-to-stderr":
			m.Debug = true
		case "--model":
			m.Model = o.Value()
		case "--effort":
			m.Effort = o.Value()
		case "--permission-mode":
			m.PermissionMode = o.Value()
		case "--dangerously-skip-permissions":
			m.SkipPermissions = true
		case "--allow-dangerously-skip-permissions":
			m.AllowSkipPermissions = true
		case "--bare":
			m.Bare = true
		case "--safe-mode":
			m.SafeMode = true
		case "--restricted":
			m.Restricted = true
		case "--disable-slash-commands":
			m.DisableSlashCommands = true
		case "--brief":
			m.Brief = true
		case "--settings":
			m.Settings = append(m.Settings, o.Value())
		case "--setting-sources":
			m.SettingSources = o.Value()
		case "--add-dir":
			m.AddDirs = append(m.AddDirs, o.Values...)
		case "--worktree":
			m.Worktree = true
			m.WorktreeName = o.Value()
		}
	}
	return nil
}

func warnText(f *Flag, name string) string {
	switch f.Long {
	case "--bare":
		return name + ": the engine skips hooks, skills, plugins, MCP servers and CLAUDE.md for this session"
	case "--teammate-mode":
		return name + ": mantle can't show teammate panes yet; forwarded to the engine as is"
	}
	return name + ": forwarded to the engine as is"
}

// parseBool reads an optional boolean value the way claude does: no value means true.
func parseBool(o Occurrence) (bool, error) {
	if len(o.Values) == 0 {
		return true, nil
	}
	switch strings.ToLower(o.Value()) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not a boolean (use true or false)", o.Name, o.Value())
}

// Has reports whether the flag with this long name occurred.
func (p *Parsed) Has(long string) bool {
	return p.has(func(f *Flag) bool { return f.Long == long })
}

// Value returns the last value given to the flag with this long name.
func (p *Parsed) Value(long string) (string, bool) {
	val, ok := "", false
	for _, o := range p.Flags {
		if o.Flag != nil && o.Flag.Long == long {
			val, ok = o.Value(), true
		}
	}
	return val, ok
}

// Values returns every value given to the flag with this long name, in order.
func (p *Parsed) Values(long string) []string {
	var vals []string
	for _, o := range p.Flags {
		if o.Flag != nil && o.Flag.Long == long {
			vals = append(vals, o.Values...)
		}
	}
	return vals
}
