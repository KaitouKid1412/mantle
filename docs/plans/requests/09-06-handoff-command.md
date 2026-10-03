# 09 → 06: a hidden `handoff` command for the generic hand-off

**Status:** open (2026-10-03).

Plan 09 registers ~30 commands that open interactive Claude Code (`/remote-control`,
`/teleport`, `/upgrade`, `/feedback`, `/install-github-app`, …, see plan 09 B10). They
call plan 06's generic hand-off (B5) by ID, without importing `features/sessions`.

Proposal: register a hidden command

```go
ext.Command{Name: "handoff", ID: "cmd.handoff", Hidden: true, Source: ext.SourceBuiltin,
	Run: func(ctx ext.Ctx, args string) tea.Cmd { /* B5 procedure */ }}
```

`args` is the slash command line to run in Claude Code (`"/remote-control"`,
`"/feedback my report"`), or `""` to just open the session. Plan 09 looks it up with
`ctx.Command("handoff")` and calls `Run(ctx, "/<cmd> <args>")`.

The B5 procedure as planned: wait for idle and warn about background tasks, stop the
engine, `tea.ExecProcess(claude --resume <sid> [cmdline])`, restart with `--resume`
and reprint new history.

Until it exists, plan 09 shows a notice telling the user to run
`claude --resume <sid>` and type the command.

Spike note: plan 09 assumes `claude --resume <sid> "/<cmd>"` runs the slash command at
startup in interactive mode (the initial prompt goes through the same input path as
typed text). Please confirm when you build B5; if it doesn't, keep `args` for the
notice ("type /remote-control") and open the session without it.
