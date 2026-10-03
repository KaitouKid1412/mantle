# Plan 03: Rendering & transcript

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-03 | `pkg/render`, `pkg/ui/diffview`, `features/transcript` | tags `contracts-v1`, `proto-v1` | none |

## Goal

Make the conversation look and feel like Claude Code: streaming markdown, syntax
highlighting, word-level diffs, a renderer for every tool and system event, the spinner, and
turn durations. Commit finished output to the terminal's own scrollback without ghost lines
or redraw cost.
- **Part A:** a pure render kit (text in, styled lines out).
- **Part B:** the transcript store, the commit policy, the renderers and the views.

## Start prompt

> You are session 03 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/03-render-transcript.md`, then execute that plan. Do Part A now. Start Part B
> once `git tag -l` shows the tags it needs. Commit with prefix `[03]`.

## Read first

- `docs/research/design-review.md` §1.3 (rendering design and Bubble Tea v2 inline facts)
  and §2 (libraries).
- `docs/research/inventory-docs.md` §3 (transcript rendering, views, chrome).
- `docs/research/inventory-binary-2.1.288.md` §6 (tool names) and §8 (UI features).
- `docs/research/protocol-2.1.288.md` §7 (stdout messages; `tool_use_result`).
- `docs/plans/01-contracts-shell.md` (the `Item`, `ContentKey`, `Renderer` and committer
  primitives you build on).

## Facts you can rely on

- **Libraries.**
  - `github.com/yuin/goldmark` v1.8.6 with GFM extensions (tables, strikethrough, task
    lists, autolinks); write a custom ANSI renderer.
  - `charm.land/glamour/v2` v2.0.1 is reference only: whole-document, wrong margins, no
    incremental API.
  - `github.com/alecthomas/chroma/v2` v2.27.0: `lexers.Match(filename)`, plus a custom
    formatter mapped to `pkg/theme` syntax tokens. Regex lexers are slow on huge inputs.
  - `github.com/charmbracelet/x/ansi` v0.11.8: Wrap, Truncate, StringWidth, Strip, OSC 8
    hyperlinks.
  - `github.com/aymanbagabas/go-udiff` v0.4.1 for unified hunks;
    `github.com/sergi/go-diff` v1.4.0 for word-level highlights.
- **Engine data.**
  - Edit and Write results carry `tool_use_result.structuredPatch` (hunks); prefer it over
    re-diffing.
  - `assistant` messages arrive **one per finished content block**; consecutive ones share
    `message.id`.
  - `stream_event` deltas (text_delta, thinking_delta, input_json_delta) come first; plan 02
    coalesces them (≤ 16 ms).
  - `supersedes: [uuid…]` on an assistant message means "remove these earlier messages".
  - `parent_tool_use_id` nests subagent output under its Agent/Task tool call.
  - `--forward-subagent-text` forwards subagent text and thinking; by default only their
    tool_use and tool_result arrive.
  - `system/thinking_tokens` gives `estimated_tokens`. `tool_progress` gives
    `elapsed_time_seconds` heartbeats. `tool_use_summary` gives `summary` plus
    `preceding_tool_use_ids`.
- **Inline commit facts (Bubble Tea v2.0.10).**
  - `tea.Println` writes immediately.
  - A print taller than H − liveHeight leaves ghost lines; a frame taller than H loses its
    top lines.
  - Plan 01 provides `Ctx.Print(blocks…)` (chunked) and `Ctx.Reprint()`. You decide *what*
    to commit and *when*.
- **Claude Code look to match** (write your own strings and colours):
  - `⏺` bullets for assistant text and tool calls; `⎿` for tool results.
  - Tool header: `Bash(git status)`.
  - Collapsed output: "… +N lines (ctrl+o to expand)".
  - Thinking: "✻ Thinking…" / "Thought for Ns", collapsed by default.
  - Turn duration line: "Cooked for 1m 6s".
  - Spinner: a verb (about 189 built-in; `spinnerVerbs` setting with mode append|replace),
    elapsed time, ↑/↓ token arrows, "esc to interrupt"; tips line (`spinnerTipsEnabled`,
    `spinnerTipsOverride`); honours `prefersReducedMotion`.

## Part A: start immediately (`pkg/render`, `pkg/ui/diffview`; depends only on `pkg/theme` token names; use a local token interface until `contracts-v1`)

- [ ] **A1 `pkg/render/wrap.go`.** ANSI-aware wrapping that preserves styles and OSC 8
  links across breaks, handles CJK/emoji widths (graphemes), hard-wraps long tokens, and
  supports a hanging indent.
- [ ] **A2 `pkg/render/markdown.go`.** goldmark → ANSI lines at a width.
  - Block elements: headings, paragraphs, bullet/ordered/task lists (nested), block quotes,
    GFM tables (fit-to-width with truncation), thematic breaks.
  - Inline elements: fenced code (via A4), inline code, emphasis, strong, strikethrough,
    links as OSC 8 with the visible URL when the text differs.
  - Width: honour `maxProseWidth` (prose capped; code and tables may use the full width).
- [ ] **A3 `pkg/render/stream.go`.** A streaming markdown renderer.
  - Split the source into top-level blocks.
  - **Cache rendered lines for closed blocks**; re-render only the open tail.
  - Expose `ClosedBlocks()`, used by progressive commit.
  - An unclosed code fence stays open until its fence closes.
- [ ] **A4 `pkg/render/highlight.go`.** chroma highlighting to theme tokens.
  - Language from the fence info string or the filename.
  - Cache by (content hash, lang, theme revision).
  - Skip highlighting above about 2,000 lines or 200 KB.
  - Honour `syntaxHighlightingDisabled`.
- [ ] **A5 `pkg/ui/diffview`.** Render hunks from `structuredPatch` (or old/new strings via
  go-udiff): line-number gutter, +/- colouring, word-level highlights within changed line
  pairs (go-diff), context folding, a dimmed variant for rejected edits, and a max-lines cap
  with a "+N lines" footer.
- [ ] **A6 Tests and stories.** Goldens at widths 40/60/100/160 for headings, lists,
  tables, code, links, CJK, emoji, and long URLs; diff goldens; streaming tests (feed
  character by character; closed-block cache hits; final output equals a one-shot render).
  Benchmark: render a 2k-token answer incrementally.

## Part B: after `contracts-v1` and `proto-v1`

- [ ] **B1 [M1] Transcript store (`features/transcript/store.go`).**
  - Builds `ext.Item`s from Engine*Msg, merging stream deltas into the open item.
  - Groups assistant blocks by `message.id`; attaches tool_results to their tool_use;
    nests by `parent_tool_use_id`.
  - Handles `supersedes` and `aborted`.
  - Bumps `Rev` on each change.
  - Implements `ext.Transcript`.
  - Accepts history items produced by plan 06's JSONL normalizer, which are already
    `Item`s.
- [ ] **B2 [M1] Commit policy (`features/transcript/commit.go`).**
  - Maintain a watermark.
  - Commit the longest prefix of items whose state is Done, Failed or Interrupted. Render
    at the current width and mode, and call `Ctx.Print`.
  - **Progressive commit:** for the streaming text item at the watermark, commit its closed
    markdown blocks as they finish; the rest stays live.
  - Never re-render committed lines.
  - On `supersedes` of already-committed content: print a dim "(response replaced)" marker
    plus the new content.
  - `Reprint()` on /clear, ctrl+l, rewind, session switch or theme change (optional),
    using the whole store.
- [ ] **B3 [M1] Live area (`SlotLive` component).** Running and streaming items, capped to
  the budget. Beyond it: "+N more running". The streaming tail shows its last K lines.
- [ ] **B4 [M1] Renderers (`features/transcript/render_*.go`), each registered with
  `AddRenderer`:**
  - `user.prompt`: prompt text with image/paste chips (`[Image #1]`,
    `[Pasted text #1 +40 lines]`), user background token; `user.bash` for `!` commands
    and their output.
  - `assistant.text`: streaming markdown (A3).
  - `assistant.thinking`: collapsed "Thought for Ns" by default; full gray italic in verbose
    view or when `showThinkingSummaries` is set; redacted thinking shows a placeholder.
  - **Core tools:**
    - `tool.Bash`: command header; stdout/stderr collapsed to about 5 lines with
      "+N lines"; exit code or error state; "Running…" with elapsed time from
      `tool_progress`; background shells flagged.
    - `tool.Read`: path and line range; "Read N lines" summary (contents hidden unless
      verbose). `tool.Write`: path; created/updated; diffview of new content (capped).
    - `tool.Edit` / `tool.MultiEdit`: path plus diffview from `structuredPatch`;
      "Added N lines, removed M lines"; dimmed when rejected.
    - `tool.Glob` / `tool.Grep`: pattern, path, match count; first matches when verbose.
    - `tool.WebFetch` (URL, status, size); `tool.WebSearch` (query, result count, links).
    - `tool.Agent` (alias `Task`): subagent type and description header, nested children
      (from `RenderCtx.Children`) with a compact tool-use count and token/duration summary
      (`task_progress`/`task_notification`); collapsed when done.
  - **Planning and interaction tools:**
    - `tool.TodoWrite`, `tool.TaskCreate` / `TaskUpdate` / `TaskList`: compact checklist
      (☐ / ◼ / ☒). The panel itself is plan 07.
    - `tool.ExitPlanMode`: the plan as markdown in a framed block.
    - `tool.AskUserQuestion`: question(s) and the chosen answers.
    - `tool.NotebookEdit`: cell id and a diff of the source.
    - `tool.Skill`: "Skill(name)".
    - `tool.TaskStop` / `TaskOutput` / `Monitor`: one-liners.
    - `tool.EnterPlanMode`, `tool.EnterWorktree`, `tool.ExitWorktree`, `tool.SendMessage`,
      `tool.ToolSearch`, `tool.WebFetch`-like MCP resource tools: one-liners.
  - **MCP and fallback:**
    - `tool.mcp.*`: `server - tool (MCP)` header with arguments summary and result text
      (collapsed). Group consecutive calls to the same server as "Called <server> N
      times".
    - Default renderer for unknown tools: name, JSON input summary, result text.
  - **Errors and interruptions:**
    - Tool `is_error`: red ⎿ with the error text.
    - "Interrupted by user" marker for aborted turns.
    - `permission_denials` from `result`: "Denied: Tool(args)".
    - Assistant `error` categories (rate_limit, overloaded, billing_error,
      authentication_failed, …) with friendly text.
  - **System events:**
    - `system.compact_boundary`: "Conversation compacted (pre → post tokens)".
    - `system.status` (compacting): spinner text.
    - `system.api_retry`: "Retrying in Ns (attempt a/b)".
    - `system.informational` (by level), `system.hook` (hook name, outcome, stderr on
      failure), `system.rate_limit`, `system.model_refusal_*`.
    - `system.local_command`: output of headless local commands (synthetic assistant text,
      `result.local_command`).
    - `system.notification`, `tool_use_summary`.
