# Request 10 → 02 (cc 01, 11): fd hand-off instant restart (B11, M3)

**From:** plan 10 (launcher, /mantle) · **To:** plan 02 (engine), plan 01 (host),
plan 11 (CLI flag) · **Milestone:** M3 · **PARITY:** MT-32

## Goal

`/mantle restart` today exits 75: the launcher relaunches the new build with
`--resume <session>`, which restarts claude and loses background shells and
subagents. B11 keeps the engine alive: mantle-ui `exec`s the new build in place
(same pid), so the claude child, its pipes and its background work survive.

## Protocol

1. **Old mantle-ui** (plan 10, `features/selfmod`), when the user runs
   `/mantle restart` and the engine supports hand-off:
   - asks each engine to keep for its process state (new optional interface,
     below), and writes `$MANTLE_HOME/run/<pid>.handoff.json`:
     `{engines: [{id, pid, pgid, stdin_fd, stdout_fd, stderr_fd, session: SessionInfo, init: <raw initialize response>, capabilities: [...]}], argv: [...]}`;
   - clears `FD_CLOEXEC` on those fds (Go sets it on every fd it opens);
   - updates the run file's `version` to the new build id (the launcher accounts
     probation to it; done in plan 10's launcher, `Supervisor.handedOff`);
   - `syscall.Exec(newBinary, RestartArgs + ["--attach-engine-fds=<handoff.json>"], env)`
     with `MANTLE_BUILD_ID=<new id>` and `MANTLE_PROBATION=1`;
   - if `Exec` fails, falls back to exit 75.
2. **New mantle-ui**:
   - plan 11: accept the hidden flag `--attach-engine-fds=<path>` (consumed, never
     forwarded) and expose it in `cli.Startup`;
   - plan 01: with the flag, the host does not spawn the main engine; it asks the
     engine layer to adopt the engines listed in the file, then deletes the file;
   - plan 02: `Manager.Adopt(id string, spec AdoptSpec) (*Engine, error)` with
     `AdoptSpec{PID, PGID int; Stdin, Stdout, Stderr *os.File; Init json.RawMessage; Session ext.SessionInfo; Capabilities []string}`:
     re-creates the transport, correlator and coalescer on the inherited fds,
     **does not** send `initialize` again (restores the capability cache from
     `Init`), waits on the inherited child (`os.FindProcess(pid).Wait()` works:
     after exec it is still our child), writes the usual engine run record, and
     emits `EngineAttachMsg` and `SessionChangedMsg`;
   - plan 06: reloads the transcript from the session JSONL
     (`TranscriptHistoryMsg{Reset: true}`), as on resume.
3. **Engine state** to hand over (plan 02 decides the exact set): no pending
   control requests (hand-off only when idle, which plan 10 already checks), the
   session tracker's data, the capability cache, stderr ring contents (optional).

## Proposed pkg/ext addition (plan 01, additive)

```go
// HandoffEngine is an optional interface of ext.Engine: engines that can be
// handed to a new mantle-ui process across exec.
type HandoffEngine interface {
	// Handoff prepares the engine for exec: it stops its goroutines without
	// closing the process's pipes and returns what the new process needs.
	Handoff() (EngineHandoff, error)
}

type EngineHandoff struct {
	EngineID   string
	PID, PGID  int
	Files      [3]*os.File // stdin (write end), stdout, stderr (read ends)
	Init       json.RawMessage
	Session    SessionInfo
	Capabilities []string
}
```

## What plan 10 has done

- The launcher accounts a run to the build named by the run file's `version`
  (`internal/launcher.Supervisor.handedOff`, tested), so probation and last-good
  follow an in-place restart.
- `features/selfmod` will call `Handoff` and `Exec` once the interface exists; until
  then `/mantle restart` keeps using exit 75.
