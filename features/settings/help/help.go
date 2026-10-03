// Package help builds the /help content: commands grouped by where they come from, and
// key bindings grouped by context. Pure functions over plain lists, so the dialog, the
// `?` shortcut panel and tests share one model.
package help

import (
	"sort"
	"strings"
)

// Source is where a command comes from.
type Source int

const (
	Native Source = iota // implemented by mantle
	Engine               // a Claude Code built-in passed through to the engine
	Custom               // .claude/commands/*.md
	Skill
	Plugin
	MCP // MCP server prompts
	Mod // mantle mods
)

// Title is the group heading for a source.
func (s Source) Title() string {
	switch s {
	case Native:
		return "Built-in"
	case Engine:
		return "Claude Code"
	case Custom:
		return "Custom commands"
	case Skill:
		return "Skills"
	case Plugin:
		return "Plugins"
	case MCP:
		return "MCP prompts"
	case Mod:
		return "mantle mods"
	}
	return "Other"
}

// Command is one slash command.
type Command struct {
	Name        string // without the leading slash
	Description string
	ArgHint     string
	Aliases     []string
	Source      Source
	Hidden      bool
}

// EngineCommand is an entry of initialize.commands / commands_changed.
type EngineCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ArgumentHint string   `json:"argumentHint,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	Builtin      bool     `json:"builtin,omitempty"`
}

// Known names the engine-side sets used to classify engine commands.
type Known struct {
	Skills  map[string]bool // system/init skills
	Plugins map[string]bool // plugin names (commands are "<plugin>:<cmd>")
}

// Classify decides an engine command's source. Built-ins are Engine; names starting
// with "mcp__" are MCP prompts; "<plugin>:<name>" with a known plugin is Plugin; known
// skill names are Skill; the rest are custom commands.
func Classify(c EngineCommand, k Known) Source {
	switch {
	case c.Builtin:
		return Engine
	case strings.HasPrefix(c.Name, "mcp__"):
		return MCP
	case strings.Contains(c.Name, ":") && k.Plugins[c.Name[:strings.IndexByte(c.Name, ':')]]:
		return Plugin
	case k.Skills[c.Name]:
		return Skill
	case strings.Contains(c.Name, ":"):
		return Plugin
	}
	return Custom
}

// Merge combines mantle's registered commands with the engine's list. A mantle command
// shadows an engine command of the same name or alias (mantle runs it natively), and
// hidden commands are dropped.
func Merge(native []Command, engine []EngineCommand, k Known) []Command {
	taken := map[string]bool{}
	var out []Command
	for _, c := range native {
		if c.Hidden {
			continue
		}
		out = append(out, c)
		taken[c.Name] = true
		for _, a := range c.Aliases {
			taken[a] = true
		}
	}
	for _, e := range engine {
		if taken[e.Name] {
			continue
		}
		taken[e.Name] = true
		out = append(out, Command{
			Name: e.Name, Description: e.Description, ArgHint: e.ArgumentHint,
			Aliases: e.Aliases, Source: Classify(e, k),
		})
	}
	return out
}

// Group is a titled list of commands.
type Group struct {
	Source   Source
	Title    string
	Commands []Command
}

// Groups sorts commands by name inside groups ordered by Source. Empty groups are left
// out.
func Groups(cmds []Command) []Group {
	by := map[Source][]Command{}
	for _, c := range cmds {
		by[c.Source] = append(by[c.Source], c)
	}
	var out []Group
	for s := Native; s <= Mod; s++ {
		list := by[s]
		if len(list) == 0 {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		out = append(out, Group{Source: s, Title: s.Title(), Commands: list})
	}
	return out
}

// Usage renders "/name <hint>" with aliases, e.g. "/clear (reset, new)".
func (c Command) Usage() string {
	s := "/" + c.Name
	if c.ArgHint != "" {
		s += " " + c.ArgHint
	}
	if len(c.Aliases) > 0 {
		s += " (" + strings.Join(c.Aliases, ", ") + ")"
	}
	return s
}

// Filter keeps commands matching query (case-insensitive) in their name, aliases or
// description. Name matches rank first, then alias matches, then description matches;
// within a rank, prefix matches come first and then names in order.
func Filter(cmds []Command, query string) []Command {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "/")))
	if q == "" {
		return cmds
	}
	type hit struct {
		c    Command
		rank int
	}
	var hits []hit
	for _, c := range cmds {
		r := rank(c, q)
		if r >= 0 {
			hits = append(hits, hit{c, r})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].rank != hits[j].rank {
			return hits[i].rank < hits[j].rank
		}
		return hits[i].c.Name < hits[j].c.Name
	})
	out := make([]Command, len(hits))
	for i, h := range hits {
		out[i] = h.c
	}
	return out
}

func rank(c Command, q string) int {
	name := strings.ToLower(c.Name)
	switch {
	case name == q:
		return 0
	case strings.HasPrefix(name, q):
		return 1
	case strings.Contains(name, q):
		return 2
	}
	for _, a := range c.Aliases {
		if strings.HasPrefix(strings.ToLower(a), q) {
			return 3
		}
	}
	if strings.Contains(strings.ToLower(c.Description), q) {
		return 4
	}
	return -1
}

// Binding is one key binding from the keymap table.
type Binding struct {
	Context     string // Claude Code context, e.g. "Chat"
	Keys        string // "ctrl+x ctrl+k"
	Action      string // action ID
	Description string
}

// KeyLine is one action with every key bound to it.
type KeyLine struct {
	Action      string
	Keys        []string
	Description string
}

// KeyGroup is the bindings of one context.
type KeyGroup struct {
	Context string
	Lines   []KeyLine
}

// contextOrder puts the contexts users meet most first; others follow alphabetically.
var contextOrder = []string{"Global", "Chat", "Autocomplete", "Confirmation", "HistorySearch", "Transcript", "Task", "Select", "Tabs", "Settings"}

// KeyGroups groups bindings by context, merging keys bound to the same action. Unbound
// entries (empty Keys) are dropped.
func KeyGroups(bs []Binding) []KeyGroup {
	type key struct{ ctx, action string }
	lines := map[key]*KeyLine{}
	var order []key
	for _, b := range bs {
		if b.Keys == "" || b.Action == "" {
			continue
		}
		k := key{b.Context, b.Action}
		l, ok := lines[k]
		if !ok {
			l = &KeyLine{Action: b.Action, Description: b.Description}
			lines[k] = l
			order = append(order, k)
		}
		if l.Description == "" {
			l.Description = b.Description
		}
		dup := false
		for _, have := range l.Keys {
			if have == b.Keys {
				dup = true
			}
		}
		if !dup {
			l.Keys = append(l.Keys, b.Keys)
		}
	}
	byCtx := map[string][]KeyLine{}
	for _, k := range order {
		byCtx[k.ctx] = append(byCtx[k.ctx], *lines[k])
	}
	var ctxs []string
	for c := range byCtx {
		ctxs = append(ctxs, c)
	}
	sort.Slice(ctxs, func(i, j int) bool {
		ri, rj := ctxRank(ctxs[i]), ctxRank(ctxs[j])
		if ri != rj {
			return ri < rj
		}
		return ctxs[i] < ctxs[j]
	})
	out := make([]KeyGroup, 0, len(ctxs))
	for _, c := range ctxs {
		out = append(out, KeyGroup{Context: c, Lines: byCtx[c]})
	}
	return out
}

func ctxRank(c string) int {
	for i, o := range contextOrder {
		if o == c {
			return i
		}
	}
	return len(contextOrder)
}

// FilterKeys keeps lines whose keys, action or description contain query.
func FilterKeys(groups []KeyGroup, query string) []KeyGroup {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return groups
	}
	var out []KeyGroup
	for _, g := range groups {
		var keep []KeyLine
		for _, l := range g.Lines {
			hay := strings.ToLower(l.Action + " " + l.Description + " " + strings.Join(l.Keys, " "))
			if strings.Contains(hay, q) {
				keep = append(keep, l)
			}
		}
		if len(keep) > 0 {
			out = append(out, KeyGroup{Context: g.Context, Lines: keep})
		}
	}
	return out
}
