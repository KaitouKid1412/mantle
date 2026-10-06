package sessions

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Hand-off to interactive Claude Code, for panels mantle has not rebuilt yet. Two ways
// in, one path:
//   - ctx.OpenDialog(DialogHandoff, []string{"/privacy-settings"}): the extra claude
//     arguments, appended after --resume <session>;
//   - the hidden command: ctx.Command("handoff") then Run(ctx, "/feedback some text"),
//     whose argument is one slash command line ("" opens the session).
//
// Both confirm first, then stop the engine, run claude in the terminal, and restart
// the engine on the same session when claude exits.
const (
	DialogHandoff  = "dialog.handoff"
	HandoffCommand = "handoff"
)

// handoff is one trip to interactive Claude Code. Two processes must never write the
// same transcript, so the engine is stopped first and restarted after.
type handoff struct {
	engineID string
	sid      string
	cwd      string
	extra    []string
	opts     ext.SpawnOpts // launch options to restart the engine with
	restart  bool          // an engine was running (or a session exists) before
	bin      string
	args     []string
	pending  bool // waiting for the current turn to end
}

func (f *feature) registerHandoff(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID:          ext.CommandID(HandoffCommand),
		Name:        HandoffCommand,
		Description: "Open this conversation in Claude Code, then come back",
		ArgHint:     "[/command args]",
		Hidden:      true,
		Source:      ext.SourceBuiltin,
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			var extra []string
			if a := strings.TrimSpace(args); a != "" {
				extra = []string{a}
			}
			return ctx.OpenDialog(DialogHandoff, extra)
		},
	})
	// /subtask runs a subagent with the full conversation; the headless engine has no
	// twin of it, so it opens in Claude Code.
	r.AddCommand(ext.Command{
		ID: ext.CommandID("subtask"), Name: "subtask", Source: ext.SourceBuiltin,
		Description: "Run a task in a subagent that sees the whole conversation (in Claude Code)",
		ArgHint:     "<task>",
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			return ctx.OpenDialog(DialogHandoff, []string{joinCommand("/subtask", args)})
		},
	})
	r.AddDialog(DialogHandoff, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		extra, _ := args.([]string)
		st := f.engine(ext.MainEngine)
		return &handoffDialog{f: f, extra: extra, tasks: st.tasks, sid: ctx.Session().SessionID}, nil
	})
	ext.Subscribe(r, "sessions.handoff-exec", f.onHandoffExec)
	ext.Subscribe(r, "sessions.handoff-done", f.onHandoffDone)
}

type handoffExecMsg struct{}

type handoffDoneMsg struct{ err error }

// startHandoff runs a confirmed hand-off: now, or when the current turn ends.
func (f *feature) startHandoff(ctx ext.Ctx, extra []string) tea.Cmd {
	if f.ho != nil {
		return notice(ctx, "handoff", "Claude Code is already open for this session", ext.NoticeInfo)
	}
	bin, err := f.claudePath()
	if err != nil {
		return notice(ctx, "handoff", "Cannot find the claude binary: "+err.Error(), ext.NoticeError)
	}
	cwd := f.cwd(ctx)
	sid := ctx.Session().SessionID
	// The session flags are chosen when each claude starts (sessionFlags): a fresh
	// session has no transcript to resume until its first prompt.
	h := &handoff{
		engineID: ext.MainEngine, sid: sid, cwd: cwd, extra: extra, bin: bin,
		restart: ctx.Engine(ext.MainEngine) != nil || sid != "",
		opts:    f.spawnOpts(ctx, ext.MainEngine, cwd, nil),
	}
	f.ho = h
	st := f.engine(ext.MainEngine)
	if st.state == proto.StateRunning || st.state == proto.StateRequiresAction {
		h.pending = true
		return notice(ctx, "handoff", "Opening in Claude Code when this turn ends", ext.NoticeInfo)
	}
	return f.beginHandoff(ctx)
}

// onIdle runs work that waited for the engine to finish its turn.
func (f *feature) onIdle(ctx ext.Ctx, engineID string) tea.Cmd {
	if h := f.ho; h != nil && h.pending && h.engineID == engineID {
		h.pending = false
		return f.beginHandoff(ctx)
	}
	return nil
}

// beginHandoff stops the engine, then execs Claude Code.
func (f *feature) beginHandoff(ctx ext.Ctx) tea.Cmd {
	h := f.ho
	cmds := []tea.Cmd{notice(ctx, "handoff", "Opening in Claude Code; exit it to return to mantle", ext.NoticeInfo)}
	if eng := ctx.Engine(h.engineID); eng != nil {
		cmds = append(cmds, eng.Restart(ext.SpawnOpts{Stop: true}))
	}
	cmds = append(cmds, ext.Msg(handoffExecMsg{}))
	return tea.Sequence(cmds...)
}

func (f *feature) onHandoffExec(ctx ext.Ctx, _ handoffExecMsg) tea.Cmd {
	h := f.ho
	if h == nil {
		return nil
	}
	// The engine has stopped, so the transcript on disk is final.
	h.args = handoffArgs(f.sessionFlags(h.cwd, h.sid))
	h.args = append(h.args, h.extra...)
	return tea.Exec(&cleanExec{Cmd: handoffProcess(h)}, func(err error) tea.Msg { return handoffDoneMsg{err: err} })
}

// clearScreen homes the cursor and erases the screen and the scrollback. Erasing only
// the screen is not enough: most terminals move what it held into the scrollback.
const clearScreen = "\x1b[H\x1b[2J\x1b[3J"

