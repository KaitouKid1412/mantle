# mantle parity audit (plan 12, B6)

Audit of mantle against the Claude Code terminal UI, run on 2026-10-04 and 2026-10-05.
The feature inventory is `docs/PARITY.md` (Claude Code 2.1.288); the reference engine on
this machine had auto-updated to **2.1.289**, which the side-by-side suite ran against.
Per-row evidence is generated: `make parity` writes `docs/parity-status.md`.

## How it was audited

- **Side-by-side suite** (`test/parity`, `make parity-side-by-side`): 33 scenarios run
  against interactive `claude` and `mantle-ui` in a pty with an x/vt emulator, both on the
  scripted fakeapi with an isolated `CLAUDE_CONFIG_DIR` (offline, free). Checkpoints
  capture the screen and scrollback; a normalizer removes volatile content (logo art,
  spinners, durations, ids, paths, costs, ports); the report diffs every checkpoint and
  marks intentional differences from `test/parity/allowlist.txt`.
  - 27 inline scenarios cover every PARITY area B1 lists (answers, tool approval allow /
    always / deny / feedback, Edit diffs, Bash collapse, plan mode, AskUserQuestion,
    tasks, subagents, interrupt, queue, paste, `@`, `/`, history, `/model`, `/context`,
    `/help`, `/mcp`, `/compact`, `/clear`, resume `-c` and `/resume`, rewind, statusLine,
    mode cycle, an H hand-off).
  - 6 fullscreen scenarios pin `tui: fullscreen` (answer, permission, PgUp/ctrl+end
    scrolling, ctrl+r, `/` menu); `default-renderer` leaves `tui` unset so each program
    picks its own default (both start fullscreen with 2.1.289, see below).
  - Every scenario is deterministic on both targets (two runs, identical normalized
    frames: `MANTLE_PARITY_CLAUDE=1 MANTLE_PARITY_MANTLE=1 go test ./test/parity -run
    Deterministic`).
- **Performance suite** (`test/e2e/perf`, `make perf`): mantle against claude on fakeapi.
- **Real end-to-end** (`test/e2e/real`, `MANTLE_E2E_REAL=1 make e2e-real`): mantle with the
  real engine and API (isolated config, Haiku); opt-in, costs a few cents.
- **Terminal matrix and accessibility** (B5): a manual checklist in
  `docs/plans/12-fullscreen-audit.md`, run by the user.

## Findings that changed the audit itself

- **The emulator misread claude's window title.** x/vt treats bytes 0x80–0x9F inside OSC
  strings as C1 controls, so the UTF-8 title "✳ Claude Code" (0x9C = ST) ended early and
  printed onto the screen; claude skips blank cells with cursor jumps, so the stray
  letters stayed. Frames captured before 9ceeeb6 are unreliable. `test/parity/vtfix.go`
  sanitizes string payloads before the emulator.
- **Claude Code 2.1.289 starts in the fullscreen renderer** with a fresh config (alternate
  screen, mouse tracking); `tui: default` restores inline. The user decided mantle follows
  the installed Claude Code's default when `tui` is unset (plan 01, plan 11's version
  table). The inline suite pins `tui: default` on both targets.
- **Claude's pending-tool bullet blinks** in fullscreen; checkpoints sample twice and merge
  blinking cells so frames don't depend on the animation phase.
- Esc twice within ~30 ms reached mantle as alt+esc (fixed in plan 04).

## Results

Side-by-side on integration-6: all 66 runs complete on both targets. Differences were
filed as requests to the owning plans in two rounds.

| Request | Owner | Status |
|---|---|---|
| [12-03](plans/requests/12-03-transcript-parity.md) transcript: turn line, collapse, task rows, gutter, tabs, agents, compact | 03 | done |
| [12-04](plans/requests/12-04-input-parity.md) input: esc esc, menus, `@`, paste, history, glyph, echo | 04 | §1, §6 done; rest taken |
| [12-05](plans/requests/12-05-permission-and-turn-parity.md) permissions, plan approval, auto mode, AskUserQuestion | 05 | done |
| [12-06](plans/requests/12-06-sessions-parity.md) /clear phantom id, resume, picker, rewind, /context | 06 | done (compact screen with 03) |
| [12-07](plans/requests/12-07-chrome-parity.md) banner, footer, status line, task panel | 07 | done |
| [12-08](plans/requests/12-08-panels-parity.md) /model, /help, closing lines | 08 | done |
| [12-09](plans/requests/12-09-ecosystem-parity.md) /mcp closing line, hand-off | 09 | done |
| [12-01 fullscreen print](plans/requests/12-01-fullscreen-print.md) printed output in fullscreen | 01, 12 | done (contracts-v1.10, viewport) |
| [12-03 round 2](plans/requests/12-03-transcript-parity-2.md) line breaks, Waiting rows, notices, fullscreen rows | 03 | done except §4 (resumed turn lines, with 06) |
| [12-04 round 2](plans/requests/12-04-input-parity-2.md) history numbering, VW-22 dialog, esc, /clear echo, menu entries | 04 | done except §5 (menu entries) |
| [12-06 round 2](plans/requests/12-06-sessions-parity-2.md) hand-off screen, resumed turn lines | 06 | §1 done; §2 open (with 03) |

Also fixed along the way: in fullscreen, `Reprint` never delivered `ScreenClearedMsg`, so a
session picked in `/resume` never showed (plan 01, 1baa21a); the startup banner prints
before any input can be echoed above it (plan 07, fd729c0).

