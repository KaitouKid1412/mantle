package sessions

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/clipcmd"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// /btw asks a side question about the conversation without interrupting it. The
// engine answers it from the live conversation without tools (side_question);
// engines that can't get a forked one-shot claude instead. Answers show in an overlay
// that keeps the last 20 exchanges.

const (
	DialogBtw  = "dialog.btw"
	subSideQ   = "side_question"
	btwKeep    = 20
	btwContext = "SideQuestion" // mantle's own keybinding context: the overlay reads raw keys
)

type btwExchange struct {
	id               int
	question, answer string
	err              error
	loading          bool
}

type btwAnswerMsg struct {
	id     int
	answer string
	err    error
}

// btwFallbackMsg: the engine could not answer; ask a forked one-shot claude.
type btwFallbackMsg struct {
	id       int
	question string
	history  []btwTurn
}

// btwTurn is one earlier exchange, as side_question takes it.
type btwTurn struct {
	Question string `json:"question"`
	Response string `json:"response"`
}

func (f *feature) registerBtw(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("btw"), Name: "btw", Source: ext.SourceBuiltin,
		Description: "Ask a side question without interrupting the conversation",
		ArgHint:     "<question>",
		Run:         f.runBtw,
	})
	r.AddDialog(DialogBtw, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		return &btwOverlay{f: f}, nil
	})
	ext.Subscribe(r, "sessions.btw-answer", f.onBtwAnswer)
	ext.Subscribe(r, "sessions.btw-fallback", func(ctx ext.Ctx, m btwFallbackMsg) tea.Cmd {
		return f.askOneShot(ctx, m.id, m.question, m.history)
	})
	ext.Subscribe(r, "sessions.btw-closed", func(ctx ext.Ctx, m ext.DialogClosedMsg) tea.Cmd {
		if m.ID == DialogBtw {
			f.btwOpen = false
		}
		return nil
	})
}

func (f *feature) runBtw(ctx ext.Ctx, args string) tea.Cmd {
	q := strings.TrimSpace(args)
	if q == "" {
		if len(f.btw) == 0 {
			return notice(ctx, "btw", "Usage: /btw <question>", ext.NoticeInfo)
		}
		return f.openBtw(ctx)
	}
	var history []btwTurn
	for _, ex := range f.btw {
		if !ex.loading && ex.err == nil && ex.answer != "" {
			history = append(history, btwTurn{Question: ex.question, Response: ex.answer})
		}
	}
	f.btwSeq++
	ex := &btwExchange{id: f.btwSeq, question: q, loading: true}
	f.btw = append(f.btw, ex)
	if len(f.btw) > btwKeep {
		f.btw = f.btw[len(f.btw)-btwKeep:]
	}
	f.btwSel = len(f.btw) - 1
	return tea.Batch(f.openBtw(ctx), f.askSide(ctx, ex.id, q, history))
}

func (f *feature) openBtw(ctx ext.Ctx) tea.Cmd {
	if f.btwOpen {
		ctx.Invalidate(DialogBtw)
		return nil
	}
	f.btwOpen = true
	return ctx.OpenDialog(DialogBtw, nil)
}

// askSide asks the engine (side_question), falling back to a one-shot claude.
func (f *feature) askSide(ctx ext.Ctx, id int, q string, history []btwTurn) tea.Cmd {
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil || !eng.Supports(subSideQ) {
		return f.askOneShot(ctx, id, q, history)
	}
	if history == nil {
		history = []btwTurn{}
	}
	fields, _ := json.Marshal(map[string]any{"question": q, "history": history})
	return controlCmd(eng.Control(subSideQ, proto.RawRequest{Subtype: subSideQ, Fields: fields}), func(r ext.ControlResultMsg) tea.Msg {
		var resp struct {
			Response *string `json:"response"`
		}
		if err := decodeControl(r, &resp); err != nil {
			return btwFallbackMsg{id: id, question: q, history: history}
		}
		if resp.Response == nil || strings.TrimSpace(*resp.Response) == "" {
			return btwAnswerMsg{id: id, err: errNoAnswer}
		}
		return btwAnswerMsg{id: id, answer: strings.TrimSpace(*resp.Response)}
	})
}

type btwError string

func (e btwError) Error() string { return string(e) }

const errNoAnswer = btwError("no answer for this side question; try again, or ask in the conversation")

// askOneShot runs claude -p on a fork of the session that is not saved, with no tools.
func (f *feature) askOneShot(ctx ext.Ctx, id int, q string, history []btwTurn) tea.Cmd {
	bin, err := f.claudePath()
	if err != nil {
		return ext.Msg(btwAnswerMsg{id: id, err: err})
	}
	argv := btwArgs(ctx.Session().SessionID, q, history)
	cwd, run := f.cwd(ctx), f.runSide
	return func() tea.Msg {
		out, err := run(bin, cwd, argv)
		if err != nil && out != "" {
			err = btwError(lastLine(out))
		}
		return btwAnswerMsg{id: id, answer: strings.TrimSpace(out), err: err}
	}
}

