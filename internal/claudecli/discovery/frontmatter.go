package discovery

import (
	"fmt"
	"strconv"
	"strings"
)

// Frontmatter is the YAML header of a Markdown file (agents, skills, commands,
// rules). The parser handles the subset these files use: scalars (plain and
// quoted), block scalars (| and >), inline lists, block lists and nested
// mappings. Values are string, bool, []any or map[string]any.
type Frontmatter struct {
	Fields map[string]any
	Keys   []string // top-level keys in file order
	Body   string   // text after the closing delimiter
}

// String returns a scalar field as text ("" when missing or not a scalar).
func (f Frontmatter) String(key string) string {
	switch v := f.Fields[key].(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// List returns a field as a list: a YAML list, or a comma-separated scalar
// ("Read, Grep" → [Read Grep]), which is how tool lists are often written.
func (f Frontmatter) List(key string) []string {
	switch v := f.Fields[key].(type) {
	case string:
		var out []string
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Bool returns a boolean field; ok is false when it is missing or not boolean.
func (f Frontmatter) Bool(key string) (value, ok bool) {
	switch v := f.Fields[key].(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(v) {
		case "true", "yes", "on":
			return true, true
		case "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

// ParseFrontmatter splits a Markdown document into frontmatter and body. A
// document without a leading "---" line has empty Fields and the whole text as
// Body. A malformed header returns the fields parsed so far and an error.
func ParseFrontmatter(doc string) (Frontmatter, error) {
	fm := Frontmatter{Fields: map[string]any{}}
	doc = strings.TrimPrefix(doc, "\ufeff")
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		fm.Body = doc
		return fm, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimRight(lines[i], " \t"); t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		fm.Body = doc
		return fm, fmt.Errorf("frontmatter: missing closing ---")
	}
	fm.Body = strings.Join(lines[end+1:], "\n")
	p := &yamlParser{}
	for _, l := range lines[1:end] {
		p.add(l)
	}
	v, err := p.parseBlock(0)
	if m, ok := v.(map[string]any); ok {
		fm.Fields = m
		fm.Keys = p.topKeys
	}
	if err == nil && p.pos < len(p.lines) {
		err = fmt.Errorf("frontmatter: unexpected line %q", p.lines[p.pos].text)
	}
	return fm, err
}

type yamlLine struct {
	indent int
	text   string // trimmed of indentation and trailing space
	raw    string
}

type yamlParser struct {
	lines   []yamlLine
	pos     int
	topKeys []string
	depth   int
}

func (p *yamlParser) add(raw string) {
	trimmed := strings.TrimLeft(raw, " \t")
	text := strings.TrimRight(trimmed, " \t")
	if text == "" || strings.HasPrefix(text, "#") {
		// Blank and comment lines matter only inside block scalars; keep blanks.
		if text == "" {
			p.lines = append(p.lines, yamlLine{indent: -1, raw: raw})
		}
		return
	}
	p.lines = append(p.lines, yamlLine{indent: len(raw) - len(trimmed), text: text, raw: raw})
}

func (p *yamlParser) skipBlank() {
	for p.pos < len(p.lines) && p.lines[p.pos].indent < 0 {
		p.pos++
	}
}

// parseBlock parses the mapping or sequence starting at the current line, whose
// lines are indented at least minIndent.
func (p *yamlParser) parseBlock(minIndent int) (any, error) {
	p.skipBlank()
	if p.pos >= len(p.lines) || p.lines[p.pos].indent < minIndent {
		return nil, nil
	}
	if p.depth > 32 {
		return nil, fmt.Errorf("frontmatter: nested too deeply")
	}
	p.depth++
	defer func() { p.depth-- }()
	first := p.lines[p.pos]
	if first.text == "-" || strings.HasPrefix(first.text, "- ") {
		return p.parseSeq(first.indent)
	}
	return p.parseMap(first.indent)
}

func (p *yamlParser) parseMap(indent int) (any, error) {
	m := map[string]any{}
	top := p.depth == 1
	for {
		p.skipBlank()
		if p.pos >= len(p.lines) || p.lines[p.pos].indent != indent {
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				return m, fmt.Errorf("frontmatter: bad indentation at %q", p.lines[p.pos].text)
			}
			return m, nil
		}
		l := p.lines[p.pos]
		if strings.HasPrefix(l.text, "- ") || l.text == "-" {
			return m, nil
		}
		key, rest, ok := splitKey(l.text)
		if !ok {
			return m, fmt.Errorf("frontmatter: expected key: value, got %q", l.text)
		}
		p.pos++
		v, err := p.parseValue(rest, indent)
		if err != nil {
			return m, err
		}
		if _, dup := m[key]; !dup && top {
			p.topKeys = append(p.topKeys, key)
		}
		m[key] = v
	}
}

func (p *yamlParser) parseSeq(indent int) (any, error) {
	var seq []any
	for {
		p.skipBlank()
		if p.pos >= len(p.lines) || p.lines[p.pos].indent != indent {
			return seq, nil
		}
		l := p.lines[p.pos]
		if l.text != "-" && !strings.HasPrefix(l.text, "- ") {
			return seq, nil
		}
		item := strings.TrimSpace(strings.TrimPrefix(l.text, "-"))
		p.pos++
		if item == "" {
			v, err := p.parseBlock(indent + 1)
			if err != nil {
				return seq, err
			}
			seq = append(seq, v)
			continue
		}
		if key, rest, ok := splitKey(item); ok && !isQuoted(item) {
			// "- key: value" starts a mapping whose other keys are indented to
			// line up with "key".
			childIndent := indent + (len(l.text) - len(item))
			m := map[string]any{}
			v, err := p.parseValue(rest, childIndent)
			if err != nil {
				return seq, err
			}
			m[key] = v
			p.skipBlank()
			if p.pos < len(p.lines) && p.lines[p.pos].indent == childIndent {
				more, err := p.parseMap(childIndent)
				if err != nil {
					return seq, err
				}
				for k, v := range more.(map[string]any) {
					m[k] = v
				}
			}
			seq = append(seq, m)
			continue
		}
		seq = append(seq, scalar(item))
	}
}

// parseValue parses what follows "key:" on a line owned by a mapping at indent.
func (p *yamlParser) parseValue(rest string, indent int) (any, error) {
	rest = strings.TrimSpace(rest)
	switch {
	case rest == "":
		p.skipBlank()
		if p.pos < len(p.lines) && (p.lines[p.pos].indent > indent ||
			(p.lines[p.pos].indent == indent && strings.HasPrefix(p.lines[p.pos].text, "- "))) {
			return p.parseBlock(p.lines[p.pos].indent)
		}
		return "", nil
	case rest[0] == '|' || rest[0] == '>':
		return p.blockScalar(rest[0] == '>', rest[1:], indent), nil
	case rest[0] == '[':
		return inlineList(rest), nil
	case rest[0] == '{':
		return rest, nil // inline mappings are rare here; keep the text
	}
	// A scalar may continue on more-indented lines; YAML folds them with spaces.
	for p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
		rest += " " + p.lines[p.pos].text
		p.pos++
	}
	return scalar(rest), nil
}

func (p *yamlParser) blockScalar(folded bool, chomp string, indent int) string {
	var lines []string
	blockIndent := -1
	for p.pos < len(p.lines) {
		l := p.lines[p.pos]
		if l.indent >= 0 && l.indent <= indent {
			break
		}
		if l.indent < 0 {
			lines = append(lines, "")
			p.pos++
			continue
		}
		if blockIndent < 0 {
			blockIndent = l.indent
		}
		cut := blockIndent
		if cut > len(l.raw) {
			cut = len(l.raw)
		}
		lines = append(lines, strings.TrimRight(l.raw[cut:], " \t"))
		p.pos++
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var s string
	if folded {
		var b strings.Builder
		for i, l := range lines {
			switch {
			case i == 0:
			case l == "" || lines[i-1] == "":
				b.WriteByte('\n')
			default:
				b.WriteByte(' ')
			}
			b.WriteString(l)
		}
		s = b.String()
	} else {
		s = strings.Join(lines, "\n")
	}
	if !strings.Contains(chomp, "-") && s != "" {
		s += "\n"
	}
	return s
}

// splitKey splits "key: value" (or "key:") at the first ": " outside quotes.
func splitKey(s string) (key, rest string, ok bool) {
	if s == "" || s[0] == '"' || s[0] == '\'' {
		if q, n := unquote(s); n > 0 {
			after := strings.TrimLeft(s[n:], " ")
			if strings.HasPrefix(after, ":") {
				return q, after[1:], true
			}
		}
		return "", "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
			key = strings.TrimSpace(s[:i])
			return key, s[i+1:], key != ""
		}
	}
	return "", "", false
}

func isQuoted(s string) bool { return s != "" && (s[0] == '"' || s[0] == '\'') }

func scalar(s string) any {
	s = strings.TrimSpace(s)
	if isQuoted(s) {
		if q, n := unquote(s); n > 0 {
			return q
		}
		return s
	}
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	switch s {
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	case "~", "null", "Null", "NULL":
		return ""
	}
	return s
}

// unquote reads a quoted scalar at the start of s and returns its value and the
// number of bytes consumed (0 if unterminated).
func unquote(s string) (string, int) {
	if s == "" {
		return "", 0
	}
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '\'' && c == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i++
				continue
			}
			return b.String(), i + 1
		case q == '"' && c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
		case q == '"' && c == '"':
			return b.String(), i + 1
		default:
			b.WriteByte(c)
		}
	}
	return "", 0
}

func inlineList(s string) []any {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	var out []any
	for _, part := range splitOutsideQuotes(s, ',') {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, scalar(part))
		}
	}
	return out
}

func splitOutsideQuotes(s string, sep byte) []string {
	var parts []string
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == sep:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}
