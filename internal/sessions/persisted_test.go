package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersisted(t *testing.T) {
	l := Layout{ConfigDir: t.TempDir()}
	const cwd, other = "/work/demo", "/work/other"
	const id = "7b2a9022-0000-4000-8000-000000000001"
	write := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(SessionFile(dir, id), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A fresh session: the headless engine has written nothing yet.
	if l.Persisted(cwd, id) {
		t.Fatal("no transcript: persisted")
	}
	write(l.ProjectDir(cwd), "")
	if l.Persisted(cwd, id) {
		t.Fatal("empty transcript: persisted")
	}
	// Metadata alone (a title set before the first prompt) cannot be resumed.
	write(l.ProjectDir(cwd), `{"type":"custom-title","customTitle":"x","sessionId":"`+id+`"}`+"\n"+`{"type":"mode","mode":"normal"}`+"\n")
	if l.Persisted(cwd, id) {
		t.Fatal("metadata only: persisted")
	}
	// The first prompt makes it resumable, whether or not the line ends.
	write(l.ProjectDir(cwd), `{"type":"custom-title","customTitle":"x"}`+"\n"+`{"type":"user","message":{"role":"user","content":"hi"}}`)
	if !l.Persisted(cwd, id) {
		t.Fatal("transcript with a prompt: not persisted")
	}
	// Only cwd's project counts: that is where `claude --resume` looks.
	if l.Persisted(other, id) {
		t.Fatal("other project: persisted")
	}
	if l.Persisted("", id) || l.Persisted(cwd, "not-a-uuid") {
		t.Fatal("bad arguments: persisted")
	}
	if _, err := os.Stat(filepath.Join(l.ProjectDir(other), id+".jsonl")); err == nil {
		t.Fatal("test wrote to the wrong project")
	}
}