- [ ] **B5 [M1] Spinner and status (`SlotStatus`).**
  - Verb rotation from built-in verbs (write our own list) merged with `spinnerVerbs`;
    shimmer animation unless `prefersReducedMotion`.
  - Elapsed time; token count (↓ from `thinking_tokens` / usage deltas); "esc to interrupt".
  - Tips line (respect `spinnerTipsEnabled` and `spinnerTipsOverride{tips, tipsFile}`).
  - Driven by `session_state_changed` and `result`; stops on idle.
- [ ] **B6 [M1] Turn duration and timestamps.** On `result`, a duration line from
  `duration_ms` and the time (`showTurnDuration`, `timeFormat`, `timeZone`); message
  timestamps when `showMessageTimestamps` is on.
- [ ] **B7 [M2] View modes.** `verbose` setting / `--verbose` (show everything expanded);
  `viewMode` default|verbose|focus; `/focus` (prompt, summary and response only); brief
  mode (ctrl+shift+b, `app:toggleBrief`). Each mode is a `RenderCtx.Mode`; switching
  triggers `Reprint`.
- [ ] **B8 [M2] ctrl+o transcript viewer (`app:toggleTranscript`, `PlaceAltScreen`).**
  - Full store with timestamps and the model per message; collapsed items expandable;
    ctrl+e show-all.
  - Less-style keys (j/k, ctrl+u/d/b/f, g/G, space, b, arrows, home/end, `/` search, n/N).
  - q, esc or ctrl+c to exit. `v` opens the transcript in `$EDITOR`.
  - Context `Transcript`.

