package termsetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// JSON with comments and trailing commas, as VS Code writes keybindings.json. Edits are
// textual insertions so the user's comments and formatting survive.

// stripComments blanks out // and /* */ comments, keeping the length and every newline,
// so offsets in the result match the source.
func stripComments(src []byte) []byte {
	out := append([]byte(nil), src...)
	inString, escaped := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			j := i
			for ; j < len(out); j++ {
				if j > i+2 && out[j-1] == '*' && out[j] == '/' {
					break
				}
			}
			end := min(j+1, len(out))
			for k := i; k < end; k++ {
				if out[k] != '\n' {
					out[k] = ' '
				}
			}
			i = end - 1
		}
	}
	return out
}

// dropTrailingCommas blanks commas that directly precede ] or } in comment-free JSON.
func dropTrailingCommas(b []byte) []byte {
	out := append([]byte(nil), b...)
	inString, escaped := false, false
	for i, c := range out {
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c != ',' {
			continue
		}
		j := i + 1
		for j < len(out) && isSpace(out[j]) {
			j++
		}
		if j < len(out) && (out[j] == ']' || out[j] == '}') {
			out[i] = ' '
		}
	}
	return out
}

func parseJSONC(src []byte, v any) error {
	return json.Unmarshal(dropTrailingCommas(stripComments(src)), v)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// arraySpan finds the top-level array in comment-free JSON: the offsets of '[' and the
// matching ']'.
func arraySpan(stripped []byte) (open, close int, err error) {
	open = -1
	for i, c := range stripped {
		if isSpace(c) {
			continue
		}
		if c != '[' {
			return 0, 0, errors.New("the file doesn't hold a JSON array")
		}
		open = i
		break
	}
	if open < 0 {
		return 0, 0, errors.New("the file is empty")
	}
	depth, inString, escaped := 0, false, false
	for i := open; i < len(stripped); i++ {
		c := stripped[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return open, i, nil
			}
		}
	}
	return 0, 0, errors.New("the array is not closed")
}

// appendToArray inserts an element at the end of the top-level array of a JSONC
// document. element receives the indentation used by existing elements and returns
// the element's text, every line already indented, without a trailing newline.
func appendToArray(src []byte, element func(indent string) string) ([]byte, error) {
	stripped := stripComments(src)
	open, closeAt, err := arraySpan(stripped)
	if err != nil {
		return nil, err
	}
	last := -1
	for i := closeAt - 1; i > open; i-- {
		if !isSpace(stripped[i]) {
			last = i
			break
		}
	}
	ind := "    "
	if last >= 0 {
		first := open + 1
		for first < closeAt && isSpace(stripped[first]) {
			first++
		}
		lineStart := bytes.LastIndexByte(src[:first], '\n') + 1
		if lead := src[lineStart:first]; lineStart > open && len(bytes.Trim(lead, " \t")) == 0 && len(lead) > 0 {
			ind = string(lead)
		}
	}
	var sp []splice
	if last >= 0 && stripped[last] != ',' {
		sp = append(sp, splice{last + 1, last + 1, ","})
	}
	text := element(ind)
	lineStart := bytes.LastIndexByte(src[:closeAt], '\n') + 1
	if lineStart > open && len(bytes.Trim(src[lineStart:closeAt], " \t")) == 0 {
		sp = append(sp, splice{lineStart, lineStart, text + "\n"})
	} else {
		sp = append(sp, splice{closeAt, closeAt, "\n" + text + "\n"})
	}
	return applySplices(src, sp)
}

// indentLines prefixes every line of s with ind.
func indentLines(s, ind string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ind + l
	}
	return strings.Join(lines, "\n")
}
