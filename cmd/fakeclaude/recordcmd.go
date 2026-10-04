package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/KaitouKid1412/mantle/internal/engine/fixture"
	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

const recordUsage = `usage: fakeclaude record -o out.jsonl [-raw] [-drive client.jsonl] [-timeout 2m] -- claude [args...]

Runs a real engine and records both directions (timestamped, sanitised unless -raw)
into out.jsonl. Without -drive it proxies this process's stdin/stdout (use it as
MANTLE_CLAUDE_BIN through a wrapper script). With -drive it runs a client-mode
enginefake script against the engine, then closes its stdin.`

// record implements `fakeclaude record`.
func record(args []string) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	out := fs.String("o", "", "fixture file to write")
	raw := fs.Bool("raw", false, "do not sanitise (never commit raw recordings)")
	drive := fs.String("drive", "", "client-mode script to drive the engine")
	timeout := fs.Duration("timeout", 2*time.Minute, "give up after this long (with -drive)")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, recordUsage) }
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cmdArgs := fs.Args()
	if *out == "" || len(cmdArgs) == 0 {
		fs.Usage()
		return 2
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer f.Close()
	var san *fixture.Sanitizer
	if !*raw {
		san = fixture.NewSanitizer()
	}
	rec := fixture.NewRecorder(f, san)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	toEngine := &tapWriter{w: stdin, rec: rec, dir: fixture.In}
	var fromEngine io.Reader = &tapReader{r: stdout, rec: rec, dir: fixture.Out}

	code := 0
	if *drive != "" {
		s, err := enginefake.ParseFile(*drive)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = cmd.Process.Kill()
			return 2
		}
		s.Client = true
		// The script's own reader keeps reading (and recording) engine output after
		// the steps end; eof tells us when the engine closed stdout.
		eof := &eofReader{r: fromEngine, done: make(chan struct{})}
		dctx, cancel := context.WithTimeout(ctx, *timeout)
		_, derr := s.Run(dctx, eof, toEngine, os.Stderr)
		cancel()
		if derr != nil {
			code = 3
		}
		_ = stdin.Close()
		done := eof.done
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			<-done
		}
	} else {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = io.Copy(toEngine, os.Stdin)
			_ = stdin.Close()
		}()
		_, _ = io.Copy(os.Stdout, fromEngine)
	}
	werr := cmd.Wait()
	if err := rec.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "record:", err)
		return 1
	}
	if code == 0 && werr != nil {
		if ee, ok := werr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return code
}

// eofReader closes done once its reader returns an error (EOF or closed pipe).
type eofReader struct {
	r    io.Reader
	done chan struct{}
	once sync.Once
}

func (e *eofReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil {
		e.once.Do(func() { close(e.done) })
	}
	return n, err
}

// tapWriter records each complete line written through it.
type tapWriter struct {
	w   io.Writer
	rec *fixture.Recorder
	dir string
	buf bytes.Buffer
}

func (t *tapWriter) Write(p []byte) (int, error) {
	t.buf.Write(p)
	for {
		i := bytes.IndexByte(t.buf.Bytes(), '\n')
		if i < 0 {
			break
		}
		t.rec.Record(t.dir, t.buf.Next(i+1))
	}
	return t.w.Write(p)
}

// tapReader records each complete line read through it.
type tapReader struct {
	r   io.Reader
	rec *fixture.Recorder
	dir string
	br  *bufio.Reader
	buf []byte
}

func (t *tapReader) Read(p []byte) (int, error) {
	if t.br == nil {
		t.br = bufio.NewReaderSize(t.r, 1<<20)
	}
	if len(t.buf) == 0 {
		line, err := t.br.ReadBytes('\n')
		if len(line) > 0 {
			t.rec.Record(t.dir, line)
			t.buf = line
		}
		if len(t.buf) == 0 {
			return 0, err
		}
	}
	n := copy(p, t.buf)
	t.buf = t.buf[n:]
	return n, nil
}
