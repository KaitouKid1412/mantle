package settingsfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Order remembers the key order of every object in a parsed document, so Encode can
// write a modified document back with its keys where the user put them.
type Order struct {
	Keys  []string          // object keys in file order
	Child map[string]*Order // order of nested values, by key
	Elems []*Order          // order of array elements, by index
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

func decodeValue(dec *json.Decoder) (any, *Order, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return tok, nil, nil // string, json.Number, bool or nil
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

// Encode writes doc as two-space-indented JSON with a trailing newline. Keys known to
// order keep their position; new keys follow in sorted order. Strings are not
// HTML-escaped (matching how Claude Code writes its files).
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
		b.Write(raw)
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
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
}
