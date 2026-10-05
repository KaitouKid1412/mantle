package enginetest

// Engine passthrough (PARITY ENG-35..59, code E): the real claude, driven through
// engine.Manager against fakeapi with an isolated config (offline, free), does what
// Claude Code does. Each subtest checks one engine feature end to end.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake/fakeapi"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// passRun starts a real engine with script and opts, sends prompt and returns the
// Real, the result and the tool results seen.
type passRun struct {
	r       *Real
	res     *proto.Result
	results []proto.ToolResult
	perms   int
}

func runPass(t *testing.T, script *fakeapi.Script, prompt string, setup func(r *Real, o *ext.SpawnOpts)) passRun {
	t.Helper()
	r := NewReal(t, script)
	r.AutoAllow = true
	o := r.Opts()
	if setup != nil {
		setup(r, &o)
	}
	e, err := r.Manager.Start("", o)
	if err != nil {
		t.Fatal(err)
	}
	u := "11111111-1111-4111-8111-111111111111"
	e.Send(ext.Prompt{UUID: u, Blocks: text(prompt)})()
	got := r.Rec.WaitFor(t, func(m tea.Msg) bool {
		if x, ok := m.(ext.EngineExitedMsg); ok && x.Err != nil {
			return true
		}
		return resultFor(u)(m)
	})
	ev, ok := got.(ext.EngineEventMsg)
	if !ok {
		t.Fatalf("engine exited: %+v", got)
	}
	p := passRun{r: r, res: ev.Event.(*proto.Result)}
	for _, m := range r.Rec.Msgs() {
		switch v := m.(type) {
		case ext.PermissionMsg:
			p.perms++
		case ext.EngineEventMsg:
			if u, ok := v.Event.(*proto.User); ok && !u.IsReplay {
				p.results = append(p.results, u.ToolResults()...)
			}
		}
	}
	return p
}

func toolTurn(name string, input string) fakeapi.Turn {
	return fakeapi.Turn{Reply: []fakeapi.Block{{Type: "tool_use", Name: name, Input: json.RawMessage(input)}}}
}

