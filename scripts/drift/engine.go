package main

import (
	"context"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

// EngineReport is what a throwaway engine session reports, as normalized items.
type EngineReport struct {
	Commands     []Item // initialize.commands: name, aliases
	Models       []Item // initialize.models[].value
	OutputStyles []Item // initialize.available_output_styles
	Tools        []Item // system/init.tools
}

// engineSession runs cli.ProbeEngine: isolated config, empty cwd, the API at a closed
// local port (zero tokens).
func (c *Collector) engineSession(ctx context.Context) (EngineReport, error) {
	var rep EngineReport
	r, err := cli.ProbeEngine(ctx, c.Claude, c.EngineTimeout)
	if err != nil {
		return rep, err
	}
	for _, cmd := range r.Commands {
		it := Item{Name: cmd.Name}
		if len(cmd.Aliases) > 0 {
			it.Attrs = map[string]string{"aliases": strings.Join(cmd.Aliases, ",")}
		}
		rep.Commands = append(rep.Commands, it)
	}
	names := func(ss []string) []Item {
		var out []Item
		for _, s := range ss {
			out = append(out, Item{Name: s})
		}
		return out
	}
	rep.Models, rep.OutputStyles, rep.Tools = names(r.Models), names(r.OutputStyles), names(r.Tools)
	return rep, nil
}

// collectEngine runs the engine session and returns its lists. With NoEngine set, or
// when the session fails, the lists are unavailable.
func (c *Collector) collectEngine(ctx context.Context) []List {
	source := "zero-token engine session (initialize, system/init)"
	lists := []List{
		{Kind: KindSlash, Source: source},
		{Kind: KindTool, Source: source},
		{Kind: KindOutputStyle, Source: source},
		{Kind: KindModel, Source: source},
	}
	fail := func(msg string) []List {
		for i := range lists {
			lists[i].Error = msg
		}
		return lists
	}
	if c.NoEngine {
		return fail("skipped (-no-engine)")
	}
	rep, err := c.engineSession(ctx)
	if err != nil {
		return fail(err.Error())
	}
	lists[0].Items, lists[1].Items, lists[2].Items, lists[3].Items = rep.Commands, rep.Tools, rep.OutputStyles, rep.Models
	return lists
}
