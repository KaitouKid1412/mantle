package perms

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

const fixtures = "../../../testdata/fixtures/08/perms"

func loadDoc(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func fixtureDocs(t *testing.T) map[patch.Scope]map[string]any {
	return map[patch.Scope]map[string]any{
		patch.User:    loadDoc(t, "user.json"),
		patch.Project: loadDoc(t, "project.json"),
		patch.Local:   loadDoc(t, "local.json"),
		patch.Policy:  loadDoc(t, "policy.json"),
	}
}

func raws(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.String()
	}
	return out
}

func TestMerge(t *testing.T) {
	v := Merge(fixtureDocs(t))

	wantAllow := []string{
		"Bash(make build) [Local]", "Bash(oops [Local]",
		"Bash(npm run test:*) [Project]", "Edit(./src/**) [Project]",
		"Bash(git status) [User]", "Read(~/notes/**) [User]", "mcp__github__* [User]",
	}
	if got := raws(v.Allow); !reflect.DeepEqual(got, wantAllow) {
		t.Errorf("allow:\n got %q\nwant %q", got, wantAllow)
	}
	wantDeny := []string{
		"Bash(curl:*) [Managed]", "Read(//etc/secrets/**) [Managed]",
		"Read(./.env) [Local]", "WebFetch(domain:example.com) [User]",
	}
	if got := raws(v.Deny); !reflect.DeepEqual(got, wantDeny) {
		t.Errorf("deny:\n got %q\nwant %q", got, wantDeny)
	}
	if got := raws(v.Ask); !reflect.DeepEqual(got, []string{"Bash(git push:*) [Project]"}) {
		t.Errorf("ask: %q", got)
	}

	for _, e := range v.Deny[:2] {
		if !e.ReadOnly {
			t.Errorf("managed rule %q is editable", e.Raw)
		}
	}
	for _, e := range v.Allow {
		if e.ReadOnly {
			t.Errorf("rule %q from %s is read-only", e.Raw, e.Scope)
		}
	}
	if v.Allow[1].Err == nil {
		t.Error("broken rule has no parse error")
	}
	if v.Allow[1].Describe() == "" {
		t.Error("broken rule has no description")
	}

	if v.DefaultMode != (Value{Value: "plan", Scope: patch.Project, Set: true}) {
		t.Errorf("default mode: %+v", v.DefaultMode)
	}
	if v.DisableBypass != (Value{Value: "disable", Scope: patch.Policy, Set: true}) {
		t.Errorf("disable bypass: %+v", v.DisableBypass)
	}
	wantDirs := []Dir{{"../shared-lib", patch.Project, false}, {"~/shared/docs", patch.User, false}}
	if !reflect.DeepEqual(v.Directories, wantDirs) {
		t.Errorf("dirs: %+v", v.Directories)
	}
	if got := v.AutoMode[AutoHardDeny]; len(got) != 1 || got[0].Scope != patch.Policy || !got[0].ReadOnly {
		t.Errorf("hard_deny: %+v", got)
	}
	if got := v.AutoMode[AutoSoftDeny]; len(got) != 1 || got[0].Scope != patch.Local {
		t.Errorf("soft_deny: %+v", got)
	}
	if got := v.AutoMode[AutoEnvironment]; len(got) != 1 || got[0].Text != "Personal laptop, no production credentials" {
		t.Errorf("environment: %+v", got)
	}
	if !reflect.DeepEqual(v.Rules(Deny), v.Deny) {
		t.Error("Rules(Deny) mismatch")
	}
}

func TestMergeDefaultModeFlagWins(t *testing.T) {
	docs := map[patch.Scope]map[string]any{
		patch.Local: {"permissions": map[string]any{"defaultMode": "plan"}},
		patch.Flag:  {"permissions": map[string]any{"defaultMode": "dontAsk", "allow": []any{"Read"}}},
	}
	v := Merge(docs)
	if v.DefaultMode.Value != "dontAsk" || v.DefaultMode.Scope != patch.Flag {
		t.Errorf("default mode: %+v", v.DefaultMode)
	}
	if len(v.Allow) != 1 || !v.Allow[0].ReadOnly {
		t.Errorf("flag rule should be read-only: %+v", v.Allow)
	}
}

func TestMergeEmptyAndOdd(t *testing.T) {
	v := Merge(nil)
	if len(v.Allow)+len(v.Ask)+len(v.Deny)+len(v.Directories) != 0 || v.DefaultMode.Set {
		t.Errorf("empty merge: %+v", v)
	}
	odd := map[patch.Scope]map[string]any{
		patch.User: {"permissions": map[string]any{"allow": []any{"Read", 42.0}, "deny": "Edit"}},
	}
	v = Merge(odd)
	if got := raws(v.Allow); !reflect.DeepEqual(got, []string{"Read [User]", "42 [User]"}) {
		t.Errorf("odd allow: %q", got)
	}
	if len(v.Deny) != 0 {
		t.Errorf("non-array deny: %+v", v.Deny)
	}
}

func TestFromListResponse(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(fixtures, "list_response.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := FromListResponse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := raws(v.Allow); !reflect.DeepEqual(got, []string{"Bash(git status) [User]", "Bash(ls) [session]"}) {
		t.Errorf("allow: %q", got)
	}
	if !v.Allow[1].ReadOnly || v.Allow[0].ReadOnly {
		t.Errorf("editability: %+v", v.Allow)
	}
	if v.Allow[0].Describe() != "Commands starting with git status" {
		t.Errorf("summary: %q", v.Allow[0].Describe())
	}
	if len(v.Deny) != 1 || !v.Deny[0].ReadOnly || v.Deny[0].Scope != patch.Policy {
		t.Errorf("deny: %+v", v.Deny)
	}
	if len(v.Ask) != 1 || !v.Ask[0].NotInEffect {
		t.Errorf("ask: %+v", v.Ask)
	}
	if len(v.Directories) != 2 || v.Directories[0].ReadOnly || !v.Directories[1].ReadOnly {
		t.Errorf("dirs: %+v", v.Directories)
	}
	if v.OriginalCwd != "/work/repo" {
		t.Errorf("cwd: %q", v.OriginalCwd)
	}

	// The bare state object (no wrapper) decodes the same way.
	var wrapper struct{ State json.RawMessage }
	if err := json.Unmarshal(b, &wrapper); err != nil {
		t.Fatal(err)
	}
	v2, err := FromListResponse(wrapper.State)
	if err != nil || len(v2.Allow) != 2 {
		t.Errorf("bare state: %v %+v", err, v2.Allow)
	}
}

func TestFromListResponseFallbacks(t *testing.T) {
	raw := `{"state":{"allow":["Read",{"rule":"Edit","source":"localSettings"},{"ruleValue":{"toolName":"Bash","ruleContent":"ls"},"source":"userSettings"}],"deny":[{"ruleValue":{"toolName":"WebSearch"},"source":"cliArg"}]}}`
	v, err := FromListResponse(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := raws(v.Allow); !reflect.DeepEqual(got, []string{"Edit [Local]", "Bash(ls) [User]", "Read [?]"}) {
		t.Errorf("allow: %q", got)
	}
	if len(v.Deny) != 1 || v.Deny[0].Raw != "WebSearch" || !v.Deny[0].ReadOnly {
		t.Errorf("deny: %+v", v.Deny)
	}
	for _, bad := range []string{`{"state":{"mode":"default"}}`, `{}`, `[]`, `nope`} {
		if _, err := FromListResponse(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: no error", bad)
		} else if bad == `{}` && !errors.Is(err, ErrUnknownShape) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}