func btwArgs(sid, q string, history []btwTurn) []string {
	argv := []string{"-p"}
	if sid != "" {
		argv = append(argv, "--resume", sid, "--fork-session")
	}
	argv = append(argv, "--no-session-persistence", "--tools", "")
	prompt := q
	if n := len(history); n > 0 {
		var b strings.Builder
		b.WriteString("Earlier side questions in this aside:\n")
		for _, t := range history[max(0, n-3):] {
			b.WriteString("Q: " + t.Question + "\nA: " + t.Response + "\n")
		}
		b.WriteString("\nSide question: " + q)
		prompt = b.String()
	}
	return append(argv, prompt)
}

// runClaudeText runs a claude -p command and returns its text output.
func runClaudeText(bin, cwd string, argv []string) (string, error) {
	c, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(c, bin, argv...)
	cmd.Dir = cwd
	cmd.Env = handoffEnv(cmdEnv())
	out, err := cmd.Output()
	return string(out), err
}

func (f *feature) onBtwAnswer(ctx ext.Ctx, m btwAnswerMsg) tea.Cmd {
	for _, ex := range f.btw {
		if ex.id == m.id {
			ex.loading, ex.answer, ex.err = false, m.answer, m.err
		}
	}
	ctx.Invalidate(DialogBtw)
	if !f.btwOpen {
		return notice(ctx, "btw", "Your side question has an answer: /btw shows it", ext.NoticeInfo)
	}
	return nil
}

// btwOverlay shows one exchange at a time.
type btwOverlay struct{ f *feature }

func (o *btwOverlay) ID() string                      { return DialogBtw }
func (o *btwOverlay) Init(ext.Ctx) tea.Cmd            { return nil }
func (o *btwOverlay) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (o *btwOverlay) KeyContext() string              { return btwContext }
func (o *btwOverlay) Placement() ext.Placement        { return ext.PlaceInline }

func (o *btwOverlay) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (o *btwOverlay) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if a == ext.ActAppInterrupt {
		return true, o.close(ctx)
	}
	return false, nil
}

func (o *btwOverlay) close(ctx ext.Ctx) tea.Cmd {
	o.f.btwOpen = false
	return ctx.CloseDialog(DialogBtw)
}

func (o *btwOverlay) current() *btwExchange {
	f := o.f
	if len(f.btw) == 0 {
		return nil
	}
	f.btwSel = min(max(f.btwSel, 0), len(f.btw)-1)
	return f.btw[f.btwSel]
}

func (o *btwOverlay) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	defer ctx.Invalidate(DialogBtw)
	f := o.f
	switch k.String() {
	case "esc", "q", "enter":
		return true, o.close(ctx)
	case "shift+left", "left":
		f.btwSel = max(f.btwSel-1, 0)
	case "shift+right", "right":
		f.btwSel = min(f.btwSel+1, len(f.btw)-1)
	case "c":
		if ex := o.current(); ex != nil && ex.answer != "" {
			return true, clipcmd.Copy(tagCopy, ex.answer)
		}
	case "f":
		if ex := o.current(); ex != nil {
			return true, tea.Sequence(o.close(ctx), f.runFork(ctx, ex.question))
		}
	case "x":
		f.btw, f.btwSel = nil, 0
		return true, o.close(ctx)
	}
	return true, nil
}

func (o *btwOverlay) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 20)
	ex := o.current()
	if ex == nil {
		return ext.Rendered{Text: th.Paint(theme.Inactive, "No side questions yet.")}
	}
	head := th.Fg(theme.Accent).Bold(true).Render("btw")
	if n := len(o.f.btw); n > 1 {
		head += th.Paint(theme.Inactive, " · "+itoa(o.f.btwSel+1)+"/"+itoa(n)+" · shift+←→ earlier questions")
	}
	lines := []string{fit(head, w), fit(th.Paint(theme.Text, "> "+oneLine(ex.question)), w), ""}
	var body []string
	switch {
	case ex.loading:
		body = []string{th.Paint(theme.Inactive, "Thinking…")}
	case ex.err != nil:
		body = []string{th.Paint(theme.Error, ex.err.Error())}
	default:
		body = strings.Split(wrapLines(strings.Split(ex.answer, "\n"), w), "\n")
	}
	limit := a.MaxHeight - len(lines) - 2
	if limit <= 0 {
		limit = 16
	}
	if len(body) > limit {
		more := len(body) - limit + 1
		body = append(body[:limit-1], th.Paint(theme.Inactive, "… +"+itoa(more)+" lines (c copies all)"))
	}
	lines = append(lines, body...)
	lines = append(lines, "", fit(th.Paint(theme.Inactive, "c copy · f fork into a session · x clear · esc close"), w))
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
