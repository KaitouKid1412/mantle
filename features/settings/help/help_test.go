package help

import (
	"reflect"
	"testing"
)

func sampleKnown() Known {
	return Known{Skills: map[string]bool{"simplify": true}, Plugins: map[string]bool{"acme": true}}
}

func TestClassify(t *testing.T) {
	k := sampleKnown()
	cases := map[string]struct {
		c    EngineCommand
		want Source
	}{
		"builtin":         {EngineCommand{Name: "compact", Builtin: true}, Engine},
		"mcp":             {EngineCommand{Name: "mcp__github__review"}, MCP},
		"plugin":          {EngineCommand{Name: "acme:deploy"}, Plugin},
		"unknown plugin":  {EngineCommand{Name: "other:thing"}, Plugin},
		"skill":           {EngineCommand{Name: "simplify"}, Skill},
		"custom":          {EngineCommand{Name: "fix-issue"}, Custom},
		"builtin wins":    {EngineCommand{Name: "simplify", Builtin: true}, Engine},
		"mcp beats colon": {EngineCommand{Name: "mcp__a:b"}, MCP},
	}
	for name, c := range cases {
		if got := Classify(c.c, k); got != c.want {
			t.Errorf("%s: got %v want %v", name, got, c.want)
		}
	}
}

func TestMergeAndGroups(t *testing.T) {
	native := []Command{
		{Name: "model", Description: "Pick a model", ArgHint: "[model]", Source: Native},
		{Name: "clear", Aliases: []string{"reset", "new"}, Source: Native},
		{Name: "secret", Hidden: true, Source: Native},
		{Name: "pirate", Source: Mod},
	}
	engine := []EngineCommand{
		{Name: "model", Description: "engine twin", Builtin: true}, // shadowed by native
		{Name: "reset", Builtin: true},                             // shadowed by alias
		{Name: "compact", Description: "Summarise", Builtin: true},
		{Name: "simplify", Description: "Tidy code"},
		{Name: "acme:deploy"},
		{Name: "mcp__gh__pr"},
		{Name: "fix-issue", ArgumentHint: "<n>"},
		{Name: "compact", Builtin: true}, // duplicate
	}
	cmds := Merge(native, engine, sampleKnown())
	groups := Groups(cmds)
	var got [][]string
	for _, g := range groups {
		var names []string
		for _, c := range g.Commands {
			names = append(names, c.Name)
		}
		got = append(got, append([]string{g.Title}, names...))
	}
	want := [][]string{
		{"Built-in", "clear", "model"},
		{"Claude Code", "compact"},
		{"Custom commands", "fix-issue"},
		{"Skills", "simplify"},
		{"Plugins", "acme:deploy"},
		{"MCP prompts", "mcp__gh__pr"},
		{"mantle mods", "pirate"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups\n got %v\nwant %v", got, want)
	}
	if groups[0].Commands[1].Description != "Pick a model" {
		t.Error("native command lost its description")
	}
}

func TestUsage(t *testing.T) {
	c := Command{Name: "clear", Aliases: []string{"reset", "new"}}
	if c.Usage() != "/clear (reset, new)" {
		t.Errorf("%q", c.Usage())
	}
	c = Command{Name: "model", ArgHint: "[model]"}
	if c.Usage() != "/model [model]" {
		t.Errorf("%q", c.Usage())
	}
}

func TestFilter(t *testing.T) {
	cmds := []Command{
		{Name: "compact", Description: "Summarise the conversation"},
		{Name: "config", Description: "Settings"},
		{Name: "context", Description: "Context usage"},
		{Name: "clear", Aliases: []string{"reset"}},
		{Name: "theme", Description: "Pick colours for the conversation"},
	}
	names := func(cs []Command) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.Name)
		}
		return out
	}
	// Name prefixes first; "theme" only matches through its description.
	if got := names(Filter(cmds, "/co")); !reflect.DeepEqual(got, []string{"compact", "config", "context", "theme"}) {
		t.Errorf("prefix: %v", got)
	}
	if got := names(Filter(cmds, "conversation")); !reflect.DeepEqual(got, []string{"compact", "theme"}) {
		t.Errorf("description: %v", got)
	}
	if got := names(Filter(cmds, "reset")); !reflect.DeepEqual(got, []string{"clear"}) {
		t.Errorf("alias: %v", got)
	}
	if got := names(Filter(cmds, "config")); got[0] != "config" {
		t.Errorf("exact first: %v", got)
	}
	if len(Filter(cmds, "  ")) != len(cmds) {
		t.Error("empty query")
	}
}

func TestActionDescription(t *testing.T) {
	if ActionDescription("chat:modelPicker") != "Choose the model" {
		t.Error("known action")
	}
	if got := ActionDescription("scroll:halfPageDown"); got != "Half page down" {
		t.Errorf("humanize %q", got)
	}
	if got := Humanize("plain"); got != "Plain" {
		t.Errorf("no prefix %q", got)
	}
}

func TestKeyGroups(t *testing.T) {
	bs := []Binding{
		{Context: "Chat", Keys: "enter", Action: "chat:submit", Description: "Send"},
		{Context: "Chat", Keys: "ctrl+j", Action: "chat:newline"},
		{Context: "Chat", Keys: "shift+enter", Action: "chat:newline", Description: "New line"},
		{Context: "Chat", Keys: "ctrl+j", Action: "chat:newline"},
		{Context: "ModelPicker", Keys: "s", Action: "modelPicker:sessionOnly"},
		{Context: "Global", Keys: "ctrl+c", Action: "app:interrupt"},
		{Context: "Chat", Keys: "", Action: "chat:stash"},
		{Context: "Autocomplete", Keys: "tab", Action: "autocomplete:accept"},
	}
	gs := KeyGroups(bs)
	var ctxs []string
	for _, g := range gs {
		ctxs = append(ctxs, g.Context)
	}
	if !reflect.DeepEqual(ctxs, []string{"Global", "Chat", "Autocomplete", "ModelPicker"}) {
		t.Errorf("contexts %v", ctxs)
	}
	chat := gs[1]
	if len(chat.Lines) != 2 {
		t.Fatalf("chat lines %+v", chat.Lines)
	}
	nl := chat.Lines[1]
	if !reflect.DeepEqual(nl.Keys, []string{"ctrl+j", "shift+enter"}) || nl.Description != "New line" {
		t.Errorf("newline line %+v", nl)
	}
	f := FilterKeys(gs, "newline")
	if len(f) != 1 || f[0].Context != "Chat" || len(f[0].Lines) != 1 {
		t.Errorf("filter %+v", f)
	}
	if len(FilterKeys(gs, "")) != len(gs) {
		t.Error("empty filter")
	}
}
