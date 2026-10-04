package cli

import (
	"regexp"
	"strings"
)

// HelpFlag is one option listed by commander.js help output.
type HelpFlag struct {
	Long    string   `json:"long"`
	Short   string   `json:"short,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	// Arity is "none", "required", "optional", "variadic" or "optional-variadic".
	Arity string `json:"arity"`
}

// HelpCommand is one subcommand listed by help output.
type HelpCommand struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

var commandNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// ParseHelp reads commander.js help output (`claude --help`, `claude <sub> --help`)
// and returns the options and subcommands it lists, names only.
//
// Option entries start with two spaces and a dash; the flag spec ends at the first run
// of two spaces (long specs sit alone on their line, the description below). Command
// entries start with two spaces and a letter; "plugin|plugins" lists aliases.
func ParseHelp(text string) (flags []HelpFlag, commands []HelpCommand) {
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" {
			continue
		}
		if line[0] != ' ' {
			section = ""
			if strings.HasSuffix(line, ":") {
				section = strings.TrimSuffix(line, ":")
			}
			continue
		}
		if !strings.HasPrefix(line, "  ") || len(line) < 3 || line[2] == ' ' {
			continue // a continuation line
		}
		entry := line[2:]
		if i := strings.Index(entry, "  "); i >= 0 {
			entry = entry[:i]
		}
		switch section {
		case "Options":
			if f, ok := parseFlagSpec(entry); ok {
				flags = append(flags, f)
			}
		case "Commands":
			if c, ok := parseCommandSpec(entry); ok {
				commands = append(commands, c)
			}
		}
	}
	return flags, commands
}

// parseFlagSpec reads "-r, --resume [value]" or "--allowedTools, --allowed-tools <x...>".
func parseFlagSpec(spec string) (HelpFlag, bool) {
	f := HelpFlag{Arity: "none"}
	for _, tok := range strings.Fields(spec) {
		tok = strings.TrimSuffix(tok, ",")
		switch {
		case strings.HasPrefix(tok, "<"):
			f.Arity = "required"
			if strings.HasSuffix(tok, "...>") {
				f.Arity = "variadic"
			}
		case strings.HasPrefix(tok, "["):
			f.Arity = "optional"
			if strings.HasSuffix(tok, "...]") {
				f.Arity = "optional-variadic"
			}
		case strings.HasPrefix(tok, "--"):
			if f.Long == "" {
				f.Long = tok
			} else {
				f.Aliases = append(f.Aliases, tok)
			}
		case strings.HasPrefix(tok, "-"):
			f.Short = tok
		}
	}
	if f.Long == "" {
		f.Long, f.Short = f.Short, ""
	}
	return f, f.Long != ""
}

// parseCommandSpec reads "stop|kill <id>" or "agents [options]".
func parseCommandSpec(spec string) (HelpCommand, bool) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return HelpCommand{}, false
	}
	names := strings.Split(fields[0], "|")
	for _, n := range names {
		if !commandNameRE.MatchString(n) {
			return HelpCommand{}, false
		}
	}
	if names[0] == "help" {
		return HelpCommand{}, false
	}
	return HelpCommand{Name: names[0], Aliases: names[1:]}, true
}

// HelpIsFor reports whether help output belongs to the subcommand: claude prints its
// root help for a subcommand it doesn't know. The usage line may list aliases
// ("Usage: claude plugin|plugins [options] [command]").
func HelpIsFor(text, sub string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	rest, ok := strings.CutPrefix(first, "Usage: claude "+sub)
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '|')
}

// Arity returns the table arity for a help arity ("optional-variadic" parses like
// optional for the first value; commander then keeps taking values).
func (f HelpFlag) TableArity() Arity {
	switch f.Arity {
	case "required":
		return ArityRequired
	case "optional", "optional-variadic":
		return ArityOptional
	case "variadic":
		return ArityVariadic
	}
	return ArityNone
}
