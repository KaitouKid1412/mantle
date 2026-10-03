# Plan 04: Input

| Session | Primary paths | Part B needs | Produces |
|---|---|---|---|
| mantle-04 | `pkg/ui/editor`, `features/input` | tags `contracts-v1`, `proto-v1` | none |

## Goal

The prompt editor and everything around it must feel identical to Claude Code:
- multiline editing, readline keys with kill ring and undo, vim mode;
- paste collapsing, image chips, `/` and `@` autocomplete, `!` bash mode;
- history and ctrl+r search, stash, external editor, emoji, ghost-text suggestions.

**Part A** builds a standalone editor widget plus history and paste I/O. **Part B** wires the
submit pipeline and the menus.

## Start prompt

> You are session 04 of mantle. Read `CLAUDE.md`, `docs/plans/00-overview.md` and
> `docs/plans/04-input.md`, then execute that plan. Do Part A now. Start Part B once
> `git tag -l` shows the tags it needs. Commit with prefix `[04]`.

## Read first

- `docs/research/inventory-docs.md` §1 (prompt input), §2 (turn control and queueing,
  for submit priorities), §9 (keybindings).
- `docs/research/inventory-binary-2.1.288.md` §3 (default bindings by context, vim mode),
  §7 (`history.jsonl`, `paste-cache`), §8 (prompt input features).
- `docs/research/protocol-2.1.288.md` §4.1 (user message fields), §5 (`file_suggestions`),
  §9 (slash commands headless).
- `docs/research/design-review.md` §2 (bubbles textarea limits; paste and keyboard facts).

## Facts you can rely on

