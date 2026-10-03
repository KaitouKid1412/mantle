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

// hostWithTurn runs the real host with only the turn feature, against a temp HOME, and
// counts engine spawns.
func hostWithTurn(t *testing.T) (*testkit.Harness, *atomic.Int32, string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("CLAUDE_CODE_SANDBOXED", "")
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	var features []ext.Feature
	for _, f := range ext.Pending() {
		if f.ID == turn.FeatureID {
			features = append(features, f)
		}
	}
	if len(features) != 1 {
		t.Fatalf("turn feature not registered: %d", len(features))
	}
	spawned := &atomic.Int32{}
	root := app.New(app.Options{
		Host:              app.NewHost(features, app.HostOptions{Core: app.CoreFeatures()}),
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
	hn.WaitForText("Do you trust this folder?", 5*time.Second)
	hn.Settle(100*time.Millisecond, time.Second)
	if n := spawned.Load(); n != 0 {
		t.Fatalf("engine spawned %d times before the gate passed", n)
	}
	hn.Send("enter")
	deadline := time.Now().Add(5 * time.Second)
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
	hn.WaitForText("Do you trust this folder?", 5*time.Second)
	hn.Send("esc")
	if _, err := hn.Wait(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := spawned.Load(); n != 0 {
		t.Fatalf("declined trust still spawned %d times", n)
	}
}

// A permission prompt through the real host: inline dialog, real keymap, one reply.
func TestHostPermissionPrompt(t *testing.T) {
	hn, spawned, _ := hostWithTurn(t)
	hn.WaitForText("Do you trust this folder?", 5*time.Second)
	hn.Send("enter")
	// Keys and program messages travel separately: wait until the gate passed (the
	// engine spawned) before the engine's first request arrives.
	for deadline := time.Now().Add(5 * time.Second); spawned.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	replies := make(chan proto.PermissionResult, 2)
	var req proto.CanUseTool
	_ = json.Unmarshal([]byte(`{"tool_name":"Bash","tool_use_id":"t1","input":{"command":"go test ./..."}}`), &req)
	hn.SendMsg(ext.PermissionMsg{EngineID: ext.MainEngine, RequestID: "r1", Req: req,
		Reply: func(r proto.PermissionResult) tea.Cmd { replies <- r; return nil }})
	hn.WaitForText("go test ./...", 5*time.Second)
	hn.Send("down", "enter") // "No, and tell Claude…" opens the feedback field
	hn.WaitForText("enter to send", 5*time.Second)
	hn.Type("use make test")
	hn.Settle(100*time.Millisecond, 2*time.Second)
	hn.Send("enter")
	select {
	case r := <-replies:
		if r.Behavior != proto.BehaviorDeny || r.Message != "The user denied this tool call and said: use make test" {
			t.Fatalf("reply = %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	hn.Settle(100*time.Millisecond, time.Second)
	if _, err := hn.Quit(); err != nil {
		t.Fatal(err)
	}
}
