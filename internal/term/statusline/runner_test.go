package statusline

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// fakeExec records runs. Each run blocks until released or cancelled.
type fakeExec struct {
	mu      sync.Mutex
	reqs    []ExecRequest
	release chan struct{} // nil: return immediately
	out     func(req ExecRequest) ([]byte, error)
	started int
	ended   int
}

func (f *fakeExec) exec(ctx context.Context, req ExecRequest) ([]byte, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.started++
	rel := f.release
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.ended++
		f.mu.Unlock()
	}()
	if rel != nil {
		select {
		case <-rel:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.out != nil {
		return f.out(req)
	}
	return append([]byte("out:"), req.Stdin...), nil
}

func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeExec) last() ExecRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

// recv returns the buffered result, or false.
func recv(r *Runner) (Result, bool) {
	select {
	case res, ok := <-r.Results():
		return res, ok
	default:
		return Result{}, false
	}
}

func TestRunnerDebounce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec})
		defer r.Close()

		r.Update([]byte("1"))
		time.Sleep(100 * time.Millisecond)
		r.Update([]byte("2"))
		time.Sleep(100 * time.Millisecond)
		r.Update([]byte("3"))
		time.Sleep(299 * time.Millisecond)
		synctest.Wait()
		if fx.count() != 0 {
			t.Fatalf("ran before the debounce settled: %d", fx.count())
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if fx.count() != 1 || string(fx.last().Stdin) != "3" {
			t.Fatalf("runs = %d, last stdin %q", fx.count(), fx.last().Stdin)
		}
		res, ok := recv(r)
		if !ok || res.Err != nil || !slices.Equal(res.Lines, []string{"out:3"}) {
			t.Errorf("result = %+v, %v", res, ok)
		}
	})
}

func TestRunnerCancelsInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{release: make(chan struct{})}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec})
		defer r.Close()

		r.Update([]byte("old"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		if fx.started != 1 {
			t.Fatal("first run should have started")
		}
		r.Update([]byte("new"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		// The old run was cancelled when the new one started.
		if fx.started != 2 || fx.ended != 1 {
			t.Fatalf("started=%d ended=%d", fx.started, fx.ended)
		}
		if _, ok := recv(r); ok {
			t.Fatal("cancelled run must not publish")
		}
		close(fx.release)
		synctest.Wait()
		res, ok := recv(r)
		if !ok || !slices.Equal(res.Lines, []string{"out:new"}) || res.Seq != 2 {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestRunnerKeepsOnlyLatestResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec})
		defer r.Close()
		r.Update([]byte("a"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		r.Update([]byte("b"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		res, _ := recv(r)
		if !slices.Equal(res.Lines, []string{"out:b"}) {
			t.Errorf("result = %+v", res)
		}
		if _, ok := recv(r); ok {
			t.Error("older unread result should have been replaced")
		}
	})
}

func TestRunnerRefreshInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, RefreshInterval: 2 * time.Second})
		defer r.Close()

		time.Sleep(5 * time.Second)
		synctest.Wait()
		if fx.count() != 0 {
			t.Fatal("refresh must wait for a first payload")
		}
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		n := fx.count()
		time.Sleep(4 * time.Second)
		synctest.Wait()
		if got := fx.count() - n; got != 2 {
			t.Errorf("refresh runs in 4s = %d, want 2", got)
		}
	})
}

func TestRunnerRefreshSkipsWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{release: make(chan struct{})}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, RefreshInterval: time.Second, Timeout: time.Minute})
		defer r.Close()
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if fx.started != 1 {
			t.Errorf("slow run restarted by the timer: started=%d", fx.started)
		}
		close(fx.release)
	})
}

func TestRunnerTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{release: make(chan struct{})}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, Timeout: 2 * time.Second})
		defer r.Close()
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce + 2*time.Second)
		synctest.Wait()
		res, ok := recv(r)
		if !ok || !errors.Is(res.Err, ErrTimeout) || res.Lines != nil {
			t.Fatalf("result = %+v", res)
		}
		if !strings.Contains(res.Notice(), "timed out") {
			t.Errorf("Notice = %q", res.Notice())
		}
	})
}

