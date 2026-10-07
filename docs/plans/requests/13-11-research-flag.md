# 13 → 11: `--research` flag (MT-R1)

**Status:** open (needs `contracts-v1.11`)

Add a mantle-only flag `--research`:
- Class `Consume` (not forwarded to `claude`), like `--continue` and `--resume` in
  `internal/cli/flags.go` and `parse.go`.
- `cmd/mantle-ui/main.go` sets `ext.EnvResearch=1` for the UI process.
- Research mode reads it and enters on the first frame.
- Add `--research` to the mantle-only section of the help output.

Tests: the parse test shows the flag is not forwarded and the env var is set.