Plan 12's own fullscreen work after the comparison: the sticky prompt header shows only
while scrolled up, and a jump-to-bottom hint sits on the last visible line whenever the
view isn't following (VW-19, VW-20). VW-22 (fullscreen ctrl+r dialog) moved to plan 04,
which owns history.

### Intentional differences

`test/parity/allowlist.txt` lists them with reasons. In short: mantle's own banner and
art (CH-20, licensing), mantle's own wording for modes, hints, closing lines and notices
(it never copies Claude Code's strings), mantle-only notices (engine drift, the engine
version check), and the agent-view footer hint (CL-10 is a hand-off).

### Row status

From `make parity` (generated `docs/parity-status.md`), 572 rows:

| State | Rows | Meaning |
|---|---:|---|
| compared | 58 | tagged registration, exercised side by side against Claude Code |
| tagged | 340 | a registration names the row (owner's claim, unit and vt tests) |
| evidenced | 1 | no registration can name it; `docs/parity-evidence/NN.md` points at its tests |
| scenario only | 2 | a scenario covers it, no registration tag |
| untagged | 122 | no tag or evidence yet: engine plumbing (plan 02, 59 rows), launcher and self-mod internals (10), drift rows (11), and rows to confirm with their owners (04, 05, 06, 09); each owner is adding tags or evidence (`docs/parity-evidence/NN.md`) |
| hand-off | 36 | H: opens real Claude Code (the generic path is verified by the `handoff` scenario, `/mobile`) |
| gap | 13 | X, below |

Known gaps (X), as PARITY.md records them:

| Row | Feature | Reason |
|---|---|---|
| GAP-01 | Voice dictation | claude.ai speech-to-text isn't exposed to headless clients |
| GAP-02 | Claude Code's TypeScript mods drawing UI | internal protocol; mantle's `/mantle` mods replace it |
| GAP-03 | Agent-team split panes | teammates aren't spawned in `-p` |
| GAP-04 | Computer use | interactive sessions only |
| GAP-05 | Session-quality survey | deliberately omitted |
| GAP-06 | Hook `terminalSequence` | ignored in `-p`; partly recovered by CH-27 |
| GAP-07 | Fable usage-credits consent | not forwarded to headless clients (unverified) |
| GAP-08 | Model-choice dialog after a refusal | partly shown via TR-37 |
| GAP-09 | Gated features | behind flags; drift detection notices them |
| GAP-10 | Claude Code's `/resume` hides mantle sessions | `sdk-cli` entrypoint; mitigated by SE-13 |
| GAP-11 | Deep links | registered by interactive Claude Code only |
| GAP-12 | Push notifications to mobile | needs claude.ai remote |
| GAP-13 | Internal commands | not user-facing |

Not applicable, and caveats (from the owners' evidence):

- **CL-13 `/stop`** — not applicable: Claude Code enables it only inside a background
  session (`claude --bg`), which mantle's engine never is, so the engine never lists it;
  background sessions are stopped from mantle's agent view (`claude stop <id>`). Recorded as
  "Not applicable" in `docs/parity-evidence/09.md`, which `make parity` reports as its own
  state.
- **EC-26, EC-27** — `commit-push-pr`, `commit` and `pr` are gated bundled skills that the
  test account's engine doesn't offer (not in `initialize` commands, 2.1.288/2.1.289); when
  an engine offers them they take the same passthrough path as the other skills.

### Performance

`make perf` (test/e2e/perf), 2026-10-05, on fakeapi, claude 2.1.289 vs mantle on
integration-6. mantle's "with engine" figures include its claude engine child (mantle
runs the same binary headless); "own" is the mantle-ui process alone.

| Measure | claude | mantle |
|---|---:|---:|
| First prompt drawn (median of 5) | 168 ms | 44 ms |
| Engine ready (mantle: startup banner at initialize; median / max) | 168 ms | 356 ms / 965 ms |
| Idle CPU at the prompt with a status line (10 s) | 2.5% | 2.0% |
| Streaming a 250-line answer, prompt to end | 1.07 s | 1.17 s |
| … lines missing or repeated in scrollback | 0 | 0 |
| 2 MB of Bash output + a 200k-character line, prompt to done | 1.13 s | 1.19 s |
| Resume a 10,000-item session, start to last answer | 1.45 s | 1.48 s |
| RSS, own process (startup / 10k-item resume) | 212 MB / 289 MB | 33 MB / 80 MB |
| Peak RSS with engine (startup / 10k-item resume) | 215 MB / 321 MB | 197 MB / 235 MB |
| CJK, emoji and table cells (80 columns) | reference | identical |

mantle draws its prompt before its engine answers `initialize` and holds input until
then, so "first prompt" and "engine ready" differ for mantle only. The steady `View()`
cost of the fullscreen viewport over 10k items stays under 1 ms
(`features/fullscreen` `TestViewSteadyUnderBudget`, ~30 µs). No regression needed a
request.

### Terminal matrix and accessibility

Pending: the user runs the B5 checklist (`docs/plans/12-fullscreen-audit.md`); results are
recorded there.

## Open items

- Round 2: resumed turn lines (03 §4 / 06 §2) and the menu entries claude hides
  (04 §5).
- Untagged rows: each owner adds a `Parity:` tag or evidence in
  `docs/parity-evidence/NN.md` (list in `docs/parity-status.md`, state "untagged").
- B5 results.
- Re-run `make parity-side-by-side` on each integration tag; `make parity` regenerates
  the status.
