# 13 → 11: `--research` flag (MT-R1)

**Status:** done (e60e246). `--research` is `Consume`/`Mantle` in `internal/cli/flags.go`
and sets `MantleOpts.Research` → `Startup.Research`. `Startup.SetEnv` (called by
`cmd/mantle-ui/main.go` with `os.Setenv`) exports `ext.EnvResearch=1`, and `ext.EnvSafe`
as before. The flag is listed in the mantle options section of `mantle --help`. Tests:
`TestParse/research`, `TestStartupResearch` (not in `ExtraArgs`, env set, no env without
the flag), `TestFlagTableGolden`. Note: with `-p` or a subcommand, argv goes to `claude`
unchanged, so `--research -p` gets claude's unknown-option error (the same as `--safe`
without the launcher).
13c: read `os.Getenv(ext.EnvResearch) == "1"` to enter research mode on the first frame.

Add a mantle-only flag `--research`:
- Class `Consume` (not forwarded to `claude`), like `--continue` and `--resume` in
  `internal/cli/flags.go` and `parse.go`.
- `cmd/mantle-ui/main.go` sets `ext.EnvResearch=1` for the UI process.
- Research mode reads it and enters on the first frame.
- Add `--research` to the mantle-only section of the help output.

Tests: the parse test shows the flag is not forwarded and the env var is set.
