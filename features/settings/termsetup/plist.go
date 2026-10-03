package termsetup

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// A minimal XML property-list reader that remembers byte offsets, so edits can be
// spliced into the original text. Everything outside an edit, including <data> blocks
// and Apple's exact indentation, is preserved byte for byte.

type plistNode struct {
	kind        string // dict, array, string, integer, real, true, false, data, date
	depth       int    // 0 for the root value
	start, end  int    // the whole element, [start, end)
	openEnd     int    // end of the start tag
	closeStart  int    // start of the end tag; == openEnd when self-closing
	selfClosing bool
	text        string // character data of scalars
	keys        []string
	vals        []*plistNode
	items       []*plistNode
}

// get returns the value for key in a dict (the last one, as CFPreferences does).
func (n *plistNode) get(key string) *plistNode {
	if n == nil || n.kind != "dict" {
		return nil
	}
	for i := len(n.keys) - 1; i >= 0; i-- {
		if n.keys[i] == key {
			return n.vals[i]
		}
	}
	return nil
}

// isTrue reports a boolean-ish true (<true/> or a non-zero integer).
func (n *plistNode) isTrue() bool {
	if n == nil {
		return false
	}
	switch n.kind {
	case "true":
		return true
	case "integer":
		return strings.TrimSpace(n.text) != "0" && strings.TrimSpace(n.text) != ""
	}
	return false
}

type plistParser struct {
	d *xml.Decoder
}

func (p *plistParser) next() (xml.Token, int, error) {
	start := int(p.d.InputOffset())
	tok, err := p.d.Token()
	if cd, ok := tok.(xml.CharData); ok {
		tok = cd.Copy()
	}
	return tok, start, err
}

// parsePlist parses an XML plist and returns its root value.
func parsePlist(src []byte) (*plistNode, error) {
	p := &plistParser{d: xml.NewDecoder(bytes.NewReader(src))}
	p.d.Strict = true
	inPlist := false
	for {
		tok, start, err := p.next()
		if err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !inPlist {
			if se.Name.Local != "plist" {
				return nil, fmt.Errorf("plist: unexpected root element <%s>", se.Name.Local)
			}
			inPlist = true
			continue
		}
		return p.value(se, start, 0)
	}
}

func (p *plistParser) value(se xml.StartElement, start, depth int) (*plistNode, error) {
	n := &plistNode{kind: se.Name.Local, depth: depth, start: start, openEnd: int(p.d.InputOffset())}
	var pendingKey string
	haveKey := false
	var text strings.Builder
	for {
		tok, tstart, err := p.next()
		if err != nil {
			return nil, fmt.Errorf("plist <%s>: %w", n.kind, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch n.kind {
			case "dict":
				if t.Name.Local == "key" {
					k, err := p.text()
					if err != nil {
						return nil, err
					}
					pendingKey, haveKey = k, true
					continue
				}
				if !haveKey {
					return nil, fmt.Errorf("plist: <%s> in a dict without a <key>", t.Name.Local)
				}
				v, err := p.value(t, tstart, depth+1)
				if err != nil {
					return nil, err
				}
				n.keys = append(n.keys, pendingKey)
				n.vals = append(n.vals, v)
				haveKey = false
			case "array":
				v, err := p.value(t, tstart, depth+1)
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, v)
			default:
				return nil, fmt.Errorf("plist: <%s> inside <%s>", t.Name.Local, n.kind)
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			n.closeStart = tstart
			n.end = int(p.d.InputOffset())
			if n.end == n.openEnd {
				n.selfClosing = true
				n.closeStart = n.openEnd
			}
			if haveKey {
				return nil, fmt.Errorf("plist: key %q has no value", pendingKey)
			}
			if n.kind != "dict" && n.kind != "array" {
				n.text = text.String()
			}
			return n, nil
		}
	}
}

// text reads character data up to the end of the current element.
func (p *plistParser) text() (string, error) {
	var b strings.Builder
	for {
		tok, _, err := p.next()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		case xml.StartElement:
			return "", fmt.Errorf("plist: unexpected <%s> in <key>", t.Name.Local)
		}
	}
}

// splice replaces src[start:end] with text.
type splice struct {
	start, end int
	text       string
}

// applySplices applies non-overlapping splices. Insertions at the same offset keep
// their list order in the output.
func applySplices(src []byte, sp []splice) ([]byte, error) {
	sorted := make([]splice, len(sp))
	for i, s := range sp {
		sorted[len(sp)-1-i] = s
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start > sorted[j].start })
	out := append([]byte(nil), src...)
	limit := len(src)
	for _, s := range sorted {
		if s.start < 0 || s.end < s.start || s.end > limit {
			return nil, errors.New("overlapping or out-of-range edits")
		}
		out = append(out[:s.start], append([]byte(s.text), out[s.end:]...)...)
		limit = s.start
	}
	return out, nil
}

const plistIndent = "\t"

func indent(depth int) string { return strings.Repeat(plistIndent, depth) }

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plistKey formats one dict entry whose value fits on a line (e.g. "<true/>").
func plistKey(depth int, key, valueXML string) string {
	return indent(depth) + "<key>" + xmlEscape(key) + "</key>\n" + indent(depth) + valueXML + "\n"
}

// plistDictEntry formats a dict entry whose value is a dict of single-line values.
func plistDictEntry(depth int, key string, pairs [][2]string) string {
	var b strings.Builder
	b.WriteString(indent(depth) + "<key>" + xmlEscape(key) + "</key>\n")
	b.WriteString(indent(depth) + "<dict>\n")
	for _, kv := range pairs {
		b.WriteString(plistKey(depth+1, kv[0], kv[1]))
	}
	b.WriteString(indent(depth) + "</dict>\n")
	return b.String()
}

// insertIntoDict returns a splice that appends entries (formatted for dict.depth+1,
// each line ending in "\n") at the end of dict.
func insertIntoDict(src []byte, dict *plistNode, entries string) splice {
	if dict.selfClosing {
		return splice{dict.start, dict.end, "<dict>\n" + entries + indent(dict.depth) + "</dict>"}
	}
	inner := src[dict.openEnd:dict.closeStart]
	if nl := bytes.LastIndexByte(inner, '\n'); nl >= 0 && isBlank(inner[nl+1:]) {
		at := dict.openEnd + nl + 1
		return splice{at, at, entries}
	}
	if isBlank(inner) {
		return splice{dict.openEnd, dict.closeStart, "\n" + entries + indent(dict.depth)}
	}
	return splice{dict.closeStart, dict.closeStart, "\n" + entries + indent(dict.depth)}
}

func isBlank(b []byte) bool {
	return len(bytes.TrimSpace(b)) == 0
}
