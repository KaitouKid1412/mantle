# mantle

mantle is a terminal UI for Claude Code written in Go with Bubble Tea v2. It drives the
real `claude` binary headlessly (stream-json over stdio), so the engine stays exactly
Claude Code's; only the UI is new. Two goals: full parity with the Claude Code 2.1.288
terminal UI, and `/mantle <request>`, which rebuilds mantle itself with new or changed
features. "mantle" is a working name.

## Start here

1. `docs/plans/00-overview.md`: decisions, architecture, session layout, rules.
2. Your plan: `docs/plans/NN-*.md`. Several sessions run in parallel in this checkout,
   one per plan.
3. `docs/PARITY.md`: every Claude Code feature, with its owner plan.
4. `docs/research/`: protocol spec, feature inventories and design review. These are
   internal notes; never copy strings, themes or schemas from them into shipped code.

## Build and test

```bash
make build        # bin/mantle, bin/mantle-ui, bin/fakeclaude, bin/fakeapi
make test-NN      # tests for plan NN's packages only
make fmt-NN       # gofmt plan NN's packages only
make lint         # go vet + gofmt check
make archtest     # import rules
make tags         # contract tags that unblock Part B
go build -tags no_input ./cmd/mantle-ui   # build while an area is broken
```

Go 1.27 (pinned toolchain), module `github.com/KaitouKid1412/mantle`.

## Contract tags

Each plan has a Part A (no dependencies) and a Part B (integration). Part B waits for:
- `contracts-v1`: tagged by plan 01 when `pkg/ext`, `pkg/theme` and the `Item` model land.
- `proto-v1`: tagged by plan 02 when `pkg/proto` lands.

Check with `git tag -l`. After a tag exists, that package is **additive-only**.

## Rules for parallel sessions

1. Read `00-overview.md` and your plan before starting.
2. Work mainly in your plan's primary paths. Small edits elsewhere are fine; file-baton
   serializes same-file edits. For larger changes in another plan's area, write
   `docs/plans/requests/<from>-<to>-<slug>.md`.
3. `pkg/ext` and `pkg/proto` are additive-only after their tags: no renames, removals or
   signature changes. file-baton cannot catch semantic breaks across files.
4. file-baton cannot see shell edits:
   - Never run repo-wide rewriters (`go mod tidy`, `gofmt -w .`, `go generate ./...`,
     cross-directory `sed -i`). Use `make fmt-NN`.
   - New dependency: `go get <mod>@<ver>`, then commit `go.mod` and `go.sum` right away
     on their own.
5. No cross-feature imports: `features/a` never imports `features/b`, and `mods/` imports
   only `pkg/`. Talk through `pkg/ext` IDs and messages.
6. Your packages compile at the end of every turn.
7. Run scoped tests (`make test-NN`). Run golden `-update` only in your own packages.
8. Commit only your own files, named explicitly (never `git add -A`), with message prefix
   `[NN]`.
9. Parity status: tick your plan's checklist and tag registrations with PARITY IDs.
   PARITY.md is not edited per feature.
10. Fixtures go in `testdata/fixtures/<NN>/`, sanitized (no home paths, tokens or emails).

## Conventions

- Keybinding contexts and action IDs are Claude Code's verbatim (`chat:submit`).
  mantle-only actions use `mantle:*`.
- mantle settings live in `~/.mantle/`. Read Claude Code settings from every scope.
  Never write `~/.claude.json`; prefer control requests over editing settings files.
- The engine's stderr must never reach the terminal. Pipe it to a ring buffer.
- No real API calls in tests: use `fakeclaude` (scripted engine) or `fakeapi` (the real
  engine with an isolated `CLAUDE_CONFIG_DIR`). Real `claude` runs are opt-in spikes.
- UI tests run in the x/vt emulator and assert both the screen and the scrollback.
