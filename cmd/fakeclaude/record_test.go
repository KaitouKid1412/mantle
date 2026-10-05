package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/engine/fixture"
)

// TestRecordDrive records a client script driving fakeclaude itself (offline).
func TestRecordDrive(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeclaude")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	engine := filepath.Join(dir, "engine.jsonl")
	os.WriteFile(engine, []byte(`{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[]}}
{"expect": {"type":"user"}, "timeout": 30000}
{"emit": {"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"hello from `+dir+`"}]},"session_id":"1bd4a383-7c8a-49fb-a66b-d55320e41104"}}
{"emit": {"type":"result","subtype":"success","result":"ok","user_message_uuid":"${uuid}"}}
`), 0o644)
	client := filepath.Join(dir, "client.jsonl")
	os.WriteFile(client, []byte(`{"emit": {"type":"control_request","request_id":"r1","request":{"subtype":"initialize"}}}
{"expect": {"type":"control_response"}, "timeout": 30000}
{"emit": {"type":"user","uuid":"${new:u}","message":{"role":"user","content":"hi"}}}
{"expect": {"type":"result"}, "timeout": 30000}
`), 0o644)
	out := filepath.Join(dir, "rec.jsonl")
	t.Setenv("FAKECLAUDE_SCRIPT", engine)
	if code := record([]string{"-o", out, "-drive", client, "--", bin}); code != 0 {
		t.Fatalf("record exit %d", code)
	}
	es, err := fixture.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range es {
		dirs = append(dirs, e.Dir)
	}
	if strings.Join(dirs, " ") != "in out in out out" {
		t.Errorf("directions %v", dirs)
	}
	b, _ := os.ReadFile(out)
	if strings.Contains(string(b), "1bd4a383") {
		t.Error("session id not sanitised")
	}
}
