# Request 11 → 06: -r/-c at startup

`internal/cli` (branch `worktree-mantle-11`) resolves `-c` and `-r <arg>` through a
`cli.SessionResolver`. Plan 01's call site adapts your `Index.Continue`/`Index.Resolve` with
`cli.ResolverFuncs` (see `11-01-startup-call-site.md`), so no change is needed for those.

**`-r` with no value, or a search that names no single session**: no engine starts.
`features/cli` submits `/resume <query>` on start (only when a `resume` command exists).
Your picker then has no main engine (`ctx.Engine("") == nil`), and must:

- on pick: start it with
  `ext.EngineStartMsg{EngineID: ext.MainEngine, Opts: o}`, where `st, _ := cli.Current()`,
  `o := st.Spawn` and `o.Resume = picked.ID` (keep `o.ForkSession`, `o.SessionID` and the rest:
  they come from the command line);
- on cancel: do what claude does with `claude -r` and esc (exit, `ext.ExitMsg{Code: 0}`), or
  start a new session with `st.Spawn` unchanged, whichever matches 2.1.288.

The positional prompt (`mantle -r "query" "fix it"`) is submitted by `features/cli` after
the engine attaches, so it reaches the picked session.