## Design notes

- **Renderers are pure:** `func(RenderCtx, *Item) Block`. The same renderer serves inline
  commit, the ctrl+o viewer and plan 12's fullscreen.
- **Line cache** key: (item ID, Rev, width, mode, theme revision, expanded).
- **Width.** Re-wrap on resize only for live and uncommitted items. Committed scrollback
  belongs to the terminal.
- **Collapsing.** `Block.Collapsible` plus "(ctrl+o to expand)". Expansion happens in the
  viewer, not in scrollback.
- **Performance budget.** Steady-state `View()` < 1 ms with a 10k-item store (only the live
  slot renders). Highlighting is cached. Delta handling allocates little.
- **Never emit raw engine text unescaped.** Strip or neutralise control sequences in tool
  output before printing (security and layout).

## Interfaces you provide / consume

- **Provide:**
  - `pkg/render` (wrap, markdown, stream, highlight) and `pkg/ui/diffview`, used by plans
    05 (diff preview in permission dialogs), 06 (history, /diff) and 12.
  - The transcript store as `ext.Transcript`; renderer IDs `render.<ContentKey>` that mods
    can Replace or Wrap.
- **Consume:**
  - `ext.Item`/`Renderer`/`Ctx.Print`/`Reprint` and slot APIs from plan 01.
  - Engine*Msg and proto types from plan 02.
  - History `Item`s from plan 06.
  - Theme tokens from 01.
