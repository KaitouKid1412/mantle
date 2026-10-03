package settings

import (
	"testing"

	"github.com/KaitouKid1412/mantle/internal/testkit"
)

// Every registered story renders within 60, 100 and 160 columns and matches its golden.
func TestStories(t *testing.T) {
	g := newRig(t)
	if len(g.r.Stories) == 0 {
		t.Fatal("no stories registered")
	}
	seen := map[string]bool{}
	for _, s := range g.r.Stories {
		if seen[s.ID] {
			t.Errorf("duplicate story %s", s.ID)
		}
		seen[s.ID] = true
		t.Run(s.ID, func(t *testing.T) { testkit.RunStory(t, s) })
	}
}
