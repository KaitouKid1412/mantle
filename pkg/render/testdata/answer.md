I looked through the scheduler and found three separate problems. Here's what's going on and how I'd fix each one.

## 1. The retry loop never backs off

In `internal/queue/worker.go`, the retry path calls `w.enqueue(job)` immediately after a failure:

```go
func (w *Worker) handle(ctx context.Context, job *Job) {
	if err := job.Run(ctx); err != nil {
		job.Attempts++
		if job.Attempts < w.maxAttempts {
			w.enqueue(job) // retried right away, no delay
			return
		}
		w.deadLetter(job, err)
	}
}
```

Because the job goes straight back to the head of the queue, a job that fails deterministically burns through all of its attempts in a few milliseconds. The fix is to schedule the retry with exponential backoff and jitter:

```go
func backoff(attempt int) time.Duration {
	base := 200 * time.Millisecond
	d := base << min(attempt, 8) // cap the shift so it cannot overflow
	jitter := time.Duration(rand.Int64N(int64(d / 2)))
	return d/2 + jitter
}
```

Then `handle` becomes `w.enqueueAfter(job, backoff(job.Attempts))`.

## 2. Shutdown drops in-flight jobs

`Stop()` cancels the root context and returns immediately. Workers that are halfway through `job.Run` see the cancellation, return `context.Canceled`, and the job is counted as a failure. Two things need to change:

1. Give in-flight jobs a grace period. Cancel the *dequeue* context first, so no new work starts, and only cancel the *run* context after the grace period expires.
2. Treat `context.Canceled` during shutdown as "not attempted": put the job back without incrementing `Attempts`.

A sketch of the new shutdown sequence:

- stop accepting new jobs (`close(w.intake)`)
- wait up to `grace` for the `sync.WaitGroup` of running jobs
- if the wait times out, cancel the run context and wait again, briefly
- persist anything still queued so the next process picks it up

> Note: the grace period should be shorter than your orchestrator's termination timeout. Kubernetes sends `SIGKILL` 30 seconds after `SIGTERM` by default, so something like 20 seconds leaves room for the final flush.

## 3. The metrics double-count retries

Each retry increments `jobs_started_total` again, so dashboards show more jobs than were ever submitted. Here is how the counters behave today versus after the fix:

| Metric | Today | After the fix |
|:-------|:------|:--------------|
| `jobs_started_total` | +1 per attempt | +1 per job |
| `jobs_retried_total` | not recorded | +1 per retry |
| `jobs_failed_total` | +1 per failed attempt | +1 per job sent to the dead-letter queue |
| `job_duration_seconds` | includes queue wait | run time only |

## Testing

I'd add three tests:

- **Backoff bounds.** For attempts 0 through 20, `backoff` stays within `[d/2, d)` and never overflows.
- **Graceful stop.** Start a job that blocks on a channel, call `Stop()`, release the job inside the grace period, and assert it completes successfully.
- **Metric accounting.** Run a job that fails twice and then succeeds; assert `jobs_started_total == 1` and `jobs_retried_total == 2`.

You can run just these with:

```bash
go test ./internal/queue/... -run 'TestBackoff|TestGracefulStop|TestMetrics' -race -count=1
```

## Summary

- [x] Found the immediate-retry bug
- [x] Found the shutdown race
- [x] Found the metric double counting
- [ ] Implement the fixes (I can do this next if you'd like)

The first fix is the most urgent: under load, a single poison job can starve the queue because it keeps jumping back to the front. The other two are correctness issues that mostly affect observability and deploys. Let me know if you want me to go ahead with all three, or start with the backoff change on its own so it can ship separately.
