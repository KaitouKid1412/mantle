package main

import (
	"regexp"
	"strings"
)

var commandNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// parseHelp reads commander.js help output and returns the flags and subcommands it
// lists, normalized. scope is "" for claude's root help, or the subcommand name.
//
// Option entries start with two spaces and a dash; the flag spec ends at the first run
// of two spaces (long specs sit alone on their line, with the description below).
// Command entries start with two spaces and a letter; "plugin|plugins" lists aliases.
func parseHelp(text, scope string) (flags, subs []Item) {
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \r")
		if line == "" {
			continue
		}
		if line[0] != ' ' {
			if strings.HasSuffix(line, ":") {
				section = strings.TrimSuffix(line, ":")
			} else {
				section = ""
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
			if it, ok := parseFlagSpec(entry, scope); ok {
				flags = append(flags, it)
			}
		case "Commands":
			if it, ok := parseCommandSpec(entry, scope); ok {
				subs = append(subs, it)
			}
		}
	}
	return flags, subs
}

// parseFlagSpec reads "-r, --resume [value]" or "--allowedTools, --allowed-tools <x...>".
func parseFlagSpec(spec, scope string) (Item, bool) {
	var short, long string
	var aliases []string
	arity := "none"
	for _, tok := range strings.Fields(spec) {
		tok = strings.TrimSuffix(tok, ",")
		switch {
		case strings.HasPrefix(tok, "<"):
			arity = "required"
			if strings.HasSuffix(tok, "...>") {
				arity = "variadic"
			}
		case strings.HasPrefix(tok, "["):
			arity = "optional"
			if strings.HasSuffix(tok, "...]") {
				arity = "optional-variadic"
			}
		case strings.HasPrefix(tok, "--"):
			if long == "" {
				long = tok
			} else {
				aliases = append(aliases, tok)
			}
		case strings.HasPrefix(tok, "-"):
			short = tok
		}
	}
	name := long
	if name == "" {
		name = short
		short = ""
	}
	if name == "" {
		return Item{}, false
	}
	it := Item{Name: name, Scope: scope, Attrs: map[string]string{"arity": arity}}
	if short != "" {
		it.Attrs["short"] = short
	}
	if len(aliases) > 0 {
		it.Attrs["aliases"] = strings.Join(aliases, ",")
	}
	return it, true
}

// parseCommandSpec reads "stop|kill <id>" or "agents [options]".
func parseCommandSpec(spec, scope string) (Item, bool) {
	f := strings.Fields(spec)
	if len(f) == 0 {
		return Item{}, false
	}
	names := strings.Split(f[0], "|")
	for _, n := range names {
		if !commandNameRE.MatchString(n) {
			return Item{}, false
		}
	}
	if names[0] == "help" {
		return Item{}, false
	}
	it := Item{Name: names[0], Scope: scope}
	if len(names) > 1 {
		it.Attrs = map[string]string{"aliases": strings.Join(names[1:], ",")}
	}
	return it, true
}

// helpIsFor reports whether help output belongs to the subcommand: claude prints its
// root help for a subcommand it doesn't know. The usage line may list aliases
// ("Usage: claude plugin|plugins [options] [command]").
func helpIsFor(text, sub string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	rest, ok := strings.CutPrefix(first, "Usage: claude "+sub)
	return ok && (rest == "" || rest[0] == ' ' || rest[0] == '|')
}
