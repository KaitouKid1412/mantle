package perms

import (
	"errors"
	"reflect"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

func apply(t *testing.T, p patch.Patch, err error, doc map[string]any) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Apply(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func list(doc map[string]any, path ...string) []string {
	return strings_(doc, path...)
}

func TestAddRuleMovesBetweenLists(t *testing.T) {
	doc := loadDoc(t, "project.json") // ask has Bash(git push:*)
	p, err := AddRule(patch.Project, Deny, "Bash(git push:*)")
	out := apply(t, p, err, doc)
	if got := list(out, "permissions", "ask"); len(got) != 0 {
		t.Errorf("ask still has %q", got)
	}
	if got := list(out, "permissions", "deny"); !reflect.DeepEqual(got, []string{"Bash(git push:*)"}) {
		t.Errorf("deny: %q", got)
	}
	// Unrelated data survives.
	if got := list(out, "permissions", "allow"); len(got) != 2 {
		t.Errorf("allow changed: %q", got)
	}
	if out["permissions"].(map[string]any)["defaultMode"] != "plan" {
		t.Error("defaultMode lost")
	}
	// Adding again is a no-op.
	p, err = AddRule(patch.Project, Deny, "Bash(git push:*)")
	again := apply(t, p, err, out)
	if got := list(again, "permissions", "deny"); len(got) != 1 {
		t.Errorf("duplicate added: %q", got)
	}
}

func TestAddRuleToEmptyDoc(t *testing.T) {
	p, err := AddRule(patch.User, Allow, "mcp__github__*")
	out := apply(t, p, err, nil)
	if got := list(out, "permissions", "allow"); !reflect.DeepEqual(got, []string{"mcp__github__*"}) {
		t.Errorf("allow: %q", got)
	}
	if _, ok := patch.Get(out, "permissions", "deny"); ok {
		t.Error("removing from a missing list created it")
	}
}

func TestRemoveRule(t *testing.T) {
	doc := loadDoc(t, "local.json")
	// A rule that does not parse can still be removed.
	p, err := RemoveRule(patch.Local, Allow, "Bash(oops")
	out := apply(t, p, err, doc)
	if got := list(out, "permissions", "allow"); !reflect.DeepEqual(got, []string{"Bash(make build)"}) {
		t.Errorf("allow: %q", got)
	}
}

func TestDirectories(t *testing.T) {
	doc := loadDoc(t, "user.json")
	p, err := AddDirectory(patch.User, "  /work/other  ")
	out := apply(t, p, err, doc)
	if got := list(out, "permissions", "additionalDirectories"); !reflect.DeepEqual(got, []string{"~/shared/docs", "/work/other"}) {
		t.Errorf("add: %q", got)
	}
	p, err = RemoveDirectory(patch.User, "~/shared/docs")
	out = apply(t, p, err, out)
	if got := list(out, "permissions", "additionalDirectories"); !reflect.DeepEqual(got, []string{"/work/other"}) {
		t.Errorf("remove: %q", got)
	}
	if _, err := AddDirectory(patch.User, "   "); !errors.Is(err, ErrEmptyDir) {
		t.Errorf("blank dir: %v", err)
	}
}

func TestDefaultMode(t *testing.T) {
	doc := loadDoc(t, "user.json")
	p, err := SetDefaultMode(patch.User, "manual")
	out := apply(t, p, err, doc)
	if v, _ := patch.Get(out, "permissions", "defaultMode"); v != "default" {
		t.Errorf("manual alias: %v", v)
	}
	for _, m := range Modes {
		if _, err := SetDefaultMode(patch.Local, m); err != nil {
			t.Errorf("mode %s: %v", m, err)
		}
	}
	if _, err := SetDefaultMode(patch.User, "yolo"); err == nil {
		t.Error("unknown mode accepted")
	}
	p, err = ClearDefaultMode(patch.User)
	out = apply(t, p, err, out)
	if _, ok := patch.Get(out, "permissions", "defaultMode"); ok {
		t.Error("defaultMode not cleared")
	}
	if got := list(out, "permissions", "allow"); len(got) != 4 {
		t.Errorf("allow changed: %q", got)
	}
}

func TestAutoModeRules(t *testing.T) {
	doc := loadDoc(t, "user.json")
	p, err := AddAutoModeRule(patch.User, AutoSoftDeny, " Force-pushing any branch ")
	out := apply(t, p, err, doc)
	if got := list(out, "autoMode", "soft_deny"); !reflect.DeepEqual(got, []string{"Force-pushing any branch"}) {
		t.Errorf("soft_deny: %q", got)
	}
	p, err = RemoveAutoModeRule(patch.User, AutoAllow, "Running the test suite")
	out = apply(t, p, err, out)
	if got := list(out, "autoMode", "allow"); len(got) != 0 {
		t.Errorf("allow: %q", got)
	}
	if got := list(out, "autoMode", "environment"); len(got) != 1 {
		t.Errorf("environment changed: %q", got)
	}
	if _, err := AddAutoModeRule(patch.User, "maybe", "x"); err == nil {
		t.Error("unknown list accepted")
	}
	if _, err := AddAutoModeRule(patch.User, AutoAllow, " "); err == nil {
		t.Error("blank rule accepted")
	}
}

func TestReadOnlyScopes(t *testing.T) {
	for _, s := range []patch.Scope{patch.Policy, patch.Flag, patch.Scope("session")} {
		checks := map[string]error{}
		_, checks["AddRule"] = AddRule(s, Allow, "Read")
		_, checks["RemoveRule"] = RemoveRule(s, Deny, "Read")
		_, checks["AddDirectory"] = AddDirectory(s, "/x")
		_, checks["RemoveDirectory"] = RemoveDirectory(s, "/x")
		_, checks["SetDefaultMode"] = SetDefaultMode(s, "plan")
		_, checks["ClearDefaultMode"] = ClearDefaultMode(s)
		_, checks["AddAutoModeRule"] = AddAutoModeRule(s, AutoAllow, "x")
		_, checks["RemoveAutoModeRule"] = RemoveAutoModeRule(s, AutoAllow, "x")
		for name, err := range checks {
			if !errors.Is(err, patch.ErrReadOnly) {
				t.Errorf("%s on %s: %v", name, s, err)
			}
		}
	}
}

func TestInvalidInput(t *testing.T) {
	if _, err := AddRule(patch.User, Allow, "Bash(git"); !errors.Is(err, ErrUnbalanced) {
		t.Errorf("unbalanced: %v", err)
	}
	if _, err := AddRule(patch.User, "sometimes", "Read"); err == nil {
		t.Error("bad behavior accepted")
	}
	if _, err := RemoveRule(patch.User, Allow, ""); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty remove: %v", err)
	}
	if m, ok := NormalizeMode("bypassPermissions"); !ok || m != "bypassPermissions" {
		t.Error("bypassPermissions rejected")
	}
}

// Every edit is valid patch data for the config writer.
func TestEditsValidate(t *testing.T) {
	ps := []func() (patch.Patch, error){
		func() (patch.Patch, error) { return AddRule(patch.Local, Ask, "Bash(rm:*)") },
		func() (patch.Patch, error) { return RemoveRule(patch.Local, Ask, "Bash(rm:*)") },
		func() (patch.Patch, error) { return AddDirectory(patch.Project, "../x") },
		func() (patch.Patch, error) { return SetDefaultMode(patch.User, "auto") },
		func() (patch.Patch, error) { return AddAutoModeRule(patch.User, AutoEnvironment, "CI runner") },
	}
	for i, f := range ps {
		p, err := f()
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		if err := p.Validate(); err != nil {
			t.Errorf("%d: %v", i, err)
		}
		if len(p.Describe()) == 0 {
			t.Errorf("%d: no description", i)
		}
	}
}