func lastMainBody(t *testing.T, r *Real) string {
	t.Helper()
	rs := r.API.RequestsOf(fakeapi.KindMain)
	if len(rs) == 0 {
		t.Fatal("no main request")
	}
	return string(rs[0].Body)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRealPassthrough(t *testing.T) {
	// ENG-38: project hooks run (a SessionStart command hook writes a marker).
	t.Run("hooks", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{DefaultReply: "ok"}, "hi", func(r *Real, o *ext.SpawnOpts) {
			writeFile(t, filepath.Join(r.Work, ".claude", "settings.json"),
				`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"touch hook-ran.txt"}]}]}}`)
		})
		if _, err := os.Stat(filepath.Join(p.r.Work, "hook-ran.txt")); err != nil {
			t.Errorf("UserPromptSubmit hook did not run: %v", err)
		}
	})

	// ENG-40: plugins load from --plugin-dir (their commands become slash commands).
	t.Run("plugins", func(t *testing.T) {
		plug := t.TempDir()
		writeFile(t, filepath.Join(plug, ".claude-plugin", "plugin.json"), `{"name":"mantleprobe","version":"0.0.1","description":"test plugin"}`)
		writeFile(t, filepath.Join(plug, "commands", "greet.md"), "---\ndescription: Greet\n---\nSay hello.\n")
		r := NewReal(t, &fakeapi.Script{})
		o := r.Opts()
		o.ExtraArgs = append(o.ExtraArgs, "--plugin-dir", plug)
		if _, err := r.Manager.Start("", o); err != nil {
			t.Fatal(err)
		}
		cm := r.Rec.WaitFor(t, func(m tea.Msg) bool { _, ok := m.(ext.CommandsMsg); return ok }).(ext.CommandsMsg)
		var names []string
		for _, c := range cm.Commands {
			names = append(names, c.Name)
		}
		if !strings.Contains(strings.Join(names, " "), "mantleprobe:greet") {
			t.Errorf("plugin command missing: %v", names)
		}
	})

	// ENG-46: permission rules: an allow rule runs without a prompt; a deny rule is
	// refused without a prompt.
	t.Run("permission-rules", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{
			{Reply: []fakeapi.Block{bash("touch allowed.txt"), bash("rm -f allowed.txt")}},
		}, DefaultReply: "done"}, "go", func(r *Real, o *ext.SpawnOpts) {
			o.Settings = `{"permissions":{"allow":["Bash(touch:*)"],"deny":["Bash(rm:*)"]}}`
		})
		if p.perms != 0 {
			t.Errorf("rules should decide without prompting, got %d prompts", p.perms)
		}
		if _, err := os.Stat(filepath.Join(p.r.Work, "allowed.txt")); err != nil {
			t.Error("allowed command did not run (or the denied rm ran)")
		}
		if len(p.res.PermissionDenials) != 1 || p.res.PermissionDenials[0].ToolName != "Bash" {
			t.Errorf("denials %+v", p.res.PermissionDenials)
		}
	})

	// ENG-59: @path and /command expand in the engine; client_composed turns it off.
	t.Run("expansion", func(t *testing.T) {
		setup := func(r *Real, o *ext.SpawnOpts) {
			writeFile(t, filepath.Join(r.Work, "notes.txt"), "FILE-MARKER-4711\n")
			writeFile(t, filepath.Join(r.Work, ".claude", "commands", "shout.md"), "Say CMD-MARKER-0815 about $ARGUMENTS.\n")
		}
		at := runPass(t, &fakeapi.Script{DefaultReply: "ok"}, "summarize @notes.txt", setup)
		if !strings.Contains(lastMainBody(t, at.r), "FILE-MARKER-4711") {
			t.Error("@notes.txt was not expanded")
		}
		cmd := runPass(t, &fakeapi.Script{DefaultReply: "ok"}, "/shout tests", setup)
		if b := lastMainBody(t, cmd.r); !strings.Contains(b, "CMD-MARKER-0815") || !strings.Contains(b, "tests") {
			t.Error("/shout was not expanded")
		}
		r := NewReal(t, &fakeapi.Script{DefaultReply: "ok"})
		o := r.Opts()
		setup(r, &o)
		e, _ := r.Manager.Start("", o)
		u := "22222222-2222-4222-8222-222222222222"
		e.Send(ext.Prompt{UUID: u, Composed: true, Blocks: text("summarize @notes.txt")})()
		r.Rec.WaitFor(t, resultFor(u))
		if strings.Contains(lastMainBody(t, r), "FILE-MARKER-4711") {
			t.Error("client_composed must disable @ expansion")
		}
	})

	// ENG-57: the Bash tool sees variables from CLAUDE_ENV_FILE.
	t.Run("env-file", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{{Reply: []fakeapi.Block{bash("echo $MANTLE_ENV_PROBE > env.txt")}}}, DefaultReply: "done"},
			"go", func(r *Real, o *ext.SpawnOpts) {
				envFile := filepath.Join(t.TempDir(), "env.sh")
				writeFile(t, envFile, "export MANTLE_ENV_PROBE=from-env-file\n")
				o.Env["CLAUDE_ENV_FILE"] = envFile
			})
		b, _ := os.ReadFile(filepath.Join(p.r.Work, "env.txt"))
		if strings.TrimSpace(string(b)) != "from-env-file" {
			t.Errorf("env.txt %q", b)
		}
	})

	// ENG-45 / ENG-54: an output style from flag settings wins over project settings
	// (settings precedence inside the engine) and is the session's style.
	t.Run("settings-precedence-output-style", func(t *testing.T) {
		r := NewReal(t, &fakeapi.Script{})
		writeFile(t, filepath.Join(r.Work, ".claude", "settings.json"), `{"outputStyle":"Concise"}`)
		o := r.Opts()
		o.Settings = `{"outputStyle":"Explanatory"}`
		e, err := r.Manager.Start("", o)
		if err != nil {
			t.Fatal(err)
		}
		init := r.Rec.WaitFor(t, isInitDone).(ext.ControlResultMsg)
		var ir proto.InitializeResponse
		json.Unmarshal(init.Resp, &ir)
		if ir.OutputStyle != "Explanatory" || len(ir.AvailableOutputStyles) < 2 {
			t.Errorf("output style %q of %v", ir.OutputStyle, ir.AvailableOutputStyles)
		}
		raw, err := e.Request(context.Background(), proto.GetSettingsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		var st proto.SettingsResponse
		json.Unmarshal(raw, &st)
		var sources []string
		for _, s := range st.Sources {
			sources = append(sources, s.Source)
		}
		if !strings.Contains(string(st.Effective), `"Explanatory"`) || !strings.Contains(strings.Join(sources, ","), "projectSettings") ||
			!strings.Contains(strings.Join(sources, ","), "flagSettings") {
			t.Errorf("settings effective=%s sources=%v", st.Effective, sources)
		}
	})

	// ENG-52: model aliases resolve in the engine (the API sees the full model id).
	t.Run("model-alias", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{DefaultReply: "ok"}, "hi", func(r *Real, o *ext.SpawnOpts) { o.Model = "haiku" })
		var body struct {
			Model string `json:"model"`
		}
		json.Unmarshal([]byte(lastMainBody(t, p.r)), &body)
		if !strings.HasPrefix(body.Model, "claude-haiku-") {
			t.Errorf("alias resolved to %q", body.Model)
		}
	})

	// ENG-53: requests use prompt caching (cache_control breakpoints).
	t.Run("prompt-caching", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{DefaultReply: "ok"}, "hi", nil)
		if !strings.Contains(lastMainBody(t, p.r), `"cache_control"`) {
			t.Error("no cache_control in the API request")
		}
	})

	// ENG-47: file checkpointing: after a Write, rewind_files can restore it (dry run).
	t.Run("checkpointing", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "x.txt")
		in, _ := json.Marshal(map[string]string{"file_path": target, "content": "one\n"})
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{toolTurn("Write", string(in))}, DefaultReply: "written"}, "write", nil)
		e := p.r.Manager.Engine("")
		raw, err := e.Request(context.Background(), proto.RewindFilesRequest{UserMessageID: "11111111-1111-4111-8111-111111111111", DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		var rw proto.RewindFilesResponse
		json.Unmarshal(raw, &rw)
		if !rw.CanRewind || !strings.Contains(string(rw.FilesChanged), "x.txt") {
			t.Errorf("rewind_files: %s", raw)
		}
	})

	// ENG-43: the sandbox confines Bash (a write outside the project fails, with
	// autoAllowBashIfSandboxed there is no prompt).
	t.Run("sandbox", func(t *testing.T) {
		outside := t.TempDir()
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{{Reply: []fakeapi.Block{bash("touch " + outside + "/sb.txt")}}}, DefaultReply: "done"},
			"go", func(r *Real, o *ext.SpawnOpts) {
				o.Settings = `{"sandbox":{"enabled":true,"autoAllowBashIfSandboxed":true}}`
			})
		if _, err := os.Stat(filepath.Join(outside, "sb.txt")); err == nil {
			t.Error("the sandbox let Bash write outside the project")
		}
		if p.perms != 0 || len(p.results) != 1 || !p.results[0].IsError {
			t.Errorf("perms=%d results=%+v", p.perms, p.results)
		}
	})

	// ENG-49: worktrees: EnterWorktree creates one and the session moves into it.
	t.Run("worktree", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{toolTurn("EnterWorktree", `{"name":"wt1"}`)}, DefaultReply: "done"},
			"go", func(r *Real, o *ext.SpawnOpts) {
				for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "i", "--allow-empty"}} {
					exec.Command("git", append([]string{"-C", r.Work}, args...)...).Run()
				}
			})
		if _, err := os.Stat(filepath.Join(p.r.Work, ".claude", "worktrees", "wt1")); err != nil || len(p.results) != 1 || p.results[0].IsError {
			t.Errorf("worktree: %v %+v", err, p.results)
		}
	})

	// ENG-50: workflows launch (after the launch approval through can_use_tool).
	t.Run("workflow", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{toolTurn("Workflow",
			`{"script":"export const meta = { name: 'probe', description: 'probe', phases: [] }\nreturn 42"}`)}, DefaultReply: "done"}, "go", nil)
		if p.perms != 1 || len(p.results) != 1 || p.results[0].IsError || !strings.Contains(p.results[0].Content.PlainText(), "launched") {
			t.Errorf("workflow perms=%d results=%+v", p.perms, p.results)
		}
	})

	// ENG-48 / ENG-51: scheduling and cross-session tools run (CronCreate, ListAgents).
	t.Run("cron-and-agents", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{{Reply: []fakeapi.Block{
			{Type: "tool_use", Name: "CronCreate", Input: json.RawMessage(`{"cron":"*/5 * * * *","prompt":"hi","recurring":false}`)},
			{Type: "tool_use", Name: "ListAgents", Input: json.RawMessage(`{}`)},
		}}}, DefaultReply: "done"}, "go", nil)
		if len(p.results) != 2 || p.results[0].IsError || p.results[1].IsError {
			t.Errorf("results %+v", p.results)
		}
	})

	// ENG-44: auto mode is accepted and decisions come back through mantle. The
	// classifier itself is a model call inside the engine: with an API-key session
	// against fakeapi it falls back to asking, which this checks.
	t.Run("auto-mode", func(t *testing.T) {
		p := runPass(t, &fakeapi.Script{Turns: []fakeapi.Turn{{Reply: []fakeapi.Block{bash("touch auto.txt")}}}, DefaultReply: "done"},
			"go", func(r *Real, o *ext.SpawnOpts) { o.PermissionMode = proto.ModeAuto })
		if p.perms != 1 {
			t.Errorf("auto-mode prompts %d", p.perms)
		}
		if _, err := os.Stat(filepath.Join(p.r.Work, "auto.txt")); err != nil {
			t.Error("approved command did not run")
		}
	})
}
