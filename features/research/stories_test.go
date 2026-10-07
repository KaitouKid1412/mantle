package research

import (
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// TestStories renders every story at 60/100/160 and compares with goldens
// (go test ./features/research -run TestStories -update rewrites them).
func TestStories(t *testing.T) {
	r, err := exttest.Setup(featureForTest())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Stories) == 0 {
		t.Fatal("no stories")
	}
	for _, s := range r.Stories {
		t.Run(strings.ReplaceAll(s.ID, "/", "_"), func(t *testing.T) {
			testkit.RunStory(t, s)
		})
	}
}
