package normalize

import (
	"path/filepath"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// The full normalizer tests run through features/sessions' forwarding Normalize;
// this covers the package on its own, including an off-branch leaf.
func TestNormalizeLeaf(t *testing.T) {
	const sid = "11111111-1111-4111-8111-111111111111"
	dir := filepath.Join("..", "..", "..", "testdata", "fixtures", "06", "claude", "projects", "-work-demo")
	tr, err := sessions.Load(sessions.SessionFile(dir, sid))
	if err != nil {
		t.Fatal(err)
	}
	all := Normalize(tr, Options{EngineID: ext.MainEngine})
	at := "000001" // first answer's turn_duration entry: 1s000001-…
	part := Normalize(tr, Options{EngineID: ext.MainEngine, Leaf: "1s" + at + "-0000-4000-8000-000000000000"})
	if len(part) == 0 || len(part) >= len(all) {
		t.Fatalf("leaf branch has %d items, full %d", len(part), len(all))
	}
	last := part[len(part)-1]
	if last.Key != KeyResult || last.EngineID != ext.MainEngine || all[0].Key != ext.KeyUserPrompt {
		t.Fatalf("last = %+v, first = %+v", last, all[0])
	}
}
