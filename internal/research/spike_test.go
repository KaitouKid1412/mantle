package research

// Branching spike against the real claude, offline (fakeapi + isolated config):
//
//	MANTLE_SPIKES=1 go test -run Spike -v ./internal/research
//
// With MANTLE_SPIKE_FIXTURE=1 it also rewrites testdata/fixtures/13/branching.jsonl
// (sanitized). Findings are in docs/plans/13-research-mode.md ("Spike notes").

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/engine/enginetest"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func resultFor(uuid string) func(tea.Msg) bool {
	return func(m tea.Msg) bool {
		if x, ok := m.(ext.EngineExitedMsg); ok && x.Err != nil {
			return true
		}
		ev, ok := m.(ext.EngineEventMsg)
		if !ok {
			return false
		}
		r, ok := ev.Event.(*proto.Result)
		if !ok {
			return false
		}
		if r.UserMessageUUID == uuid {
			return true
		}
		for _, u := range r.UserMessageUUIDs {
			if u == uuid {
				return true
			}
		}
		return false
	}
}

func TestSpikeBranching(t *testing.T) {
	if os.Getenv("MANTLE_SPIKES") == "" {
		t.Skip("set MANTLE_SPIKES=1")
	}
	r := enginetest.NewReal(t, &fakeapi.Script{DefaultReply: "an answer"})
	r.AutoAllow = true
	e, err := r.Manager.Start("", r.Opts())
	if err != nil {
		t.Fatal(err)
	}
	const (
		u1 = "11111111-1111-4111-8111-111111111111"
		u2 = "22222222-2222-4222-8222-222222222222"
		u3 = "33333333-3333-4333-8333-333333333333"
		u4 = "44444444-4444-4444-8444-444444444444"
	)
	send := func(e *engine.Engine, uuid, text string) {
		t.Helper()
		e.Send(ext.Prompt{UUID: uuid, Blocks: []proto.ContentBlock{proto.Text(text)}})()
		got := r.Rec.WaitFor(t, resultFor(uuid))
		if x, ok := got.(ext.EngineExitedMsg); ok {
			t.Fatalf("engine exited: %v", x.Err)
		}
	}
	send(e, u1, "Q1 what is a tree?")
	send(e, u2, "Q2 and a forest?")
	sid := e.Snapshot().Info.SessionID
	files, _ := filepath.Glob(filepath.Join(r.Config, "projects", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("session files: %v", files)
	}
	path := files[0]
	load := func() *sessions.Transcript {
		t.Helper()
		tr, err := sessions.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	tree := Build(load())
	leaf1 := tree.Node(u1).LeafUUID
	if p := load().Tree.Node(u2).Entry.ParentUUID; p != leaf1 {
		t.Errorf("Q1 LeafUUID %s, but Q2 hangs off %s", leaf1, p)
	}
	if got := Plan(tree, u1, false); got.Kind != Rewind || got.At != leaf1 || got.DropPrompt != u2 {
		t.Errorf("plan at Q1 = %+v", got)
	}

	// 1. Resume at Q1's leaf: same session id, Q3 appended to the same file as a
	// sibling of Q2.
	o := r.Opts()
	o.Resume, o.ResumeSessionAt = sid, leaf1
	if err := e.RestartNow(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	send(e, u3, "Q3 and a root?")
	if got := e.Snapshot().Info.SessionID; got != sid {
		t.Errorf("resume-at changed the session id: %s -> %s", sid, got)
	}
	files2, _ := filepath.Glob(filepath.Join(r.Config, "projects", "*", "*.jsonl"))
	t.Logf("after resume-at: session files %d", len(files2))
	if len(files2) != 1 {
		t.Errorf("resume-at wrote a new file: %v", files2)
	}
	if p := load().Tree.Node(u3).Entry.ParentUUID; p != leaf1 {
		t.Errorf("Q3 parent = %s, want Q1 leaf %s", p, leaf1)
	}

	// 2. rewind_conversation to drop Q3, then send Q4: another sibling.
	tree = Build(load())
	plan := Plan(tree, u1, false)
	if plan.Kind != Rewind || plan.DropPrompt != u3 {
		t.Errorf("plan after resume-at = %+v (engine leaf %s)", plan, tree.EngineLeaf)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rw, err := e.Rewind(ctx, u3, "")
	t.Logf("rewind: %+v err=%v supported=%v", rw, err, e.Supports(engine.SubRewindConversation))
	if err != nil {
		t.Fatal(err)
	}
	send(e, u4, "Q4 and a leaf?")
	if got := e.Snapshot().Info.SessionID; got != sid {
		t.Errorf("rewind changed the session id: %s -> %s", sid, got)
	}
	final := load()
	if p := final.Tree.Node(u4).Entry.ParentUUID; p != leaf1 {
		t.Errorf("Q4 parent = %s, want Q1 leaf %s", p, leaf1)
	}
	tree = Build(final)
	var kids []string
	for _, c := range tree.Node(u1).Children {
		kids = append(kids, c.ID)
	}
	t.Logf("Q1 children %v, engine leaf %s, active leaf %s", kids, tree.EngineLeaf, final.ActiveLeaf().Entry.UUID)
	if len(kids) != 3 || tree.EngineLeaf != u4 {
		t.Errorf("tree: children %v, engine leaf %s", kids, tree.EngineLeaf)
	}

	if os.Getenv("MANTLE_SPIKE_FIXTURE") != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b = sanitize(b, r.Work, r.Config)
		out := filepath.Join("..", "..", "testdata", "fixtures", "13", "branching.jsonl")
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (session %s)", out, sid)
	}
}

// sanitize replaces machine-specific paths and names in a session file.
func sanitize(b []byte, work, config string) []byte {
	repl := []struct{ from, to string }{{work, "/work/demo"}, {config, "/claude-config"}}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		repl = append(repl, struct{ from, to string }{home, "/home/user"})
	}
	if tmp := os.TempDir(); tmp != "" && tmp != "/" {
		repl = append(repl, struct{ from, to string }{filepath.Clean(tmp), "/tmp"})
	}
	if u, err := user.Current(); err == nil && len(u.Username) > 2 {
		repl = append(repl, struct{ from, to string }{u.Username, "user"})
	}
	for _, r := range repl {
		b = bytes.ReplaceAll(b, []byte(r.from), []byte(r.to))
	}
	// Attachments carry the engine's own prompt text (skill and tool listings): keep
	// only their type.
	var out [][]byte
	for _, line := range bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(line, &m) == nil && m["type"] == "attachment" {
			if a, ok := m["attachment"].(map[string]any); ok {
				m["attachment"] = map[string]any{"type": a["type"]}
				for k := range m {
					if strings.HasPrefix(k, "rendered") {
						delete(m, k)
					}
				}
				if nl, err := json.Marshal(m); err == nil {
					line = nl
				}
			}
		}
		out = append(out, line)
	}
	return append(bytes.Join(out, []byte("\n")), '\n')
}
