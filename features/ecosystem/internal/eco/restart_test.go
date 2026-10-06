package eco_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

const sid = "7b2a9022-1111-4222-8333-444455556666"

// transcript writes a transcript for sid in cwd's project dir under cfg.
func transcript(t *testing.T, cfg, cwd, content string) {
	t.Helper()
	dir := sessions.Layout{ConfigDir: cfg}.ProjectDir(cwd)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessions.SessionFile(dir, sid), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRestartSessionFlags covers the fresh-session /login bug: a headless
// session has no transcript before its first turn, and --resume of it kills
// the engine. The rule matches plan 06's hand-off: resume a saved session,
// reuse the id when no file exists, and use no session flag for a file
// without messages.
func TestRestartSessionFlags(t *testing.T) {
	cfg, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	eng := ecotest.NewEngine()
	eng.Opts = ext.SpawnOpts{Cwd: cwd, Resume: "old-session", Continue: true, Settings: `{"disabledMcpjsonServers":["x"]}`,
		AddDirs: []string{"/extra"}, ExtraArgs: []string{"--verbose-flag", "--resume", "old", "--session-id=abc", "-c", "--debug"}}
	ctx := ecotest.NewCtx(eng, cwd)
	ctx.SessionValue.SessionID = sid
	ctx.SessionValue.Model = "claude-opus-test"
	ctx.SessionValue.PermissionMode = "acceptEdits"

	// No transcript yet: a new session under the same id, keeping model, mode and
	// the launch options.
	o := eco.RestartOpts(ctx)
	if o.Resume != "" || o.SessionID != sid || o.Continue {
		t.Fatalf("fresh session: %+v", o)
	}
	if o.Model != "claude-opus-test" || o.PermissionMode != "acceptEdits" || o.Cwd != cwd {
		t.Errorf("model, mode or cwd lost: %+v", o)
	}
	if o.Settings == "" || !reflect.DeepEqual(o.AddDirs, []string{"/extra"}) {
		t.Errorf("startup gate settings or add-dirs lost: %+v", o)
	}
	if !reflect.DeepEqual(o.ExtraArgs, []string{"--verbose-flag", "--debug"}) {
		t.Errorf("session flags not stripped: %q", o.ExtraArgs)
	}

	// A file without messages: neither --resume nor --session-id.
	transcript(t, cfg, cwd, `{"type":"custom-title","customTitle":"x"}`+"\n")
	if o := eco.RestartOpts(ctx); o.Resume != "" || o.SessionID != "" {
		t.Errorf("metadata-only transcript: %+v", o)
	}

	// After a turn: resume.
	transcript(t, cfg, cwd, `{"type":"custom-title"}`+"\n"+`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n")
	if o := eco.RestartOpts(ctx); o.Resume != sid || o.SessionID != "" {
		t.Errorf("saved session: %+v", o)
	}
}

// The engine's own CLAUDE_CONFIG_DIR decides where its transcripts are.
func TestRestartUsesEngineConfigDir(t *testing.T) {
	cfg, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // mantle's own env points elsewhere
	transcript(t, cfg, cwd, `{"type":"assistant"}`+"\n")
	eng := ecotest.NewEngine()
	eng.Opts = ext.SpawnOpts{Cwd: cwd, Env: map[string]string{"CLAUDE_CONFIG_DIR": cfg}}
	ctx := ecotest.NewCtx(eng, cwd)
	ctx.SessionValue.SessionID = sid
	if o := eco.RestartOpts(ctx); o.Resume != sid {
		t.Errorf("transcript in the engine's config dir not found: %+v", o)
	}
}

func TestRestartWithoutEngineOptions(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: "not-a-uuid", Cwd: t.TempDir(), Model: "m"}
	o := eco.RestartOpts(ctx) // no engine: options from the session only
	if o.Resume != "" || o.SessionID != "" || o.Model != "m" || o.Cwd == "" {
		t.Errorf("opts = %+v", o)
	}
}
