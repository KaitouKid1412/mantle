package turn_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn"
	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// hostWait bounds every wait in the real-host tests. Generous: under a full parallel
// `go test ./...` the pty-driven program can be slow to draw.
const hostWait = time.Minute

// hostEnv points HOME at a temp dir and clears variables the gates read.
func hostEnv(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("MANTLE_HOME", "")
	t.Setenv("MANTLE_CLAUDE_BIN", stubClaude(t, home, "2.1.288"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_SANDBOXED", "")
	return home
}

// projectDir creates an untrusted project under HOME.
func projectDir(t *testing.T) string {
	t.Helper()
	proj := filepath.Join(os.Getenv("HOME"), "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj
}

// turnFeature returns the registered turn feature.
func turnFeature(t *testing.T) []ext.Feature {
	t.Helper()
	var features []ext.Feature
	for _, f := range ext.Pending() {
		if f.ID == turn.FeatureID {
			features = append(features, f)
		}
	}
	if len(features) != 1 {
		t.Fatalf("turn feature not registered: %d", len(features))
	}
	return features
}

// hostWithTurn runs the real host with only the turn feature, against a temp HOME, and
// counts engine spawns.
func hostWithTurn(t *testing.T) (*testkit.Harness, *atomic.Int32, string) {
	t.Helper()
	hostEnv(t)
	proj := projectDir(t)
	spawned := &atomic.Int32{}
	root := app.New(app.Options{
		Host:              app.NewHost(turnFeature(t), app.HostOptions{Core: app.CoreFeatures()}),
		NoBackgroundQuery: true,
		Session:           ext.SessionInfo{EngineID: ext.MainEngine, Cwd: proj},
		MainSpawn:         &ext.SpawnOpts{Cwd: proj},
		Spawn: func(string, ext.SpawnOpts) error {
			spawned.Add(1)
			return nil
		},
	})
	return testkit.New(t, root, testkit.WithSize(100, 30)), spawned, proj
}

// PD-43 against the real host: the engine starts only after the trust dialog is
// accepted.
func TestHostNoSpawnBeforeTrust(t *testing.T) {
	hn, spawned, _ := hostWithTurn(t)
	hn.WaitForText("Do you trust this folder?", hostWait)
	hn.Settle(100*time.Millisecond, time.Second)
	if n := spawned.Load(); n != 0 {
		t.Fatalf("engine spawned %d times before the gate passed", n)
	}
	hn.Send("enter")
	deadline := time.Now().Add(hostWait)
	for spawned.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := spawned.Load(); n != 1 {
		t.Fatalf("spawns after accepting trust = %d", n)
	}
	// The acceptance is recorded asynchronously in mantle's own store.
	store := filepath.Join(os.Getenv("HOME"), ".mantle", "trust.json")
	for _, err := os.Stat(store); err != nil && time.Now().Before(deadline); _, err = os.Stat(store) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("trust not recorded: %v", err)
	}
	hn.Settle(100*time.Millisecond, time.Second)
	if _, err := hn.Quit(); err != nil {
		t.Fatal(err)
	}
}

func TestHostDecliningTrustExitsWithoutSpawn(t *testing.T) {
	hn, spawned, _ := hostWithTurn(t)
	hn.WaitForText("Do you trust this folder?", hostWait)
	hn.Send("esc")
	if _, err := hn.Wait(hostWait); err != nil {
		t.Fatal(err)
	}
	if n := spawned.Load(); n != 0 {
		t.Fatalf("declined trust still spawned %d times", n)
	}
}

// A permission prompt through the real host: inline dialog, real keymap, one reply.
func TestHostPermissionPrompt(t *testing.T) {
	hn, spawned, _ := hostWithTurn(t)
	hn.WaitForText("Do you trust this folder?", hostWait)
	hn.Send("enter")
	// Keys and program messages travel separately: wait until the gate passed (the
	// engine spawned) before the engine's first request arrives.
	for deadline := time.Now().Add(hostWait); spawned.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	replies := make(chan proto.PermissionResult, 2)
	var req proto.CanUseTool
	_ = json.Unmarshal([]byte(`{"tool_name":"Bash","tool_use_id":"t1","input":{"command":"go test ./..."}}`), &req)
	hn.SendMsg(ext.PermissionMsg{EngineID: ext.MainEngine, RequestID: "r1", Req: req,
		Reply: func(r proto.PermissionResult) tea.Cmd { replies <- r; return nil }})
	hn.WaitForText("go test ./...", hostWait)
	hn.Send("tab") // tab adds instructions to No
	hn.WaitForText("Enter to send", hostWait)
	hn.Type("use make test")
	hn.Settle(100*time.Millisecond, 2*time.Second)
	hn.Send("enter")
	select {
	case r := <-replies:
		if r.Behavior != proto.BehaviorDeny || r.Message != "The user denied this tool call and said: use make test" {
			t.Fatalf("reply = %+v", r)
		}
	case <-time.After(hostWait):
		t.Fatal("no reply")
	}
	hn.Settle(100*time.Millisecond, time.Second)
	if _, err := hn.Quit(); err != nil {
		t.Fatal(err)
	}
}

// stubClaude writes an executable that prints a claude version, for the engine version
// gate (CheckEngine runs `$MANTLE_CLAUDE_BIN --version`).
func stubClaude(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "claude-stub")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho '"+version+" (Claude Code)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
