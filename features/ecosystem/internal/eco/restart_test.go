package eco_test

import (
	"os"
	"path/filepath"
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

// TestRestartResumesOnlySavedSessions covers the fresh-session /login bug: a
// headless session has no transcript before its first turn, and --resume of
// it kills the engine, so the restart must be fresh until the transcript holds
// a message.
func TestRestartResumesOnlySavedSessions(t *testing.T) {
	cfg, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	eng := ecotest.NewEngine()
	eng.Opts = ext.SpawnOpts{Cwd: cwd, Resume: "old-session", Continue: true, Settings: `{"disabledMcpjsonServers":["x"]}`,
		AddDirs: []string{"/extra"}, ExtraArgs: []string{"--verbose-flag", "--resume", "old", "--session-id=abc", "-c", "--debug"}}
	ctx := ecotest.NewCtx(eng, cwd)
	ctx.SessionValue.SessionID = sid
	ctx.SessionValue.Model = "claude-opus-test"
	ctx.SessionValue.PermissionMode = "acceptEdits"

	// Never written: fresh restart that keeps model, mode and launch options.
	o := eco.RestartOpts(ctx)
	if o.Resume != "" || o.Continue || o.SessionID != "" {
		t.Fatalf("fresh session must not be resumed: %+v", o)
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

	// Only metadata, no message yet: still fresh.
	transcript(t, cfg, cwd, `{"type":"file-history-snapshot","messageId":"x"}`+"\n")
	if o := eco.RestartOpts(ctx); o.Resume != "" {
		t.Errorf("metadata-only transcript resumed: %+v", o)
	}

	// After a turn: resume.
	transcript(t, cfg, cwd, `{"type":"file-history-snapshot"}`+"\n"+`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n")
	if o := eco.RestartOpts(ctx); o.Resume != sid {
		t.Errorf("saved session not resumed: %+v", o)
	}
}

func TestSessionPersistedUsesEngineConfigDir(t *testing.T) {
	cfg, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // mantle's own env points elsewhere
	transcript(t, cfg, cwd, `{"type":"assistant"}`+"\n")
	o := ext.SpawnOpts{Cwd: cwd, Env: map[string]string{"CLAUDE_CONFIG_DIR": cfg}}
	if !eco.SessionPersisted(o, sid) {
		t.Error("the engine's CLAUDE_CONFIG_DIR is where its transcripts are")
	}
	if eco.SessionPersisted(ext.SpawnOpts{Cwd: cwd}, sid) {
		t.Error("found a transcript in the wrong config dir")
	}
	if eco.SessionPersisted(o, "not-a-uuid") {
		t.Error("invalid ids are never resumable")
	}
	if !filepath.IsAbs(cfg) {
		t.Fatal("temp dir not absolute")
	}
}

func TestRestartWithoutOptioner(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SessionValue = ext.SessionInfo{EngineID: ext.MainEngine, SessionID: sid, Cwd: t.TempDir(), Model: "m"}
	o := eco.RestartOpts(ctx) // no engine: options from the session only
	if o.Resume != "" || o.Model != "m" || o.Cwd == "" {
		t.Errorf("opts = %+v", o)
	}
}