- **Keyboard.**
  - Bubble Tea v2 enables bracketed paste by default (`PasteStartMsg`, `PasteMsg`,
    `PasteEndMsg`) and requests kitty key disambiguation, so shift+enter and ctrl+enter
    work in kitty, Ghostty, WezTerm and iTerm2 (CSI u). `KeyboardEnhancementsMsg` reports
    support.
  - Terminal.app has none of this; fall back to `\`+Enter, alt+enter (ESC CR) and ctrl+j.
- **bubbles v2.2.1 `textarea`** has no undo, kill ring, vim, atomic chips or ghost text.
  Write a custom editor. You may reuse bubbles' `key`, `help` and `textinput` ideas.
- **Claude Code defaults to match** (contexts and action IDs are verbatim; plan 01's table
  holds the defaults).
  - **Chat bindings:**
    - Submitting: enter `chat:submit`; ctrl+j `chat:newline`; ctrl+x enter
      `chat:queueSubmit`; ctrl+enter or ctrl+x ctrl+s `chat:sendNow`.
    - Cancelling and clearing: escape `chat:cancel`; ctrl+l `chat:clearInput`; cmd+k
      clearScreen.
    - History: up/down `history:previous`/`history:next`.
    - Editing: ctrl+_ / ctrl+- / ctrl+shift+- `chat:undo`; ctrl+g or ctrl+x ctrl+e
      `chat:externalEditor`; ctrl+s `chat:stash`; ctrl+v `chat:imagePaste` (alt+v on
      Windows).
    - Mode and model: shift+tab `chat:cycleMode` (handled by plan 05); meta+p
      `chat:modelPicker`, meta+o `chat:fastMode`, meta+t `chat:thinkingToggle` (plan 08);
      meta+w workflow keyword toggle.
  - **Global:** ctrl+r `history:search`.
  - **Autocomplete:** tab accept, esc dismiss, up/down.
  - **HistorySearch:** ctrl+r next, esc/tab accept, enter execute, ctrl+c cancel, ctrl+s
    cycle scope.
  - **Attachments:** left/right, backspace/delete remove, down/esc exit.
  - **Readline:** ctrl+a/e/k/u/w/y, alt+y (yank-pop), alt+b/f/d, ctrl+_ undo. Ctrl+w deletes
    back to whitespace.
- **Double press:** double esc on a non-empty prompt clears it (with undo available); double
  esc on an empty prompt opens rewind (plan 06).
- **`?` on an empty prompt** shows the shortcut help panel.
- **Paste collapse.** Pastes over **800 characters or 3 lines** become
  `[Pasted text #N +L lines]` chips. Full text goes to `~/.claude/paste-cache/<hash>.txt`,
  and the chip is restorable. Images become `[Image #N]` chips (ctrl+v from the clipboard,
  or dragged image file paths detected in a paste).
- **`~/.claude/history.jsonl`** has one JSON object per line:
  `{display, pastedContents, timestamp, project, sessionId}`.
  - History is per project directory; consecutive duplicates collapse.
  - Spike S3 (plan 02) decides whether headless claude writes it. If not, mantle appends
    with `O_APPEND`, one line per write.
- **Vim mode** (`editorMode: "normal"|"vim"`).
  - Modes: INSERT, NORMAL, VISUAL, VISUAL LINE, REPLACE.
  - An `-- INSERT --` indicator shows below the prompt; it is hidden when
    `statusLine.hideVimModeIndicator` is set.
  - `vimInsertModeRemaps` (for example `{"jj":"<Esc>"}`); motions, operators, text objects
    and `.` repeat. `/` in NORMAL mode opens history search.
- **User message fields** (stdin).
  - Content: text plus base64 image blocks.
  - `priority`: `now` (send-now, ends the current turn), `next` (folded in between tool
    rounds), `later` (queued as its own turn).
  - `origin:{kind:"human"}` (required for human-only behaviours such as the `ultracode`
    keyword).
  - `pasted_content` / `inline_pastes` mark pasted text.
  - `client_composed:true` disables `@path` and `/command` expansion (don't set it for
    normal prompts).
- **`file_suggestions` control request** `{query}` returns `{suggestions:[{path}], cwd}`.
  It honours `respectGitignore` and the `fileSuggestion` command. Debounce it.
- **Slash commands.**
  - The engine list comes from `initialize.commands` and init `slash_commands`; updates
    arrive as `system/commands_changed`.
  - Native commands are mantle's registry (`ext.Command`).
  - A native command with the same name wins.
  - Engine commands are sent as plain prompt text (`/name args`).
- **Ultrathink and ultracode.** `ultrathink` gets rainbow highlighting (visual only);
  `ultracode` triggers the Workflow tool (meta+w toggles).

## Part A: start immediately (`pkg/ui/editor`, pure; no `pkg/ext` imports until `contracts-v1`; afterwards wrap as an `ext.Focusable`)

- [ ] **A1 Buffer (`pkg/ui/editor/buffer.go`).** Grapheme-aware rune buffer (use x/ansi or
  uniseg for widths); logical lines; cursor plus a preferred column; soft wrap at width with
  a visual-line map; virtual and real cursor (expose a `tea.Cursor` position for IME).
- [ ] **A2 Editing operations and readline keys (`keys.go`).**
  - Insert, delete; word motions (alt+b/f, ctrl+left/right); line start/end (ctrl+a/e,
    home/end).
  - Kill-line ctrl+k, kill-to-start ctrl+u, kill-word-back ctrl+w (to whitespace), alt+d.
  - **Kill ring** with ctrl+y yank and alt+y yank-pop.
  - **Undo/redo** stack with coalescing of typing bursts (ctrl+_ / ctrl+-).
- [ ] **A3 Newlines.** shift+enter (when keyboard enhancements are reported), ctrl+j,
  `\`+Enter (replace the trailing backslash), alt+enter. Plain enter submits.
- [ ] **A4 Atomic chips (`chips.go`).** Paste and image chips are single cursor units:
  deleted whole and serialized back to their content on submit. Attachment navigation mode
  (left/right/backspace/delete/down/esc, context `Attachments`).
- [ ] **A5 Paste handling (`paste.go`).** Accumulate `PasteStart`…`PasteEnd`. Collapse above
  800 chars or 3 lines into `[Pasted text #N +L lines]`, storing the text in
  `~/.claude/paste-cache/<sha>.txt`. Detect pasted image file paths (png, jpg, gif, webp)
  and turn them into image chips. Strip invisible characters (zero-width, bidi, tag chars)
  and ask for a second Enter when any were found.
- [ ] **A6 Vim engine (`vim/`).** A pure state machine:
  - modes INSERT, NORMAL, VISUAL, VISUAL LINE, REPLACE;
  - motions h j k l w b e W B E 0 ^ $ gg G f/F/t/T ; ,;
  - operators d c y > < with counts; text objects iw aw i" a" i( a( i[ i{ ip;
  - `x` `X` `p` `P` `r` `u` ctrl+r, `.` repeat, `o` `O` `A` `I`;
  - insert remaps with a timeout (`vimInsertModeRemaps`).
- [ ] **A7 History store (`pkg/ui/editor/history` or `features/input/history` I/O helper
  kept pure).** Read `~/.claude/history.jsonl` filtered by project. Navigate with a
  draft-preserving cursor. Append atomically (one line per write, `O_APPEND`). Collapse
  consecutive duplicates. Includes `pastedContents`.
- [ ] **A8 Image clipboard (`clipboard_darwin.go`).** Read PNG from the clipboard via
  `osascript -e 'the clipboard as «class PNGf»'` into a temp file, convert to a base64 image
  block (cap the size; downscale if needed). Linux: `wl-paste` / `xclip` fallbacks. No cgo.
- [ ] **A9 Ghost text.** A rendering hook for dim suggestion text after the cursor; tab or
  right-arrow at end of line accepts.
- [ ] **A10 Tests.** Table-driven key-sequence tests (keys in, buffer/cursor out); vim test
  suite; 1 MB paste test (fast, collapsed); chip deletion and serialization; history
  round-trip against real-format fixtures (sanitized); goldens for the editor at widths
  40/80/120 with wrapping and chips.

## Part B: after `contracts-v1` and `proto-v1`

- [ ] **B1 [M1] Input feature (`features/input`).** Register the editor as the `SlotInput`
  component (context `Chat`), with the prompt frame from plan 07 and border colour by mode
  (`bashBorder` for `!`).
- [ ] **B2 [M1] Submit pipeline (`PromptStage`s).**
  1. Slash routing: native registry first, else an engine command (sent as text).
  2. `!` bash mode: run via the engine as a prompt with the command (respect
     `respondToBashCommands`; spike S8/S2 decides between `shouldQuery` injection and a
     direct prompt).
  3. Attachments → image blocks; paste chips → full text plus `pasted_content` metadata.
  4. Priority: `chat:submit` while idle sends normally; while busy it queues (`later`);
     `chat:sendNow` uses `now`; `chat:queueSubmit` uses `later`. The queue display is
     plan 05.
  5. Append to history.
- [ ] **B3 [M1] `/` menu (context `Autocomplete`).** Fuzzy filter (sahilm/fuzzy) over native
  plus engine commands, showing name, description, argument hint and alias highlighting.
  Hidden commands are excluded unless typed exactly. Mid-prompt `/` completion and ghost
  completion of the top match. Refresh on `commands_changed`.
- [ ] **B4 [M1] `@` mentions.** Debounced `file_suggestions` query; MCP resources
  (`@server:proto://res`) via `mcp_status`/resources listing (spike S11). Insert the path
  as text (the engine expands `@path`).
- [ ] **B5 [M1] History keys.** Up/down at first/last visual line navigates history (per
  project); a draft is restored when returning to the bottom.
- [ ] **B6 [M1] Image paste and paste chips wired** (`chat:imagePaste`) with a notice on
  failure.
- [ ] **B7 [M2] ctrl+r history search (context `HistorySearch`).** Inline search with match
  highlight; ctrl+r next; ctrl+s cycles scope (session → project → all); enter executes;
  esc/tab accepts into the editor.
- [ ] **B8 [M2] Other editor commands.**
  - ctrl+s stash: save and restore text, cursor, chips and mode in `~/.mantle/state`.
  - ctrl+g external editor (`charmbracelet/x/editor` + `tea.ExecProcess`; honour
    `externalEditorContext`).
  - ctrl+l clear input; double-esc clear with undo; `?` help panel (lists the active
    bindings from the keymap).
- [ ] **B9 [M2] Emoji and highlighting.** `:emoji:` shortcode completion popup and
  replacement (`goldmark-emoji` definitions; `emojiCompletionEnabled`); ultrathink rainbow
  highlight; ultracode keyword with meta+w toggle.
- [ ] **B10 [M2] Prompt suggestions.** Render `prompt_suggestion` as ghost text when
  `promptSuggestionEnabled` (initialize `promptSuggestions:true`, set by plan 02).
- [ ] **B11 [M2] Vim mode wired.** `editorMode`, the `/vim` toggle command (plan 08 owns the
  panel; you own the behaviour), mode indicator below the prompt (respect
  `hideVimModeIndicator`), `/` opens history search in NORMAL mode.
- [ ] **B12 [M3] Spellcheck.** Underline misspellings via aspell, hunspell or ispell
  (`spellcheck{enabled, checker, color}`).

## Design notes

- **The editor is a widget, not a feature.** `pkg/ui/editor` has no engine or settings
  knowledge; `features/input` injects config (keymap, vim mode, remaps, colours).
  Mods can Replace `input.editor` or Wrap stages.
- **Keep keys data-driven:** the editor exposes operations (`DeleteWordBackward`, …), and
  plan 01's keymap maps chords to `chat:*` actions, which call those operations. This keeps
  `keybindings.json` overrides working.
- **Multi-line navigation.** Up/down move within the buffer first, then hit history at the
  edges (Claude Code behaviour).
- **Performance.** Re-wrap only the changed logical line. A 1 MB paste never renders in
  full (it's a chip).
- **Security.** Strip control sequences from pasted text before rendering.

## Interfaces you provide / consume

- **Provide:**
  - `pkg/ui/editor` (reusable by other dialogs, for example deny feedback in plan 05 and
    `/btw` in plan 06).
  - `features/input` component ID `input.editor` and PromptStage IDs `input.slash`,
    `input.bash`, `input.attachments`, `input.priority`, `input.history`.
  - Command-completion data for plan 08's `/help`.
- **Consume:** `ext.Engine.Send` / `Control("file_suggestions")`, `SessionInfo.Commands`
  (plan 02), the keymap (plan 01), the prompt frame (plan 07), theme tokens.
- **Requests:**
  - To plan 05: queue display and take-back use your queued-prompt state. Expose it via a
    message (`QueuedPromptsMsg`).
  - To plan 06: double-esc on an empty prompt dispatches `mantle:rewind`.

## Tests and done criteria

- Key-sequence tables cover readline keys, kill ring and undo; vim tests pass.
- A 1 MB paste collapses in < 50 ms.
- `history.jsonl` compatibility: mantle appends lines that Claude Code's own picker reads
  (temp HOME test).
- `/` and `@` menus tested with enginefake responses.
- Goldens at 40/80/120 columns.
- With fakeapi: type a prompt with a pasted chip and an image, submit, and assert the stdin
  message content blocks and `pasted_content`.

## Parity coverage

- `ED-*`: prompt editor (multiline, readline, vim, stash, external editor, paste, images,
  emoji, spellcheck, invisible-char strip, ultrathink, suggestions, `?` help).
- `AC-*`: autocomplete (`/` menu, `@` mentions, `!` bash mode).
- `HI-*`: history (↑/↓, `history.jsonl`, ctrl+r scopes).

## Out of scope

- Queue display, take-back, esc interrupt and mode cycle: 05.
- Model/fast/thinking shortcuts' panels: 08.
- Rewind selector: 06. Prompt frame, footer and vim indicator placement: 07 (you supply the
  indicator text).

## Parallel rules (reminder)

- No repo-wide rewriters (use `make fmt-04`).
- `pkg/ext` and `pkg/proto` are additive-only; requests go in `docs/plans/requests/`.
- Commit only your own files with prefix `[04]`; `make test-04`; no imports of other
  `features/*` packages.