- **Request to plan 01** if missing: per-slot height budget query; `Print` returning a
  completion message for watermark confirmation.

## Tests and done criteria

- Goldens from fixture replays (plan 02's `testdata/fixtures/02/*`) at 60/100/160 columns.
- vt test: after a full replay, scrollback text equals the expected transcript exactly,
  with no ghost lines; streaming a 2k-token answer with progressive commit leaves no
  artifacts.
- Benchmarks: a 10k-item replay; steady-state `View()` < 1 ms; incremental markdown faster
  than full re-render.
- Every tool in the 2.1.288 tool list has a renderer or falls to a tested default.

## Parity coverage

- `TR-*`: transcript rendering, tool renderers, system events, spinner, durations,
  timestamps.
- `VW-*`: views (verbose, focus, brief) and the ctrl+o viewer. Fullscreen is `VW-FS*`, owned
  by plan 12.

## Out of scope

- Todo panel and subagent panel: 07. Dialogs: 05. JSONL parsing: 06.
- Fullscreen virtual scroll and mouse: 12. Prompt editor rendering: 04.

## Parallel rules (reminder)

- No repo-wide rewriters (use `make fmt-03`).
- `pkg/ext` and `pkg/proto` are additive-only; request changes via `docs/plans/requests/`.
- Commit only your own files with prefix `[03]`; `make test-03`; no imports of other
  `features/*` packages.
