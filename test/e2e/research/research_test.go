// Package research is research mode's end-to-end test (plan 13, MT-R1…R8): mantle-ui
// built from this tree, the installed claude as its engine, the scripted fakeapi with
// an isolated CLAUDE_CONFIG_DIR (offline, no tokens), in a pty read through the vt
// emulator. It takes about a minute, so it runs only with MANTLE_E2E=1
// (`make e2e-research`).
package research

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/test/parity"
)

const (
	width, height = 100, 34
	wait          = 60 * time.Second
)

func TestResearchTree(t *testing.T) {
	if os.Getenv("MANTLE_E2E") == "" {
		t.Skip("set MANTLE_E2E=1 (make e2e-research)")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH (mantle's engine)")
	}
	bin, err := parity.BuildMantleUI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := workspace(t)

	// 1. Ask Q1, then Q2. Go back to Q1 and ask Q3.
	term := start(t, bin, ws, "--research")
	waitScreen(t, term, "research mode", has("⌕ research"))
	ask(t, term, "alpha question", "Alpha answer")
	ask(t, term, "beta question", "Beta answer")
	waitScreen(t, term, "Q1 as Q2's ancestor bar", has("▸ alpha question"))
	if s := term.Screen(); strings.Contains(s, "Alpha answer") {
		t.Fatalf("Q1's answer is on Q2's screen:\n%s", s)
	}
	term.Input("\x1b[1;3A") // alt+up: the parent
	waitScreen(t, term, "Q1 with its child bar", all(has("Alpha answer"), has("▾ beta question"), not(has("Beta answer"))))
	ask(t, term, "gamma question", "Gamma answer")

	// 2–3. Click the parent bar: Q1 has two child bars and is alone on screen.
	clickRow(t, term, "▸ alpha question")
	waitScreen(t, term, "Q1 with two child bars", q1WithTwoChildren)
	t.Logf("Q1 after the click:\n%s", term.Screen())

	// The engine wrote Q3 as Q2's sibling, in the same session file.
	quit(term)
	sid := sessionID(t, ws)
	if b, g := parentOf(t, ws, "beta question"), parentOf(t, ws, "gamma question"); b == "" || b != g {
		t.Fatalf("Q2 hangs off %q, Q3 off %q: not siblings", b, g)
	}

	// 4. Run --resume: the same tree, the same node.
	term = start(t, bin, ws, "--resume", sid)
	waitScreen(t, term, "Q1 restored with two child bars", q1WithTwoChildren)
	t.Logf("Q1 after --resume:\n%s", term.Screen())

	// Q2 is off the engine's branch (Q3 is the tip): its answer comes from the JSONL,
	// and a follow-up restarts the engine at Q2 (resume-at).
	clickRow(t, term, "▾ beta question")
	waitScreen(t, term, "Q2 off the branch", all(has("Beta answer"), has("▸ alpha question"), not(has("Alpha answer"))))
	ask(t, term, "delta question", "Delta answer")
	term.Input("\x1b[1;3A") // alt+up: back to Q2
	waitScreen(t, term, "Q2 with Q4 as its child", all(has("Beta answer"), has("▾ delta question")))
	quit(term)
	if b, d := parentOf(t, ws, "beta question"), parentOf(t, ws, "delta question"); d == "" || d == b {
		t.Fatalf("Q4 hangs off %q, want an entry of Q2's answer", d)
	}
	if sid2 := sessionID(t, ws); sid2 != sid {
		t.Fatalf("the branch changed the session: %s → %s", sid, sid2)
	}
}

// parentOf is the parentUuid of the session's user entry whose text is prompt.
func parentOf(t *testing.T, ws parity.Workspace, prompt string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(ws.ConfigDir, "projects", "*", "*.jsonl"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(string(data), "\n") {
			var e struct {
				Type, ParentUUID string
				Message          struct{ Content json.RawMessage }
			}
			if json.Unmarshal([]byte(l), &e) != nil || e.Type != "user" || !strings.Contains(string(e.Message.Content), prompt) {
				continue
			}
			return e.ParentUUID
		}
	}
	return ""
}

// TestResearchEntryPoints: shift+tab enters research from manual mode (fullscreen, the
// footer indicator, the follow-up hint), ctrl+t opens the tree picker, /research off
// leaves and restores the inline layout.
func TestResearchEntryPoints(t *testing.T) {
	if os.Getenv("MANTLE_E2E") == "" {
		t.Skip("set MANTLE_E2E=1 (make e2e-research)")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH (mantle's engine)")
	}
	bin, err := parity.BuildMantleUI(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := workspace(t)
	if err := os.WriteFile(filepath.Join(ws.ConfigDir, "settings.json"), []byte(`{"permissions":{"defaultMode":"default"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	term := start(t, bin, ws)
	ask(t, term, "alpha question", "Alpha answer")
	waitScreen(t, term, "manual mode", not(has("⌕ research")))

	term.Input("\x1b[Z") // shift+tab: default → research
	waitScreen(t, term, "research via shift+tab", all(has("⌕ research"), has("↳ type to ask a follow-up"), has("Alpha answer")))

	term.Input("\x14") // ctrl+t: the tree picker
	waitScreen(t, term, "tree picker", has("viewing"))
	term.Input("\x1b")
	waitScreen(t, term, "picker closed", not(has("viewing")))

	term.Input("/research off")
	waitScreen(t, term, "typed /research off", has("/research off"))
	term.Input("\r")
	waitScreen(t, term, "research off", all(not(has("⌕ research")), not(has("↳ type to ask a follow-up")), has("Alpha answer")))
	quit(term)
}

// q1WithTwoChildren: Q1's answer, both follow-ups as child bars, no other node's answer.
var q1WithTwoChildren = all(has("Alpha answer"), has("▾ beta question"), has("▾ gamma question"),
	not(has("Beta answer")), not(has("Gamma answer")), not(has("▸ alpha question")))

func workspace(t *testing.T) parity.Workspace {
	t.Helper()
	root, err := os.MkdirTemp("", "research-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			files, _ := filepath.Glob(filepath.Join(root, "config", "projects", "*", "*.jsonl"))
			for _, f := range files {
				b, _ := os.ReadFile(f)
				t.Logf("%s:\n%s", filepath.Base(f), b)
			}
		}
		_ = os.RemoveAll(root)
	})
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	ws := parity.Workspace{Root: root, WorkDir: filepath.Join(root, "work"),
		ConfigDir: filepath.Join(root, "config"), HomeDir: filepath.Join(root, "home")}
	for _, d := range []string{ws.WorkDir, ws.ConfigDir, ws.HomeDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	script := &fakeapi.Script{DefaultReply: "OK.", Turns: []fakeapi.Turn{
		{Match: &fakeapi.Match{Contains: "alpha question"}, Reply: []fakeapi.Block{{Type: "text", Text: "Alpha answer."}}},
		{Match: &fakeapi.Match{Contains: "beta question"}, Reply: []fakeapi.Block{{Type: "text", Text: "Beta answer."}}},
		{Match: &fakeapi.Match{Contains: "gamma question"}, Reply: []fakeapi.Block{{Type: "text", Text: "Gamma answer."}}},
		{Match: &fakeapi.Match{Contains: "delta question"}, Reply: []fakeapi.Block{{Type: "text", Text: "Delta answer."}}},
	}}
	srv := httptest.NewServer(fakeapi.New(script))
	t.Cleanup(srv.Close)
	ws.APIURL, ws.APIKey = srv.URL, fakeapi.FakeAPIKey
	if err := fakeapi.SeedConfig(ws.ConfigDir, ws.APIKey, ws.WorkDir); err != nil {
		t.Fatal(err)
	}
	return ws
}

func start(t *testing.T, bin string, ws parity.Workspace, args ...string) *parity.Term {
	t.Helper()
	cmd, err := parity.MantleTarget{Bin: bin}.Command(ws, &parity.Scenario{Args: args})
	if err != nil {
		t.Fatal(err)
	}
	term, err := parity.StartTerm(cmd, width, height)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	if err := term.WaitFor(parity.MantleTarget{}.Ready, wait); err != nil {
		t.Fatalf("mantle-ui not ready: %v\n%s", err, term.Screen())
	}
	return term
}

func quit(term *parity.Term) { parity.MantleTarget{}.Quit(term) }

// ask types a question and waits for its answer.
func ask(t *testing.T, term *parity.Term, q, answer string) {
	t.Helper()
	term.Input(q)
	waitScreen(t, term, "typed "+q, has(q))
	term.Input("\r")
	waitScreen(t, term, answer, has(answer))
	term.Settle(300*time.Millisecond, 5*time.Second)
}

// clickRow left-clicks the first screen row containing text (SGR mouse, 1-based).
func clickRow(t *testing.T, term *parity.Term, text string) {
	t.Helper()
	for i, l := range term.Snapshot().Screen {
		if c := strings.Index(l, text); c >= 0 {
			x := len([]rune(l[:c])) + 3
			term.Input(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, i+1, x, i+1))
			return
		}
	}
	t.Fatalf("no row with %q:\n%s", text, term.Screen())
}

func waitScreen(t *testing.T, term *parity.Term, what string, ok func(string) bool) {
	t.Helper()
	err := term.WaitFor(func(f parity.Frame) bool { return ok(strings.Join(f.Screen, "\n")) }, wait)
	if err != nil {
		t.Fatalf("waiting for %s: %v\n%s", what, err, term.Screen())
	}
}

// sessionID is the one session the runs wrote.
func sessionID(t *testing.T, ws parity.Workspace) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(ws.ConfigDir, "projects", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("want one session file, have %v", files)
	}
	return strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")
}

func has(s string) func(string) bool {
	return func(screen string) bool { return strings.Contains(screen, s) }
}

func not(p func(string) bool) func(string) bool { return func(s string) bool { return !p(s) } }

func all(ps ...func(string) bool) func(string) bool {
	return func(s string) bool {
		for _, p := range ps {
			if !p(s) {
				return false
			}
		}
		return true
	}
}
