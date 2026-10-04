# 03 → 11: how `--verbose` reaches the transcript

**Status:** resolved 2026-10-03: plan 11 adds `{"verbose": true}` to the flag scope (`cli.Startup.FlagSettings()`), so `Settings().Claude("verbose")` is true with `--verbose`.

Plan 11 says `--verbose` "sets mantle's verbose view and is also forwarded". The
transcript (plan 03) decides the view mode from the merged Claude Code settings:
`verbose` (bool) and `viewMode` (`default` | `verbose` | `focus`), read with
`ctx.Settings().Claude(...)`, plus the engine's `system/init.view_mode`.

Proposal (no new API): when `--verbose` is on the command line, expose it as a
flag-scope setting, so `Settings().Claude("verbose")` returns `true`, exactly as Claude
Code treats `--verbose` as overriding the `verbose` setting. If the flag must not go
through the settings layer, a `mantle:` setting or an `ext` message would also work;
tell plan 03 which one and it will read it.
