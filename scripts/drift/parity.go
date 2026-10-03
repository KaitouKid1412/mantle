package main

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// ParityIndex holds the names PARITY.md's indexes list.
type ParityIndex struct {
	IDs      map[string]bool // row IDs: CLI-01, TR-12, …
	Slash    map[string]bool // commands and aliases, without the slash
	Settings map[string]bool // UI settings keys
	Contexts map[string]bool // keybinding contexts
	Flags    map[string]bool // flags named in the CLI flags index
}

// HasFlag reports whether the CLI flags index names the flag.
func (p ParityIndex) HasFlag(name string) bool { return p.Flags[name] }

var (
	parityIDRE = regexp.MustCompile(`^\| ([A-Z]+-\d+) \|`)
	backtickRE = regexp.MustCompile("`([^`]+)`")
)

// parseParity reads the row IDs and the slash command, keybinding context, UI settings
// and CLI flag indexes from PARITY.md.
func parseParity(r io.Reader) (ParityIndex, error) {
	p := ParityIndex{IDs: map[string]bool{}, Slash: map[string]bool{}, Settings: map[string]bool{},
		Contexts: map[string]bool{}, Flags: map[string]bool{}}
	section := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
			continue
		}
		if m := parityIDRE.FindStringSubmatch(line); m != nil {
			p.IDs[m[1]] = true
		}
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "|---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		switch {
		case strings.HasPrefix(section, "Slash command index"):
			if len(cells) >= 2 {
				for _, n := range backticked(cells[0]) {
					p.Slash[strings.TrimPrefix(n, "/")] = true
				}
				for _, a := range strings.Split(cells[1], ",") {
					if a = strings.Trim(strings.TrimSpace(a), "`/"); a != "" {
						p.Slash[a] = true
					}
				}
			}
		case strings.HasPrefix(section, "UI settings keys index"):
			for _, n := range settingKeys(cells[0]) {
				p.Settings[n] = true
			}
		case strings.HasPrefix(section, "Keybinding contexts index"):
			if c := strings.TrimSpace(cells[0]); c != "" && c != "Context" {
				for _, part := range strings.Split(c, "/") {
					p.Contexts[strings.TrimSpace(part)] = true
				}
			}
		case strings.HasPrefix(section, "CLI flags index"):
			for _, n := range backticked(cells[0]) {
				for _, tok := range strings.Fields(strings.NewReplacer(",", " ", "[", " ", "]", " ").Replace(n)) {
					if strings.HasPrefix(tok, "-") {
						p.Flags[tok] = true
					}
				}
			}
		}
	}
	return p, sc.Err()
}

// settingKeys reads a UI settings index cell: "`statusLine` (`command`, `padding`)" names
// statusLine, statusLine.command and statusLine.padding; "`a` (global), `b`" names a
// and b.
func settingKeys(cell string) []string {
	var keys []string
	parent, depth := "", 0
	for i := 0; i < len(cell); i++ {
		switch cell[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '`':
			j := strings.IndexByte(cell[i+1:], '`')
			if j < 0 {
				return keys
			}
			name := cell[i+1 : i+1+j]
			i += j + 1
			if depth > 0 && parent != "" {
				keys = append(keys, parent+"."+name)
			} else {
				parent = name
				keys = append(keys, name)
			}
		}
	}
	return keys
}

func backticked(s string) []string {
	var out []string
	for _, m := range backtickRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
