package enginetest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/engine"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func unsupported(subtype string) enginefake.Step {
	return enginefake.Step{On: json.RawMessage(`{"type":"control_request","request":{"subtype":"` + subtype + `"}}`),
		RespondError: "Unsupported control request subtype: " + subtype}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := realDir(t)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	run("add", ".")
	run("commit", "-qm", "init")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	return dir
}

func TestUnstableFallbacks(t *testing.T) {
	repo := gitRepo(t)
	main := enginefake.New(enginefake.InitializeRule(nil),
		unsupported(engine.SubGetWorkspaceDiff), unsupported(engine.SubSideQuestion), unsupported(engine.SubRewindConversation))
	fork := enginefake.MustParse(`
{"on": {"type":"control_request","request":{"subtype":"initialize"}}, "respond": {"commands":[]}}
{"expect": {"type":"user","message":{"content":"what changed?"}}}
{"emit": {"type":"result","subtype":"success","is_error":false,"result":"Only a.txt.","user_message_uuid":"${uuid}"}}
`)
	m, sp, rec := setup(t, main, fork, main)
	e, err := m.Start("", ext.SpawnOpts{Cwd: repo, Resume: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	rec.WaitFor(t, isInitialized)
	ctx := context.Background()

	// get_workspace_diff -> git diff.
	d, err := e.WorkspaceDiff(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !d.FromGit || d.Stats.FilesCount != 2 || d.Stats.LinesAdded != 2 || d.Stats.LinesRemoved != 1 {
		t.Errorf("diff stats %+v %+v", d.Stats, d.PerFileStats)
	}
	if len(d.Hunks) != 1 || d.Hunks[0].Path != "a.txt" || strings.Join(d.Hunks[0].Hunks[0].Lines, "|") != " one|-two|+TWO| three|+four" {
		t.Errorf("hunks %+v", d.Hunks)
	}
	if e.Supports(engine.SubGetWorkspaceDiff) {
		t.Error("unsupported subtype should be disabled now")
	}

	// side_question -> forked engine.
	a, err := e.SideQuestion(ctx, "what changed?", nil)
	if err != nil || a.Response != "Only a.txt." || !a.Forked {
		t.Fatalf("side question: %+v %v", a, err)
	}
	forkArgs := strings.Join(sp.Specs()[1].Args, " ")
	for _, w := range []string{"--resume=sess-1", "--fork-session", "--no-session-persistence"} {
		if !strings.Contains(forkArgs, w) {
			t.Errorf("fork args missing %s: %s", w, forkArgs)
		}
	}
	for _, msg := range rec.Msgs() {
		if a, ok := msg.(ext.EngineAttachMsg); ok && a.EngineID != ext.MainEngine {
			t.Errorf("the fork must stay private, but %s reached the UI", a.EngineID)
		}
	}

	// rewind_conversation -> restart at the preceding assistant entry.
	if _, err := e.Rewind(ctx, "u-2", ""); !errors.Is(err, engine.ErrNeedsTranscript) {
		t.Errorf("rewind without transcript data: %v", err)
	}
	r, err := e.Rewind(ctx, "u-2", "a-1")
	if err != nil || !r.Restarted {
		t.Fatalf("rewind: %+v %v", r, err)
	}
	args := strings.Join(sp.Specs()[2].Args, " ")
	if !strings.Contains(args, "--resume=sess-1 --resume-session-at=a-1 --resume-drops-turn=a-1") {
		t.Errorf("rewind restart args: %s", args)
	}
}

// TestRealUnstable: the real engine answers all three natively (offline).
func TestRealUnstable(t *testing.T) {
	r := NewReal(t, &fakeapi.Script{DefaultReply: "an answer"})
	repo := gitRepo(t)
	o := r.Opts()
	o.Cwd = repo
	e, err := r.Manager.Start("", o)
	if err != nil {
		t.Fatal(err)
	}
	u := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u, Blocks: text("first")})()
	r.Rec.WaitFor(t, resultFor(u))
	ctx := context.Background()
	d, err := e.WorkspaceDiff(ctx)
	if err != nil || d.FromGit || d.Stats.FilesCount < 1 {
		t.Errorf("diff: %+v %v", d.Stats, err)
	}
	a, err := e.SideQuestion(ctx, "anything?", nil)
	if err != nil || a.Forked || a.Response == "" {
		t.Errorf("side question: %+v %v", a, err)
	}
	rw, err := e.Rewind(ctx, u, "")
	if err != nil || !rw.Rewound || rw.Restarted || rw.PrefillText != "first" {
		t.Errorf("rewind: %+v %v", rw, err)
	}
}
