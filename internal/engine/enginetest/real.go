package enginetest

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Real is a real claude binary driven by an engine.Manager against an in-process
// fakeapi server, with an isolated CLAUDE_CONFIG_DIR: offline, free and deterministic.
type Real struct {
	Manager *engine.Manager
	Rec     *Recorder
	API     *fakeapi.Server
	URL     string
	Config  string // isolated CLAUDE_CONFIG_DIR
	Work    string // engine cwd (trusted in Config)

	// AutoAllow answers every PermissionMsg with allow (set before Start).
	AutoAllow bool
}

// NewReal starts fakeapi with script and prepares a Manager for the real claude. It
// skips the test when claude is not on PATH or with -short.
func NewReal(t testing.TB, script *fakeapi.Script, opts ...fakeapi.Option) *Real {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the real claude binary")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not on PATH")
	}
	api := fakeapi.New(script, opts...)
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	r := &Real{API: api, URL: srv.URL, Config: t.TempDir(), Rec: NewRecorder()}
	r.Work, _ = filepath.EvalSymlinks(t.TempDir())
	if err := fakeapi.SeedConfig(r.Config, fakeapi.FakeAPIKey, r.Work); err != nil {
		t.Fatal(err)
	}
	m := engine.NewManager(func(msg tea.Msg) {
		r.Rec.Send(msg)
		if pm, ok := msg.(ext.PermissionMsg); ok && r.AutoAllow {
			go pm.Reply(pm.Req.Allow(nil))()
		}
	})
	m.Binary, m.RunDir = bin, "-"
	r.Manager = m
	t.Cleanup(func() { m.Close(t.Context()) })
	return r
}

// Opts returns SpawnOpts that point the engine at fakeapi; extend as needed.
func (r *Real) Opts() ext.SpawnOpts {
	env := map[string]string{}
	for _, kv := range fakeapi.Env(nil, r.URL, r.Config, fakeapi.FakeAPIKey) {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	var unset []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if (strings.HasPrefix(k, "ANTHROPIC_") || strings.HasPrefix(k, "CLAUDE_") || strings.HasPrefix(k, "OTEL_")) && env[k] == "" {
			unset = append(unset, k)
		}
	}
	return ext.SpawnOpts{Cwd: r.Work, Model: "claude-sonnet-4-5", Env: env, UnsetEnv: unset}
}