// cleanExec runs a command on a cleared terminal. Bubble Tea leaves mantle's last frame
// (transcript tail, prompt, footer) on screen when it hands the terminal over, and
// Claude Code draws inline from the cursor, so it would start below that frame. Nothing
// is lost: Claude Code shows the resumed conversation, and mantle reprints the session
// when it gets the terminal back.
type cleanExec struct {
	*exec.Cmd
	out io.Writer
}

func (c *cleanExec) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c *cleanExec) SetStdout(w io.Writer) {
	c.out = w
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c *cleanExec) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}

func (c *cleanExec) Run() error {
	if c.out != nil {
		_, _ = io.WriteString(c.out, clearScreen)
	}
	return c.Cmd.Run()
}

// onHandoffDone restarts the engine on the session and reprints its history, which
// may have grown while Claude Code had it. A session that still has no transcript
// (nothing was sent in either program) restarts fresh.
func (f *feature) onHandoffDone(ctx ext.Ctx, m handoffDoneMsg) tea.Cmd {
	h := f.ho
	f.ho = nil
	if h == nil {
		return nil
	}
	var cmds []tea.Cmd
	if m.err != nil {
		cmds = append(cmds, notice(ctx, "handoff", "Claude Code exited with an error: "+m.err.Error(), ext.NoticeWarning))
	}
	if !h.restart {
		return tea.Batch(cmds...)
	}
	opts := h.opts
	opts.Resume, opts.SessionID = f.sessionFlags(h.cwd, h.sid)
	cmds = append(cmds, startEngine(h.engineID, opts))
	if opts.Resume != "" {
		f.engine(h.engineID).loading = true
		cmds = append(cmds, f.loadCmd(loadReq{engineID: h.engineID, mode: modeRefresh, id: h.sid, cwd: h.cwd}))
	} else {
		cmds = append(cmds, ctx.Reprint()) // the hand-off cleared the terminal
	}
	return tea.Batch(cmds...)
}

// handoffArgs is the session part of the claude command line for a hand-off; the extra
// arguments follow it (a slash command runs as the first prompt).
func handoffArgs(resume, newID string) []string {
	switch {
	case resume != "":
		return []string{"--resume", resume}
	case newID != "":
		return []string{"--session-id", newID}
	}
	return nil
}

// sessionFlags picks how a claude run (Claude Code, or the engine after it) takes up
// session sid in cwd. It resumes sid when sid's transcript can be loaded. The headless
// engine writes a transcript only after the first prompt, and `--resume` of a session
// without one fails ("No conversation found"). Then it starts a new session under the
// same id, keeping mantle's view of the session, when no file of that id exists;
// claude refuses an id whose file exists, so a file without messages (a title set
// before the first prompt) means starting afresh with no id at all.
func (f *feature) sessionFlags(cwd, sid string) (resume, newID string) {
	switch {
	case !sessions.ValidID(sid):
		return "", ""
	case f.layout.Persisted(cwd, sid):
		return sid, ""
	}
	if _, err := os.Stat(sessions.SessionFile(f.layout.ProjectDir(cwd), sid)); errors.Is(err, fs.ErrNotExist) {
		return "", sid
	}
	return "", ""
}

func handoffProcess(h *handoff) *exec.Cmd {
	c := exec.Command(h.bin, h.args...)
	c.Dir = h.cwd
	c.Env = handoffEnv(os.Environ())
	return c
}

// handoffEnv drops variables that make claude think it runs inside another Claude Code
// or that belong to mantle's headless engine.
func handoffEnv(env []string) []string {
	drop := map[string]bool{
		"CLAUDECODE": true, "CLAUDE_CODE_ENTRYPOINT": true,
		"CLAUDE_CODE_EMIT_SESSION_STATE_EVENTS": true, "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING": true,
	}
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	return out
}

// handoffDialog confirms a hand-off (Confirmation context: enter, esc).
type handoffDialog struct {
	f     *feature
	extra []string
	tasks int
	sid   string
}

func (d *handoffDialog) ID() string                      { return DialogHandoff }
func (d *handoffDialog) Init(ext.Ctx) tea.Cmd            { return nil }
func (d *handoffDialog) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (d *handoffDialog) KeyContext() string              { return ext.ContextConfirmation }
func (d *handoffDialog) Placement() ext.Placement        { return ext.PlaceInline }

func (d *handoffDialog) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (d *handoffDialog) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActConfirmYes:
		return true, tea.Sequence(ctx.CloseDialog(DialogHandoff), d.f.startHandoff(ctx, d.extra))
	case ext.ActConfirmNo, ext.ActAppInterrupt:
		return true, ctx.CloseDialog(DialogHandoff)
	}
	return false, nil
}

func (d *handoffDialog) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "enter", "y":
		return d.HandleAction(ctx, ext.ActConfirmYes)
	case "esc", "n":
		return d.HandleAction(ctx, ext.ActConfirmNo)
	}
	return true, nil
}

func (d *handoffDialog) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	what := "this conversation"
	if len(d.extra) > 0 {
		what = strings.Join(d.extra, " ")
	}
	lines := []string{
		th.Fg(theme.Permission).Bold(true).Render("Open in Claude Code"),
		"",
		"Opening Claude Code for " + what + ". mantle resumes the conversation when you exit it.",
	}
	if d.sid == "" {
		lines[2] = "Opening Claude Code for " + what + " (no conversation yet). mantle returns when you exit it."
	}
	if d.tasks > 0 {
		lines = append(lines, th.Paint(theme.Warning, plural(d.tasks, "background task", "background tasks")+" will stop when mantle hands over."))
	}
	lines = append(lines, "", th.Paint(theme.Inactive, "enter continue · esc cancel"))
	return ext.Rendered{Text: wrapLines(lines, a.Width)}
}