func TestRunnerError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{out: func(ExecRequest) ([]byte, error) {
			return []byte("partial"), errors.New("exit status 1: jq: not found\nmore")
		}}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec})
		defer r.Close()
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		res, _ := recv(r)
		if res.Err == nil || res.Lines != nil {
			t.Fatalf("result = %+v", res)
		}
		if got := res.Notice(); got != "status line command failed: exit status 1: jq: not found" {
			t.Errorf("Notice = %q", got)
		}
		if (Result{}).Notice() != "" {
			t.Error("no notice without error")
		}
	})
}

func TestRunnerResizeAndPadding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{out: func(ExecRequest) ([]byte, error) { return []byte("a\nb\n\n"), nil }}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, Padding: 2, Dir: "/w", Env: []string{"X=1"}})
		defer r.Close()
		r.Resize(80, 24) // before any payload: recorded, no run
		time.Sleep(time.Second)
		synctest.Wait()
		if fx.count() != 0 {
			t.Fatal("resize alone must not run")
		}
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		req := fx.last()
		if req.Command != "cmd" || req.Dir != "/w" || !slices.Equal(req.Env, []string{"X=1", "COLUMNS=80", "LINES=24"}) {
			t.Errorf("req = %+v", req)
		}
		res, _ := recv(r)
		if !slices.Equal(res.Lines, []string{"  a", "  b"}) {
			t.Errorf("Lines = %q", res.Lines)
		}

		r.Resize(80, 24) // unchanged: nothing
		time.Sleep(time.Second)
		synctest.Wait()
		if fx.count() != 1 {
			t.Error("same size must not rerun")
		}
		r.Resize(120, 30)
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		if fx.count() != 2 || !slices.Contains(fx.last().Env, "COLUMNS=120") {
			t.Errorf("resize rerun: %d %v", fx.count(), fx.last().Env)
		}
	})
}

func TestRunnerTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec})
		defer r.Close()
		r.Trigger() // no payload yet
		synctest.Wait()
		if fx.count() != 0 {
			t.Fatal("Trigger before Update must not run")
		}
		r.Update([]byte("p"))
		r.Trigger()
		synctest.Wait()
		if fx.count() != 1 {
			t.Fatal("Trigger runs immediately")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if fx.count() != 1 {
			t.Error("Trigger cancels the pending debounce")
		}
	})
}

func TestRunnerClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{release: make(chan struct{})}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, RefreshInterval: time.Second})
		r.Update([]byte("p"))
		time.Sleep(DefaultDebounce)
		synctest.Wait()
		r.Close()
		if fx.ended != 1 {
			t.Error("Close cancels the in-flight run")
		}
		if _, ok := <-r.Results(); ok {
			t.Error("Results is closed after Close")
		}
		r.Close() // idempotent
		r.Update([]byte("q"))
		r.Resize(1, 1)
		r.Trigger()
		synctest.Wait()
		if fx.count() != 1 {
			t.Error("no runs after Close")
		}
	})
}

func TestRunnerSet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fx := &fakeExec{}
		r := NewRunner(Config{Command: "cmd", Exec: fx.exec, RefreshInterval: time.Second})
		defer r.Close()
		r.Set([]byte("quiet"))
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		if fx.count() != 0 {
			t.Fatal("Set must not schedule a run")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if fx.count() != 1 || string(fx.last().Stdin) != "quiet" {
			t.Errorf("refresh should use the Set payload: %d", fx.count())
		}
	})
}

func TestRunnerConfigDefaults(t *testing.T) {
	r := NewRunner(Config{Command: "x"})
	defer r.Close()
	c := r.Config()
	if c.Debounce != DefaultDebounce || c.Timeout != DefaultTimeout || c.Exec == nil {
		t.Errorf("defaults = %+v", c)
	}
}
