package sessions

import (
	"strings"
	"testing"
)

func TestContinue(t *testing.T) {
	l := copyConfig(t)
	ix := NewIndex(l, "")
	m, err := ix.Continue("/work/demo")
	if err != nil || m.ID != sidPlain {
		t.Fatalf("continue = %s, %v", m.ID, err)
	}
	if _, err := ix.Continue("/nowhere"); err != ErrNotFound {
		t.Fatalf("empty project err = %v", err)
	}
}

func TestResolve(t *testing.T) {
	l := copyConfig(t)
	ix := NewIndex(l, "")

	r, err := ix.Resolve("/work/demo", "")
	if err != nil || r.Session != nil || r.Query != "" {
		t.Fatalf("empty arg = %+v, %v", r, err)
	}
	// By id, also from another project, and upper case.
	r, err = ix.Resolve("/work/demo", strings.ToUpper(sidOther))
	if err != nil || r.Session == nil || r.Session.ID != sidOther {
		t.Fatalf("by id = %+v, %v", r, err)
	}
	if _, err := ix.Resolve("/work/demo", "99999999-9999-4999-8999-999999999999"); err != ErrNotFound {
		t.Fatalf("missing id err = %v", err)
	}
	// By transcript path.
	p := SessionFile(l.ProjectDir("/work/demo"), sidCompact)
	r, err = ix.Resolve("/work/demo", p)
	if err != nil || r.Session == nil || r.Session.ID != sidCompact {
		t.Fatalf("by path = %+v, %v", r, err)
	}
	// By exact name (case-insensitive).
	r, err = ix.Resolve("/work/demo", "my RENAMED session")
	if err != nil || r.Session == nil || r.Session.ID != sidMessy {
		t.Fatalf("by name = %+v, %v", r, err)
	}
	// A search: matches in this project.
	r, err = ix.Resolve("/work/demo", "build")
	if err != nil || r.Session != nil || r.Query != "build" || len(r.Matches) != 1 || r.Matches[0].ID != sidPlain {
		t.Fatalf("search = %+v, %v", r, err)
	}
	// Nothing here: falls back to every project.
	r, err = ix.Resolve("/work/demo", "hello from")
	if err != nil || len(r.Matches) != 1 || r.Matches[0].ID != sidOther {
		t.Fatalf("global search = %+v, %v", r, err)
	}
	// PR number and branch are searchable.
	if r, _ := ix.Resolve("/work/demo", "#42 feature/x"); len(r.Matches) != 1 || r.Matches[0].ID != sidTools {
		t.Fatalf("pr search = %+v", r.Matches)
	}
}
