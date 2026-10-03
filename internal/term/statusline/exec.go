package statusline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ShellExec runs req.Command with `sh -c`, the payload on stdin and stdout captured.
// The command runs in its own process group, and the whole group is killed when ctx
// ends, so helpers it started (git, jq) die with it. A non-zero exit returns an error
// carrying the first line of stderr.
func ShellExec(ctx context.Context, req ExecRequest) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", req.Command)
	cmd.Dir = req.Dir
	cmd.Env = append(os.Environ(), req.Env...)
	cmd.Stdin = bytes.NewReader(req.Stdin)
	stdout := &limitedBuffer{max: MaxOutput}
	stderr := &limitedBuffer{max: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	setProcessGroup(cmd)
	cmd.WaitDelay = 500 * time.Millisecond
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			if i := strings.IndexByte(msg, '\n'); i >= 0 {
				msg = msg[:i]
			}
			return stdout.Bytes(), fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// limitedBuffer keeps the first max bytes and discards the rest without failing the
// writer (a failing write would kill the command with SIGPIPE). The buffer is a named
// field, not embedded, so io.Copy cannot bypass Write through bytes.Buffer.ReadFrom.
type limitedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }
