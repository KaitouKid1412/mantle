package input

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// onePixelPNG is a valid 1×1 PNG.
var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")

// realEngine is the real claude binary (plan 02's engine) talking to an
// in-process fakeapi with an isolated config dir: offline, no tokens.
type realEngine struct {
	eng  *engine.Engine
	work string
	mu   sync.Mutex
	reqs []fakeapi.Request
}

func startRealEngine(t *testing.T, turns ...fakeapi.Turn) *realEngine {
	t.Helper()
	if testing.Short() {
		t.Skip("runs the real claude binary")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	re := &realEngine{}
	srv := httptest.NewServer(fakeapi.New(&fakeapi.Script{Turns: turns}, fakeapi.WithRecorder(func(r fakeapi.Request) {
		re.mu.Lock()
		re.reqs = append(re.reqs, r)
		re.mu.Unlock()
	})))
	t.Cleanup(srv.Close)
	re.work, _ = filepath.EvalSymlinks(t.TempDir())
	cfg := t.TempDir()
	if err := fakeapi.SeedConfig(cfg, fakeapi.FakeAPIKey, re.work); err != nil {
		t.Fatal(err)
	}
	var unset []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "ANTHROPIC_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "OTEL_") {
			unset = append(unset, name)
		}
	}
	mgr := engine.NewManager(func(tea.Msg) {})
	eng, err := mgr.Start(ext.MainEngine, ext.SpawnOpts{
		Cwd: re.work, Model: "claude-sonnet-4-5", UnsetEnv: unset,
		Env: map[string]string{
			"ANTHROPIC_BASE_URL": srv.URL, "ANTHROPIC_API_KEY": fakeapi.FakeAPIKey,
			"CLAUDE_CONFIG_DIR": cfg, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
			"DISABLE_TELEMETRY": "1", "DISABLE_AUTOUPDATER": "1", "DISABLE_ERROR_REPORTING": "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mgr.Close(ctx)
	})
	re.eng = eng
	return re
}

// bodies returns the recorded request bodies containing every needle.
func (re *realEngine) bodies(needles ...string) []string {
	re.mu.Lock()
	defer re.mu.Unlock()
	var out []string
	for _, rq := range re.reqs {
		b := string(rq.Body)
		ok := true
		for _, n := range needles {
			ok = ok && strings.Contains(b, n)
		}
		if ok {
			out = append(out, b)
		}
	}
	return out
}

func (re *realEngine) waitBody(t *testing.T, needles ...string) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if bs := re.bodies(needles...); len(bs) > 0 {
			return bs[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no Messages API request with %q; stderr:\n%s", needles, re.eng.StderrTail(4096))
	return ""
}

// TestRealEngineGetsChipsAndImages types a prompt with a paste chip and an
// image and checks what reaches the Messages API through the stages, plan
// 02's engine and the real claude binary.
func TestRealEngineGetsChipsAndImages(t *testing.T) {
	re := startRealEngine(t, fakeapi.Turn{Match: &fakeapi.Match{Contains: "summarize"}, Reply: []fakeapi.Block{{Type: "text", Text: "Summary done."}}})
	r := newRig(t, nil)
	r.c.Engines[ext.MainEngine] = re.eng
	r.s.cwd = re.work
	r.keys("'summarize '")
	(&promptComp{s: r.s}).HandlePaste(r.c, tea.PasteMsg{Content: "log line one\nlog line two\nlog line three\nlog line four"})
	im, err := editor.PrepareImage(onePixelPNG)
	if err != nil {
		t.Fatal(err)
	}
	r.event(editor.ImagePastedMsg{Image: im})
	r.keys("enter")

	var body map[string]any
	_ = json.Unmarshal([]byte(re.waitBody(t, "log line four")), &body)
	msgs, _ := body["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	content, _ := last["content"].([]any)
	var hasImage bool
	var text strings.Builder
	for _, b := range content {
		blk, _ := b.(map[string]any)
		switch blk["type"] {
		case "image":
			hasImage = true
		case "text":
			text.WriteString(blk["text"].(string) + "\n")
		}
	}
	if !hasImage {
		t.Errorf("image block missing from the API request")
	}
	if all := text.String(); !strings.Contains(all, "summarize") || !strings.Contains(all, "log line four") || !strings.Contains(all, "[Image #2]") {
		t.Errorf("text blocks:\n%s", all)
	}
}

// TestRealEngineBashMode runs a ! command with respondToBashCommands off:
// the output joins the conversation without a turn, and the next prompt's
// API request carries it.
func TestRealEngineBashMode(t *testing.T) {
	re := startRealEngine(t, fakeapi.Turn{Match: &fakeapi.Match{Contains: "what did it print"}, Reply: []fakeapi.Block{{Type: "text", Text: "A marker."}}})
	r := newRig(t, map[string]any{"respondToBashCommands": false})
	r.c.Engines[ext.MainEngine] = re.eng
	r.s.cwd = re.work
	r.keys("'!'", "'echo MARKER-42'", "enter")
	time.Sleep(time.Second)
	if n := len(re.bodies("MARKER-42")); n != 0 {
		t.Fatalf("shouldQuery false must not start a turn (%d requests)", n)
	}
	r.keys("'what did it print?'", "enter")
	re.waitBody(t, "what did it print", "MARKER-42", "bash-input")
}

// TestRealEngineFileMentions checks @ completion against the real engine's
// file_suggestions.
func TestRealEngineFileMentions(t *testing.T) {
	re := startRealEngine(t)
	for _, f := range []string{"main.go", "manual.md", "other.txt"} {
		if err := os.WriteFile(filepath.Join(re.work, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := newRig(t, nil)
	r.c.Engines[ext.MainEngine] = re.eng
	r.s.cwd = re.work
	r.event(ext.EngineAttachMsg{EngineID: ext.MainEngine, Engine: re.eng}) // warms the index
	r.keys("'read @ma'")
	for i := 0; i < 50 && !r.s.comp.open(); i++ {
		time.Sleep(100 * time.Millisecond) // index still building: type on
		r.keys("backspace", "'a'")
	}
	if !r.s.comp.open() || r.s.comp.kind != compFile {
		t.Fatalf("no @ menu; items %+v", r.s.comp.items)
	}
	var got []string
	for _, it := range r.s.comp.items {
		got = append(got, it.value)
	}
	if strings.Join(got, ",") != "main.go,manual.md" && strings.Join(got, ",") != "manual.md,main.go" {
		t.Fatalf("suggestions %v", got)
	}
	r.keys("tab")
	if !strings.HasPrefix(r.text(), "read @ma") || !strings.HasSuffix(r.text(), " ") {
		t.Fatalf("accept: %q", r.text())
	}
}
