package sessions

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func (f *feature) registerBranch(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("branch"), Name: "branch", Source: ext.SourceBuiltin,
		Description: "Continue in a copy of this conversation (the original stays resumable)",
		ArgHint:     "[name]",
		Run:         f.runBranch,
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("fork"), Name: "fork", Source: ext.SourceBuiltin,
		Description: "Copy this conversation into a background session",
		ArgHint:     "[prompt]",
		Run:         f.runFork,
	})
	ext.Subscribe(r, "sessions.forked", f.onForked)
}

// runBranch restarts the main engine on a fork of the current session: same history,
// new session id. The new session gets the name, if one was given, once the engine
// reports its id.
func (f *feature) runBranch(ctx ext.Ctx, args string) tea.Cmd {
	info := ctx.Session()
	if info.SessionID == "" || storeEmpty(ctx) {
		return notice(ctx, "branch", "Nothing to branch yet: send a prompt first", ext.NoticeInfo)
	}
	st := f.engine(ext.MainEngine)
	if st.state == proto.StateRunning || st.state == proto.StateRequiresAction {
		return notice(ctx, "branch", "Wait for the current turn to finish, then branch", ext.NoticeInfo)
	}
	if warn, ok := f.restartGuard(ctx, ext.MainEngine, "branch", "/branch"); !ok {
		return warn
	}
	old := info.SessionID
	f.branching = &branching{from: old, name: strings.TrimSpace(args)}
	opts := f.spawnOpts(ctx, ext.MainEngine, f.cwd(ctx), func(o *ext.SpawnOpts) {
		o.Resume, o.ForkSession = old, true
	})
	st.shown = "fork:" + old
	info.SessionID, info.Title = "", strings.TrimSpace(args)
	text := "Branched. The original conversation stays resumable"
	if t := f.titles[old]; t != "" {
		text += " as “" + t + "”"
	}
	return tea.Sequence(
		ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: info}),
		startEngine(ext.MainEngine, opts),
		notice(ctx, "branch", text, ext.NoticeSuccess),
	)
}

// branching is a /branch waiting for the fork's session id.
type branching struct {
	from, name string
}

// onBranchSession names a fresh branch when its session id arrives.
func (f *feature) onBranchSession(ctx ext.Ctx, sid string) tea.Cmd {
	b := f.branching
	if b == nil || sid == "" || sid == b.from {
		return nil
	}
	f.branching = nil
	f.engine(ext.MainEngine).shown = sid
	if b.name == "" {
		return nil
	}
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil || !eng.Supports(proto.SubRenameSession) {
		return sendCommand(ctx, ext.MainEngine, "/rename "+b.name)
	}
	name := b.name
	return controlCmd(eng.Control(proto.SubRenameSession, proto.RenameSessionRequest{Title: name}), func(r ext.ControlResultMsg) tea.Msg {
		return titledMsg{sid: sid, title: name, err: r.Err}
	})
}

type forkedMsg struct {
	out string
	err error
}

// runFork starts a background copy of the conversation (claude --bg), optionally
// with a first prompt. It shows up in `claude agents`.
func (f *feature) runFork(ctx ext.Ctx, args string) tea.Cmd {
	sid := ctx.Session().SessionID
	if sid == "" || storeEmpty(ctx) {
		return notice(ctx, "fork", "Nothing to fork yet: send a prompt first", ext.NoticeInfo)
	}
	bin, err := f.claudePath()
	if err != nil {
		return notice(ctx, "fork", "Cannot find the claude binary: "+err.Error(), ext.NoticeError)
	}
	argv := forkArgs(sid, args)
	cwd := f.cwd(ctx)
	run := f.runBackground
	return tea.Batch(
		notice(ctx, "fork", "Forking into a background session…", ext.NoticeInfo),
		func() tea.Msg {
			out, err := run(bin, cwd, argv)
			return forkedMsg{out: out, err: err}
		},
	)
}

func forkArgs(sid, prompt string) []string {
	argv := []string{"--bg", "--resume", sid, "--fork-session"}
	if p := strings.TrimSpace(prompt); p != "" {
		argv = append(argv, p)
	}
	return argv
}

// runBackgroundClaude runs a claude command that returns at once (--bg) and gives its
// output.
func runBackgroundClaude(bin, cwd string, argv []string) (string, error) {
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, bin, argv...)
	cmd.Dir = cwd
	cmd.Env = handoffEnv(cmdEnv())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (f *feature) onForked(ctx ext.Ctx, m forkedMsg) tea.Cmd {
	if m.err != nil {
		msg := m.err.Error()
		if m.out != "" {
			msg = lastLine(m.out)
		}
		return notice(ctx, "fork", "Fork failed: "+msg, ext.NoticeError)
	}
	text := "Forked into a background session"
	if m.out != "" {
		text += ": " + lastLine(m.out)
	}
	return notice(ctx, "fork", text+" (claude agents lists it)", ext.NoticeSuccess)
}

func cmdEnv() []string { return os.Environ() }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
