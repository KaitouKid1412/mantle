package settingsfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

func TestReadAll(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	env := patch.Env{Home: home, ProjectRoot: repo}
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(repo, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	os.WriteFile(filepath.Join(repo, ".claude", "settings.json"), []byte(`{"model":`), 0o644)

	srcs := ReadAll(env)
	if len(srcs) != 4 {
		t.Fatalf("sources %+v", srcs)
	}
	user, project, local := srcs[0], srcs[1], srcs[2]
	if !user.Exists || user.Err != nil || user.Doc["model"] != "opus" {
		t.Errorf("user %+v", user)
	}
	if !project.Exists || project.Err == nil || project.Doc != nil {
		t.Errorf("project %+v", project)
	}
	if local.Exists || local.Err != nil || local.Path != filepath.Join(repo, ".claude", "settings.local.json") {
		t.Errorf("local %+v", local)
	}
	if srcs[3].Scope != patch.Policy {
		t.Errorf("managed %+v", srcs[3])
	}
	docs := Docs(srcs)
	if len(docs) != 1 || docs[patch.User]["model"] != "opus" {
		t.Errorf("docs %v", docs)
	}
	if got := ReadScope(patch.Env{Home: home}, patch.Project); got.Path != "" {
		t.Errorf("project without root %+v", got)
	}
}

func TestManagedPath(t *testing.T) {
	if ManagedPath("darwin") != "/Library/Application Support/ClaudeCode/managed-settings.json" ||
		ManagedPath("linux") != "/etc/claude-code/managed-settings.json" {
		t.Error("managed paths")
	}
}
