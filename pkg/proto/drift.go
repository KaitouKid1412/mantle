package proto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Unmodelled re-encodes a decoded event and compares it with the line it came from.
// It returns the JSON paths whose non-zero values the typed form drops or changes
// (for example "message.content[0].citations"). Raw-only types (*Unknown) report
// nothing. Used by round-trip tests and drift reports.
func Unmodelled(ev Event) ([]string, error) {
	raw := ev.Env().Raw
	if len(raw) == 0 {
		return nil, fmt.Errorf("proto: event has no Raw")
	}
	if _, ok := ev.(*Unknown); ok {
		return nil, nil
	}
	enc, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	var a, b any
	if err := unmarshalNumber(raw, &a); err != nil {
		return nil, err
	}
	if err := unmarshalNumber(enc, &b); err != nil {
		return nil, err
	}
	var out []string
	diffJSON("", a, b, &out)
	sort.Strings(out)
	return out, nil
}

func unmarshalNumber(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(v)
}

// diffJSON records paths where orig has a non-zero value that enc lacks or differs.
func diffJSON(path string, orig, enc any, out *[]string) {
	switch o := orig.(type) {
	case map[string]any:
		e, _ := enc.(map[string]any)
		for k, ov := range o {
			p := k
			if path != "" {
				p = path + "." + k
			}
			ev, ok := e[k]
			if !ok {
				if !isZeroJSON(ov) {
					*out = append(*out, p)
				}
				continue
			}
			diffJSON(p, ov, ev, out)
		}
	case []any:
		e, ok := enc.([]any)
		if !ok || len(e) != len(o) {
			if !(isZeroJSON(o) && isZeroJSON(enc)) {
				*out = append(*out, path)
			}
			return
		}
		for i := range o {
			diffJSON(fmt.Sprintf("%s[%d]", path, i), o[i], e[i], out)
		}
	default:
		if isZeroJSON(orig) && isZeroJSON(enc) {
			return
		}
		if !jsonScalarEqual(orig, enc) {
			*out = append(*out, path)
		}
	}
}

func jsonScalarEqual(a, b any) bool {
	if an, ok := a.(json.Number); ok {
		bn, ok := b.(json.Number)
		if !ok {
			return false
		}
		af, err1 := an.Float64()
		bf, err2 := bn.Float64()
		return err1 == nil && err2 == nil && af == bf
	}
	return a == b
}

// isZeroJSON reports whether v is null, false, 0, "", or an empty array or object.
func isZeroJSON(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case string:
		return x == ""
	case json.Number:
		f, err := x.Float64()
		return err == nil && f == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}
