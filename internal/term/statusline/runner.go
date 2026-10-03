package statusline

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultDebounce batches rapid updates, like Claude Code.
const DefaultDebounce = 300 * time.Millisecond

// DefaultTimeout kills a command that hangs.
const DefaultTimeout = 10 * time.Second

// ExecRequest is one command run.
type ExecRequest struct {
	Command string
	Dir     string
	Env     []string // extra variables, appended to the process environment
	Stdin   []byte
}

// ExecFunc runs a command and returns its stdout. It must stop when ctx is done.
type ExecFunc func(ctx context.Context, req ExecRequest) ([]byte, error)

// Config configures a Runner.
type Config struct {
	Command         string
	Padding         int           // spaces before each line
	RefreshInterval time.Duration // 0 = only on updates
	Dir             string        // working directory for the command
	Env             []string      // extra environment
	Debounce        time.Duration // 0 = DefaultDebounce
	Timeout         time.Duration // 0 = DefaultTimeout
	Exec            ExecFunc      // nil = sh -c
}

// Result is the outcome of one run.
type Result struct {
	Seq   uint64   // increases with every run; later wins
	Lines []string // sanitized and padded; nil when blank or failed
	Raw   []byte   // unprocessed stdout (for subagentStatusLine parsing)
	Err   error
}

// Notice is a one-line description of a failed run, for a dim footer notice.
func (r Result) Notice() string {
	if r.Err == nil {
		return ""
	}
	msg := r.Err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return "status line command failed: " + msg
}

// ErrTimeout reports a run that exceeded Config.Timeout.
var ErrTimeout = errors.New("timed out")

// Runner runs a status line command with debouncing, periodic refresh and
// cancellation. Update, Resize, Trigger and Close are safe to call from any goroutine
// and never block on the command. Results arrive on Results(); only the newest result
// is buffered, older unread ones are replaced.
type Runner struct {
	cfg Config
	out chan Result

	mu        sync.Mutex
	payload   []byte
	have      bool
	cols      int
	lines     int
	seq       uint64
	running   bool
	cancel    context.CancelFunc
	debouncer *time.Timer
	closed    bool
	stop      chan struct{}
	wg        sync.WaitGroup
}

// NewRunner starts a runner. Nothing runs until the first Update.
func NewRunner(cfg Config) *Runner {
	if cfg.Debounce <= 0 {
		cfg.Debounce = DefaultDebounce
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Exec == nil {
		cfg.Exec = ShellExec
	}
	r := &Runner{cfg: cfg, out: make(chan Result, 1), stop: make(chan struct{})}
	if cfg.RefreshInterval > 0 {
		r.wg.Add(1)
		go r.refreshLoop(cfg.RefreshInterval)
	}
	return r
}

// Config returns the runner's configuration.
func (r *Runner) Config() Config { return r.cfg }

// Results delivers run results. The channel is closed by Close.
func (r *Runner) Results() <-chan Result { return r.out }

// Update sets the payload and schedules a run after the debounce delay. Further updates
// within the delay push the run back.
func (r *Runner) Update(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.payload, r.have = payload, true
	r.schedule()
}

// Set replaces the payload later runs use (refresh ticks, resizes) without scheduling
// a run: for data that changed between the events that re-run the command.
func (r *Runner) Set(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.payload, r.have = payload, true
	}
}

// Resize records the terminal size passed as COLUMNS/LINES and schedules a run when it
// changed.
func (r *Runner) Resize(cols, lines int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || (cols == r.cols && lines == r.lines) {
		return
	}
	r.cols, r.lines = cols, lines
	if r.have {
		r.schedule()
	}
}

// Trigger runs now with the current payload, skipping the debounce (used when the
// command itself changed). It does nothing before the first Update.
func (r *Runner) Trigger() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || !r.have {
		return
	}
	if r.debouncer != nil {
		r.debouncer.Stop()
	}
	r.start()
}

// Close cancels any run, stops the timers, waits for goroutines and closes Results.
func (r *Runner) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	if r.debouncer != nil {
		r.debouncer.Stop()
	}
	if r.cancel != nil {
		r.cancel()
	}
	close(r.stop)
	r.mu.Unlock()
	r.wg.Wait()
	close(r.out)
}

// schedule (re)arms the debounce timer. Caller holds mu.
func (r *Runner) schedule() {
	if r.debouncer == nil {
		r.debouncer = time.AfterFunc(r.cfg.Debounce, r.fire)
		return
	}
	r.debouncer.Reset(r.cfg.Debounce)
}

func (r *Runner) fire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed && r.have {
		r.start()
	}
}

func (r *Runner) refreshLoop(every time.Duration) {
	defer r.wg.Done()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			r.mu.Lock()
			// A slow command is not restarted by the timer; updates still cancel it.
			if !r.closed && r.have && !r.running {
				r.start()
			}
			r.mu.Unlock()
		}
	}
}

// start cancels the in-flight run and starts a new one. Caller holds mu.
func (r *Runner) start() {
	if r.cancel != nil {
		r.cancel()
	}
	r.seq++
	seq := r.seq
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.Timeout)
	r.cancel, r.running = cancel, true
	req := ExecRequest{
		Command: r.cfg.Command,
		Dir:     r.cfg.Dir,
		Env:     r.env(),
		Stdin:   r.payload,
	}
	r.wg.Add(1)
	go r.run(ctx, cancel, seq, req)
}

func (r *Runner) env() []string {
	env := append([]string{}, r.cfg.Env...)
	if r.cols > 0 {
		env = append(env, "COLUMNS="+strconv.Itoa(r.cols))
	}
	if r.lines > 0 {
		env = append(env, "LINES="+strconv.Itoa(r.lines))
	}
	return env
}

func (r *Runner) run(ctx context.Context, cancel context.CancelFunc, seq uint64, req ExecRequest) {
	defer r.wg.Done()
	defer cancel()
	out, err := r.cfg.Exec(ctx, req)
	if len(out) > MaxOutput {
		out = out[:MaxOutput]
	}
	res := Result{Seq: seq, Raw: out}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Err = fmt.Errorf("%w after %s", ErrTimeout, r.cfg.Timeout)
	case ctx.Err() != nil:
		// Superseded or closed; dropped below.
	case err != nil:
		res.Err = err
	default:
		res.Lines = Pad(Lines(out), r.cfg.Padding)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if seq != r.seq || r.closed {
		return // a newer run started; its result wins
	}
	r.running = false
	select {
	case <-r.out:
	default:
	}
	r.out <- res
}
