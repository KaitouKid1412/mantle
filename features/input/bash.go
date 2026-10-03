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
	command        string
	stdout, stderr string
	code           int
	err            error
	duration       time.Duration
}

// runBash runs a "!" command in the session's directory. Headless claude has
// no "!" path that records output in the conversation, so mantle runs the
// command and sends the command and its output as a user message, the way the
// interactive UI records them.
func (s *state) runBash(c ext.Ctx, command string) tea.Cmd {
	cwd := s.cwd
	notice := c.Notify(ext.Notice{Key: "input.bash", Text: "Running " + firstLine(command), Timeout: -1, Source: FeatureID})
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
		m := bashDoneMsg{command: command, stdout: out.String(), stderr: errb.String(), duration: time.Since(start)}
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
	done := c.Notify(ext.Notice{Key: "input.bash", Text: "Ran " + firstLine(m.command), Timeout: 2 * time.Second, Source: FeatureID})
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		return done
	}
	priority := ""
	if s.busy {
		priority = proto.PriorityLater
	}
	// Composed: command output must not trigger @path or /command expansion.
	p := ext.Prompt{Blocks: blocks, Priority: priority, UUID: uuid.NewString(), Composed: true}
	return tea.Batch(done, s.track(c, p, "!"+m.command), eng.Send(p))
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
