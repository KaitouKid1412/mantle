package input

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Limits for "!" commands.
const (
	bashTimeout   = 10 * time.Minute
	bashOutputMax = 30000 // characters kept from each of stdout and stderr
)

// bashDoneMsg is the result of a "!" command.
type bashDoneMsg struct {
	uuid           string // of the prompt that records it
	echoed         bool   // the transcript shows the command (echoPrompt)
	command        string
	stdout, stderr string
	code           int
	err            error
	duration       time.Duration
}

// runBash runs a "!" command in the session's directory. Headless claude has
// no "!" path that records output in the conversation, so mantle runs the
// command and sends the command and its output as a user message, the way the
// interactive UI records them. With the engine idle the transcript shows the
// command at once ("Running…" until its output is in); otherwise a notice
// does.
func (s *state) runBash(c ext.Ctx, command string) tea.Cmd {
	cwd := s.cwd
	id := uuid.NewString()
	in := proto.Text("<bash-input>" + command + "</bash-input>")
	notice := s.echoPrompt(c, ext.Prompt{UUID: id, Blocks: []proto.ContentBlock{in}}, ext.KeyUserBash)
	echoed := notice != nil
	if !echoed {
		notice = c.Notify(ext.Notice{Key: "input.bash", Text: "Running " + firstLine(command), Timeout: -1, Source: FeatureID})
	}
	run := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), bashTimeout)
		defer cancel()
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd := exec.CommandContext(ctx, shell, "-c", command)
		cmd.Dir = cwd
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		start := time.Now()
		err := cmd.Run()
		m := bashDoneMsg{uuid: id, echoed: echoed, command: command, stdout: out.String(), stderr: errb.String(), duration: time.Since(start)}
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			m.code = ee.ExitCode()
		case err != nil:
			m.err = err
			m.code = -1
		}
		if ctx.Err() != nil {
			m.stderr += "\nCommand timed out after " + bashTimeout.String()
		}
		return m
	}
	return tea.Batch(notice, run)
}

// bashDone sends the command and its output to the engine.
func (s *state) bashDone(c ext.Ctx, m bashDoneMsg) tea.Cmd {
	stderr := m.stderr
	if m.err != nil {
		stderr += m.err.Error()
	}
	if m.code != 0 && m.err == nil && stderr == "" {
		stderr = "exit code " + strconv.Itoa(m.code)
	}
	blocks := []proto.ContentBlock{
		proto.Text("<bash-input>" + m.command + "</bash-input>"),
		proto.Text("<bash-stdout>" + capOutput(m.stdout) + "</bash-stdout><bash-stderr>" + capOutput(stderr) + "</bash-stderr>"),
	}
	priority := ""
	if s.busy {
		priority = proto.PriorityLater
	}
	// Composed: command output must not trigger @path or /command expansion.
	// respondToBashCommands false records the output without a reply.
	respond := s.cfg.respondBash
	p := ext.Prompt{Blocks: blocks, Priority: priority, UUID: m.uuid, Composed: true, ShouldQuery: &respond}
	var done tea.Cmd
	if m.echoed {
		// The output goes into the transcript now, not when the engine
		// replays the message (which waits for any turn in progress).
		done = s.bashOutput(c, p)
	} else {
		done = c.Notify(ext.Notice{Key: "input.bash", Text: "Ran " + firstLine(m.command), Timeout: 2 * time.Second, Source: FeatureID})
	}
	return tea.Sequence(done, s.dispatch(c, p, "!"+m.command, nil))
}

// bashOutput finishes a "!" command's echo with its output.
func (s *state) bashOutput(c ext.Ctx, p ext.Prompt) tea.Cmd {
	now := c.Clock().Now()
	in := proto.NewUserInput(p.UUID, p.Blocks...)
	it := &ext.Item{
		ID: "user:" + p.UUID, Key: ext.KeyUserBash, State: ext.Done, End: now,
		Data: &proto.User{Envelope: proto.Envelope{Type: proto.TypeUser, UUID: p.UUID}, Message: in.Message},
	}
	return ext.Msg(ext.TranscriptHistoryMsg{EngineID: ext.MainEngine, Items: []*ext.Item{it}})
}

func capOutput(s string) string {
	r := []rune(s)
	if len(r) <= bashOutputMax {
		return s
	}
	cut := len(r) - bashOutputMax
	return string(r[:bashOutputMax]) + "\n… " + strconv.Itoa(cut) + " more characters"
}

func firstLine(s string) string {
	if i := bytes.IndexByte([]byte(s), '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
