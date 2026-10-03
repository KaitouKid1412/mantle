package termsetup

import (
	"fmt"
	"strconv"
	"strings"
)

// alacrittyBlock is appended to alacritty.toml. TOML basic strings take \u escapes, so
// the file stays plain ASCII.
const alacrittyBlock = "[[keyboard.bindings]]\n" +
	"key = \"Return\"\n" +
	"mods = \"Shift\"\n" +
	"chars = \"\\u001b\\r\"\n"

func planAlacritty(in Input) Proposal {
	if in.KittyKeyboard {
		return nativeProposal(in.Terminal)
	}
	summary := "Add a Shift+Enter binding to Alacritty that sends Esc+Return, which mantle reads as a newline."
	notes := []string{"Alacritty reloads its config automatically."}
	manual := func(summary string, extra ...string) Proposal {
		return Proposal{Status: Manual, Kind: TargetFile, Summary: summary, Notes: extra}
	}
	if !in.Exists && in.LegacyPath != "" {
		return manual("Only a YAML config ("+in.LegacyPath+") was found; Alacritty 0.13 and newer read alacritty.toml.",
			"Run `alacritty migrate` to convert it, then run /terminal-setup again.")
	}
	content := in.Content
	if !in.Exists {
		content = nil
	}

	scan := scanAlacritty(string(content))
	switch {
	case scan.inlineBindings:
		return manual("alacritty.toml lists key bindings in an inline array, so mantle left it alone.",
			"Add this entry to keyboard.bindings yourself: { key = \"Return\", mods = \"Shift\", chars = \"\\u001b\\r\" }")
	case scan.installed:
		return Proposal{Status: AlreadyInstalled, Kind: TargetFile,
			Summary: "Alacritty already sends a newline for Shift+Enter."}
	case scan.conflict != "":
		return manual("Alacritty already binds Shift+Enter ("+scan.conflict+"), so mantle left it alone.",
			"Change or remove that binding in alacritty.toml, then run /terminal-setup again.")
	}

	var b strings.Builder
	b.Write(content)
	if len(content) > 0 {
		if content[len(content)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(alacrittyBlock)
	p := editProposal(TargetFile, in.Path, content, []byte(b.String()), summary)
	p.Notes = notes
	return p
}

type alacrittyScan struct {
	inlineBindings bool   // keyboard.bindings written as an inline array
	installed      bool   // a Shift+Return binding already sends a newline
	conflict       string // description of a Shift+Return binding doing something else
}

// scanAlacritty is a line-oriented look at a TOML file: enough to find key bindings
// without a full TOML parser.
func scanAlacritty(src string) alacrittyScan {
	var res alacrittyScan
	table := ""
	isBinding := false
	cur := map[string]string{}
	flush := func() {
		if isBinding {
			res.check(cur)
		}
		cur = map[string]string{}
	}
	depth := 0 // bracket depth of a multi-line value being skipped
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(stripTOMLComment(raw))
		if line == "" {
			continue
		}
		if depth > 0 {
			depth += bracketDelta(line)
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			flush()
			table = strings.TrimSpace(line[2 : len(line)-2])
			isBinding = table == "keyboard.bindings"
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			table = strings.TrimSpace(line[1 : len(line)-1])
			isBinding = false
			if table == "keyboard.bindings" {
				res.inlineBindings = true
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(line[:eq]), `"'`)
		val := strings.TrimSpace(line[eq+1:])
		full := key
		if table != "" {
			full = table + "." + key
		}
		if full == "keyboard.bindings" {
			res.inlineBindings = true
		}
		if d := bracketDelta(val); d > 0 {
			depth = d
			continue
		}
		if isBinding {
			cur[key] = val
		}
	}
	flush()
	return res
}

func (res *alacrittyScan) check(kv map[string]string) {
	key, _ := tomlString(kv["key"])
	if !strings.EqualFold(key, "Return") && !strings.EqualFold(key, "Enter") {
		return
	}
	mods, _ := tomlString(kv["mods"])
	parts := strings.Split(mods, "|")
	if len(parts) != 1 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Shift") {
		return
	}
	if chars, ok := tomlString(kv["chars"]); ok {
		if chars == NewlineSeq || chars == "\n" {
			res.installed = true
			return
		}
		if res.conflict == "" {
			res.conflict = fmt.Sprintf("sends %q", chars)
		}
		return
	}
	if res.conflict == "" {
		if action, ok := tomlString(kv["action"]); ok {
			res.conflict = "action " + action
		} else {
			res.conflict = "an unrecognized binding"
		}
	}
}

// stripTOMLComment removes a # comment that isn't inside a string.
func stripTOMLComment(line string) string {
	var quote byte
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && escaped:
			escaped = false
		case quote == '"' && c == '\\':
			escaped = true
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			return line[:i]
		}
	}
	return line
}

// bracketDelta counts unbalanced [ and { outside strings.
func bracketDelta(s string) int {
	d := 0
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && escaped:
			escaped = false
		case quote == '"' && c == '\\':
			escaped = true
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && (c == '[' || c == '{'):
			d++
		case quote == 0 && (c == ']' || c == '}'):
			d--
		}
	}
	return d
}

// tomlString decodes a TOML basic ("...") or literal ('...') string.
func tomlString(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return "", false
	}
	switch {
	case v[0] == '\'' && v[len(v)-1] == '\'':
		return v[1 : len(v)-1], true
	case v[0] == '"' && v[len(v)-1] == '"':
		return unescapeTOML(v[1 : len(v)-1])
	}
	return "", false
}

func unescapeTOML(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			return "", false
		}
		switch s[i] {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case 'e':
			b.WriteByte(0x1b)
		case '"', '\\':
			b.WriteByte(s[i])
		case 'u', 'U':
			n := 4
			if s[i] == 'U' {
				n = 8
			}
			if i+1+n > len(s) {
				return "", false
			}
			r, err := strconv.ParseUint(s[i+1:i+1+n], 16, 32)
			if err != nil {
				return "", false
			}
			b.WriteRune(rune(r))
			i += n
		default:
			return "", false
		}
	}
	return b.String(), true
}
