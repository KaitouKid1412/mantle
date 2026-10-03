package config

// Order-preserving JSON for settings files, adapted from plan 08's settingsfile
// package: a user's hand-edited file keeps its key order, two-space indentation and
// number spelling when mantle changes one key.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrInvalid is returned when an existing file is not a JSON object; mantle refuses to
// overwrite it (Claude Code silently ignores invalid settings files in -p, so a
// clobbered file would lose the user's settings).
var ErrInvalid = errors.New("settings file is not a valid JSON object; fix or remove it first")

// Order remembers the key order of every object in a parsed document.
type Order struct {
	Keys  []string
	Child map[string]*Order
	Elems []*Order
}

func (o *Order) child(k string) *Order {
	if o == nil {
		return nil
	}
	return o.Child[k]
}

func (o *Order) elem(i int) *Order {
	if o == nil || i >= len(o.Elems) {
		return nil
	}
	return o.Elems[i]
}

// Decode parses a settings document. Empty input is an empty object. Numbers are kept
// as json.Number.
func Decode(raw []byte) (map[string]any, *Order, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, &Order{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, order, err := decodeValue(dec)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err == nil {
		return nil, nil, fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: top level is not an object", ErrInvalid)
	}
	return m, order, nil
}

func decodeValue(dec *json.Decoder) (any, *Order, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil, nil
	}
	switch d {
	case '{':
		m := map[string]any{}
		o := &Order{Child: map[string]*Order{}}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, nil, err
			}
			key, ok := kt.(string)
			if !ok {
				return nil, nil, fmt.Errorf("object key is %T", kt)
			}
			v, co, err := decodeValue(dec)
			if err != nil {
				return nil, nil, err
			}
			if _, dup := m[key]; !dup {
				o.Keys = append(o.Keys, key)
			}
			m[key] = v
			o.Child[key] = co
		}
		if _, err := dec.Token(); err != nil {
			return nil, nil, err
		}
		return m, o, nil
	case '[':
		arr := []any{}
		o := &Order{}
		for dec.More() {
			v, co, err := decodeValue(dec)
			if err != nil {
				return nil, nil, err
			}
			arr = append(arr, v)
			o.Elems = append(o.Elems, co)
		}
		if _, err := dec.Token(); err != nil {
			return nil, nil, err
		}
		return arr, o, nil
	}
	return nil, nil, fmt.Errorf("unexpected %v", d)
}

// Encode writes doc as two-space-indented JSON with a trailing newline. Known keys keep
// their position; new keys follow in sorted order. No HTML escaping.
func Encode(doc map[string]any, order *Order) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, doc, order, 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

func encode(b *bytes.Buffer, v any, o *Order, depth int) error {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteString("{\n")
		keys := orderedKeys(t, o)
		for i, k := range keys {
			indent(b, depth+1)
			if err := encodeString(b, k); err != nil {
				return err
			}
			b.WriteString(": ")
			if err := encode(b, t[k], o.child(k), depth+1); err != nil {
				return err
			}
			if i < len(keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		indent(b, depth)
		b.WriteByte('}')
	case []any:
		if len(t) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteString("[\n")
		for i, e := range t {
			indent(b, depth+1)
			if err := encode(b, e, o.elem(i), depth+1); err != nil {
				return err
			}
			if i < len(t)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		indent(b, depth)
		b.WriteByte(']')
	case []string:
		arr := make([]any, len(t))
		for i, s := range t {
			arr[i] = s
		}
		return encode(b, arr, o, depth)
	case string:
		return encodeString(b, t)
	case json.Number:
		b.WriteString(t.String())
	case int:
		b.WriteString(strconv.Itoa(t))
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case nil:
		b.WriteString("null")
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return err
		}
		// Re-decode so maps and slices inside get the same formatting.
		var generic any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&generic); err != nil {
			return err
		}
		if _, isScalar := generic.(map[string]any); !isScalar {
			if _, isArr := generic.([]any); !isArr {
				b.Write(raw)
				return nil
			}
		}
		return encode(b, generic, o, depth)
	}
	return nil
}

func orderedKeys(m map[string]any, o *Order) []string {
	keys := make([]string, 0, len(m))
	seen := make(map[string]bool, len(m))
	if o != nil {
		for _, k := range o.Keys {
			if _, ok := m[k]; ok && !seen[k] {
				keys = append(keys, k)
				seen[k] = true
			}
		}
	}
	var extra []string
	for k := range m {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func encodeString(b *bytes.Buffer, s string) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	b.WriteString(strings.TrimSuffix(tmp.String(), "\n"))
	return nil
}

func indent(b *bytes.Buffer, depth int) {
	for range depth {
		b.WriteString("  ")
	}
}
