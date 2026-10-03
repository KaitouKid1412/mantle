package main

import (
	"strings"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

// parseHelp turns help output into normalized items (see cli.ParseHelp). scope is ""
// for claude's root help, or the subcommand name.
func parseHelp(text, scope string) (flags, subs []Item) {
	fs, cs := cli.ParseHelp(text)
	for _, f := range fs {
		it := Item{Name: f.Long, Scope: scope, Attrs: map[string]string{"arity": f.Arity}}
		if f.Short != "" {
			it.Attrs["short"] = f.Short
		}
		if len(f.Aliases) > 0 {
			it.Attrs["aliases"] = strings.Join(f.Aliases, ",")
		}
		flags = append(flags, it)
	}
	for _, c := range cs {
		it := Item{Name: c.Name, Scope: scope}
		if len(c.Aliases) > 0 {
			it.Attrs = map[string]string{"aliases": strings.Join(c.Aliases, ",")}
		}
		subs = append(subs, it)
	}
	return flags, subs
}

func helpIsFor(text, sub string) bool { return cli.HelpIsFor(text, sub) }
