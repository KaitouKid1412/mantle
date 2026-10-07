package research

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
)

func writeSession(t *testing.T, es ...ent) string {
	t.Helper()
	var b strings.Builder
	for _, e := range es {
		e["sessionId"] = "0b9f2c55-3c8e-4d2e-9b7a-6f1e2d3c4b5a"
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	p := filepath.Join(t.TempDir(), "0b9f2c55-3c8e-4d2e-9b7a-6f1e2d3c4b5a.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelectBranch(t *testing.T) {
	// q1 → (q2 | q3); q3 is newest. Each answer is followed by an attachment, as the
	// engine writes it.
	path := writeSession(t,
		prompt("q1", "", "first"), answer("a1", "q1", "A1"),
		ent{"type": "attachment", "uuid": "t1", "parentUuid": "a1"},
		prompt("q2", "t1", "second"), answer("a2", "q2", "A2"),
		ent{"type": "attachment", "uuid": "t2", "parentUuid": "a2"},
		prompt("q3", "t1", "third"), answer("a3", "q3", "A3"),
		ent{"type": "last-prompt", "lastPrompt": "third", "leafUuid": "a3"},
	)
	tr, _ := sessions.Load(path)
	if l := tr.ActiveLeaf(); l == nil || l.Entry.UUID != "a3" {
		t.Fatalf("before: active leaf %v", l)
	}

	at, err := SelectBranch(path, "t2", "second")
	if err != nil || at != "a2" {
		t.Fatalf("SelectBranch = %q, %v; want the message above the attachment, a2", at, err)
	}
	tr, _ = sessions.Load(path)
	if l := tr.ActiveLeaf(); l == nil || l.Entry.UUID != "t2" {
		t.Fatalf("after: active leaf %v, want q2's branch (t2)", l)
	}
	if tr.Meta.LeafUUID != "a2" {
		t.Fatalf("last-prompt leaf %q", tr.Meta.LeafUUID)
	}
	data, _ := os.ReadFile(path)
	last := strings.TrimSpace(string(data)[strings.LastIndex(strings.TrimRight(string(data), "\n"), "\n")+1:])
	var rec map[string]any
	if err := json.Unmarshal([]byte(last), &rec); err != nil || rec["explicit"] != true || rec["lastPrompt"] != "second" ||
		rec["sessionId"] != "0b9f2c55-3c8e-4d2e-9b7a-6f1e2d3c4b5a" {
		t.Fatalf("record %s (%v)", last, err)
	}
	// The tree is unchanged: the record is metadata.
	if tree := Build(tr); tree.Len() != 3 || len(tree.Node("q1").Children) != 2 {
		t.Fatalf("tree changed: %d nodes", tree.Len())
	}
}

func TestSelectBranchErrors(t *testing.T) {
	path := writeSession(t, prompt("q1", "", "first"), answer("a1", "q1", "A1"))
	if _, err := SelectBranch(path, "nope", "x"); !errors.Is(err, ErrNoEntry) {
		t.Fatalf("unknown entry: %v", err)
	}
	if _, err := SelectBranch(filepath.Join(t.TempDir(), "missing.jsonl"), "a1", "x"); err == nil {
		t.Fatal("missing file: no error")
	}
	// A file without a trailing newline gets one before the record.
	data, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(strings.TrimRight(string(data), "\n")), 0o644)
	if _, err := SelectBranch(path, "a1", "first"); err != nil {
		t.Fatal(err)
	}
	if tr, err := sessions.Load(path); err != nil || tr.Meta.LeafUUID != "a1" {
		t.Fatalf("reload: %v %q", err, tr.Meta.LeafUUID)
	}
}
