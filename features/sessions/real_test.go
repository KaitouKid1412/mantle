package sessions

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// TestNormalizeRealSessions normalizes every local session read-only and logs how many
// items of each key it produced (never content).
//
//	MANTLE_TEST_REAL_SESSIONS=1 go test -run TestNormalizeRealSessions -v ./features/sessions
func TestNormalizeRealSessions(t *testing.T) {
	if os.Getenv("MANTLE_TEST_REAL_SESSIONS") != "1" {
		t.Skip("set MANTLE_TEST_REAL_SESSIONS=1 to normalize local sessions")
	}
	l := sessions.DefaultLayout()
	files, _ := filepath.Glob(filepath.Join(l.ProjectsDir(), "*", "*.jsonl"))
	keys := map[ext.ContentKey]int{}
	var nested, interrupted int
	for _, f := range files {
		tr, err := sessions.Load(f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		id := sessions.SessionIDFromPath(f)
		items := Normalize(tr, NormalizeOptions{
			EngineID:     ext.MainEngine,
			SubagentsDir: sessions.SubagentsDir(filepath.Dir(f), id),
		})
		seen := map[string]bool{}
		for _, it := range items {
			if seen[it.ID] {
				t.Errorf("%s: duplicate item id", filepath.Base(f))
			}
			seen[it.ID] = true
			if it.ParentID != "" {
				nested++
				if !seen[it.ParentID] {
					t.Errorf("%s: child before its parent", filepath.Base(f))
				}
			}
			if it.State == ext.Interrupted {
				interrupted++
			}
			keys[it.Key]++
		}
	}
	var lines []string
	for k, n := range keys {
		lines = append(lines, string(k)+"="+itoa(n))
	}
	sort.Strings(lines)
	t.Logf("%d sessions; nested=%d interrupted=%d; %v", len(files), nested, interrupted, lines)
}
