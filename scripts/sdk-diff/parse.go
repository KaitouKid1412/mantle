package main

import (
	"regexp"
	"sort"
	"strings"
)

// decls holds the type declarations of a .d.ts file: object literals and unions.
type decls struct {
	objects map[string]string   // name -> body of `{ ... }` (top level only matters)
	unions  map[string][]string // name -> member type names
}

var (
	declRe   = regexp.MustCompile(`(?m)^(?:export )?declare type ([A-Za-z0-9_]+)\s*=\s*`)
	memberRe = regexp.MustCompile(`^(?:[A-Za-z0-9_]+\.)?([A-Za-z0-9_]+)$`)
	litRe    = regexp.MustCompile(`'([a-z0-9_]+)'`)
)

// parseDecls reads `declare type X = { ... };` and `declare type X = A | B;` forms.
// Anything else (generics, intersections, mapped types) is ignored.
func parseDecls(src string) decls {
	d := decls{objects: map[string]string{}, unions: map[string][]string{}}
	locs := declRe.FindAllStringSubmatchIndex(src, -1)
	for _, l := range locs {
		name := src[l[2]:l[3]]
		rest := src[l[1]:]
		switch {
		case strings.HasPrefix(rest, "{"):
			if body, ok := braceBody(rest); ok {
				d.objects[name] = body
			}
		default:
			end := strings.Index(rest, ";")
			if end < 0 {
				continue
			}
			expr := strings.TrimSpace(rest[:end])
			var members []string
			ok := true
			for _, part := range strings.Split(expr, "|") {
				m := memberRe.FindStringSubmatch(strings.TrimSpace(part))
				if m == nil {
					ok = false
					break
				}
				members = append(members, m[1])
			}
			if ok && len(members) > 0 {
				d.unions[name] = members
			}
		}
	}
	return d
}

// braceBody returns the text between the first '{' and its matching '}'.
func braceBody(s string) (string, bool) {
	depth := 0
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// topField returns the string literals of a depth-1 field `name: 'a' | 'b';`.
func topField(body, name string) []string {
	depth := 0
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if depth == 0 && (strings.HasPrefix(t, name+":") || strings.HasPrefix(t, name+"?:")) {
			v := t[strings.Index(t, ":")+1:]
			// A union may continue on the next lines (`| 'a'`) until the ';'.
			for j := i + 1; !strings.Contains(v, ";") && j < len(lines); j++ {
				v += " " + strings.TrimSpace(lines[j])
			}
			if k := strings.Index(v, ";"); k >= 0 {
				v = v[:k]
			}
			var out []string
			for _, m := range litRe.FindAllStringSubmatch(v, -1) {
				out = append(out, m[1])
			}
			return out
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
	}
	return nil
}

// resolve expands a type name into its object members (recursively through unions).
// Names it can't resolve are returned in missing.
func (d decls) resolve(name string, seen map[string]bool) (objs []string, missing []string) {
	if seen[name] {
		return nil, nil
	}
	seen[name] = true
	if _, ok := d.objects[name]; ok {
		return []string{name}, nil
	}
	ms, ok := d.unions[name]
	if !ok {
		return nil, []string{name}
	}
	for _, m := range ms {
		o, mi := d.resolve(m, seen)
		objs = append(objs, o...)
		missing = append(missing, mi...)
	}
	return objs, missing
}

// Protocol is the set of protocol names a .d.ts (or pkg/proto) declares.
type Protocol struct {
	Types          map[string]bool // stdout message types ("assistant", "system", ...)
	SystemSubtypes map[string]bool
	ResultSubtypes map[string]bool
	ControlSubs    map[string]bool // control request subtypes, both directions
	Unresolved     []string        // union members the parser couldn't resolve
}

func newProtocol() Protocol {
	return Protocol{Types: map[string]bool{}, SystemSubtypes: map[string]bool{}, ResultSubtypes: map[string]bool{}, ControlSubs: map[string]bool{}}
}

// fromSDK extracts the protocol from sdk.d.ts: StdoutMessage (or SDKMessage) members
// for message types and subtypes, SDKControlRequestInner for control subtypes.
func fromSDK(src string) Protocol {
	d := parseDecls(src)
	p := newProtocol()
	root := "StdoutMessage"
	if _, ok := d.unions[root]; !ok {
		root = "SDKMessage"
	}
	objs, missing := d.resolve(root, map[string]bool{})
	p.Unresolved = append(p.Unresolved, missing...)
	for _, o := range objs {
		body := d.objects[o]
		for _, typ := range topField(body, "type") {
			p.Types[typ] = true
			for _, sub := range topField(body, "subtype") {
				switch typ {
				case "system":
					p.SystemSubtypes[sub] = true
				case "result":
					p.ResultSubtypes[sub] = true
				}
			}
		}
	}
	ctl, missing := d.resolve("SDKControlRequestInner", map[string]bool{})
	p.Unresolved = append(p.Unresolved, missing...)
	for _, o := range ctl {
		for _, sub := range topField(d.objects[o], "subtype") {
			p.ControlSubs[sub] = true
		}
	}
	sort.Strings(p.Unresolved)
	return p
}

// Item is one difference: Name is "<category>:<value>", Kind is "sdk-only" (the SDK
// has it, pkg/proto doesn't) or "proto-only".
type Item struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// diff compares the SDK's protocol with pkg/proto's.
func diff(sdk, mine Protocol) []Item {
	var out []Item
	cmp := func(cat string, a, b map[string]bool) {
		for k := range a {
			if !b[k] {
				out = append(out, Item{cat + ":" + k, "sdk-only"})
			}
		}
		for k := range b {
			if !a[k] {
				out = append(out, Item{cat + ":" + k, "proto-only"})
			}
		}
	}
	cmp("type", sdk.Types, mine.Types)
	cmp("system", sdk.SystemSubtypes, mine.SystemSubtypes)
	cmp("result", sdk.ResultSubtypes, mine.ResultSubtypes)
	cmp("control", sdk.ControlSubs, mine.ControlSubs)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind > out[j].Kind // sdk-only first
		}
		return out[i].Name < out[j].Name
	})
	return out
}
