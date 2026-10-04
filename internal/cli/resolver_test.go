package cli

import (
	"path/filepath"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/sessions"
)

func TestIndexResolver(t *testing.T) {
	cfg := filepath.Join("..", "..", "testdata", "fixtures", "06", "claude")
	r := IndexResolver{Index: sessions.NewIndex(sessions.Layout{ConfigDir: cfg}, "")}

	if id, err := r.Continue("/work/demo"); err != nil || !sessions.ValidID(id) {
		t.Errorf("continue: %q %v", id, err)
	}
	if _, err := r.Continue("/nowhere"); err == nil {
		t.Error("continue in an empty project should fail")
	}
	if id, q, err := r.Resolve("/work/demo", "77777777-7777-4777-8777-777777777777"); err != nil || id != "77777777-7777-4777-8777-777777777777" || q != "" {
		t.Errorf("by id: %q %q %v", id, q, err)
	}
	if id, q, err := r.Resolve("/work/demo", "build"); err != nil || id != "" || q != "build" {
		t.Errorf("search: %q %q %v", id, q, err)
	}
	if _, _, err := r.Resolve("/work/demo", "99999999-9999-4999-8999-999999999999"); err == nil {
		t.Error("missing id should fail")
	}

	// Through Startup: -c resumes the latest session, -r with a search opens the picker.
	p, _ := Parse([]string{"-c"})
	st, err := p.Startup("/work/demo", r)
	if err != nil || !sessions.ValidID(st.Spawn.Resume) || st.Spawn.Continue {
		t.Errorf("-c: %+v %v", st.Spawn, err)
	}
	p, _ = Parse([]string{"-r", "build"})
	if st, err := p.Startup("/work/demo", r); err != nil || !st.Picker || st.PickerQuery != "build" {
		t.Errorf("-r build: %+v %v", st, err)
	}
	if DefaultResolver().Index == nil {
		t.Error("DefaultResolver has no index")
	}
}
