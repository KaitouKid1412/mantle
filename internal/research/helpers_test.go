package research

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
)

// ent is one transcript entry for a hand-built session.
type ent map[string]any

func prompt(uuid, parent, text string) ent {
	return ent{"type": "user", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "user", "content": text}}
}

func answer(uuid, parent, text string) ent {
	return ent{"type": "assistant", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}}}
}

func toolCall(uuid, parent, id string) ent {
	return ent{"type": "assistant", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": id, "name": "Read", "input": map[string]any{}}}}}
}

func toolResult(uuid, parent, id string) ent {
	return ent{"type": "user", "uuid": uuid, "parentUuid": parent,
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": id, "content": "ok"}}}}
}

func (e ent) with(k string, v any) ent { e[k] = v; return e }

// transcript parses entries (in file order) as a session file.
func transcript(t *testing.T, es ...ent) *sessions.Transcript {
	t.Helper()
	var b strings.Builder
	for _, e := range es {
		if _, ok := e["sessionId"]; !ok {
			e["sessionId"] = "sess"
		}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tr, err := sessions.Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func fixture(t *testing.T) *sessions.Transcript {
	t.Helper()
	tr, err := sessions.Load(filepath.Join("..", "..", "testdata", "fixtures", "13", "branching.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// shape renders a tree as "id(state,leaf)[children]" for compact comparisons. Flags:
// "c" compacted, "s" synthetic.
func shape(t *Tree) string {
	var b strings.Builder
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for i, n := range ns {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(n.ID)
			b.WriteString("(" + n.State.String() + "," + n.LeafUUID)
			if n.Compacted {
				b.WriteString(",c")
			}
			if n.Synthetic {
				b.WriteString(",s")
			}
			b.WriteByte(')')
			if len(n.Children) > 0 {
				b.WriteByte('[')
				walk(n.Children)
				b.WriteByte(']')
			}
		}
	}
	walk(t.Roots)
	return b.String()
}
