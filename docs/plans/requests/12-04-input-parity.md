# 12 → 04: prompt, menus, history and esc (parity audit)

**Status:** §1 and §6 done (plan 04 c7703b3); §2, §3, §4, §5b, §7 taken by 04. Needs others: footer hidden while a menu/hint shows and the "History 2/2" rule title (01 adds EditorStateMsg.Panel and FrameTitle, 07 draws them); command availability via Command.Hidden in 08/09; close results with 08/09 (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Match layout and behaviour; keep mantle's
own wording.

## 1. Esc esc sent 30 ms apart reads as alt+esc (ED-*, SE-22)

`rewind`: the harness sends keys 30 ms apart. claude takes two ESC bytes that close
together as a double esc and opens rewind; mantle sees one alt+esc and does nothing. With
200 ms between the presses mantle opens rewind too (the scenario now does that). Treat
alt+esc (ESC ESC in one read) as a double esc.

## 2. `/` menu (AC-01, AC-02)

`slash-menu` / `menu`, `filtered`.

- claude draws the menu where the footer was (the footer is hidden while it is open);
  mantle draws it under the prompt and keeps the footer.
- claude's name column holds just the name (no argument hints), 30 columns wide, and a
  long description wraps to a second line; mantle shows argument hints and aliases in a
  ~52-column name column and truncates descriptions with "…".
- mantle lists commands claude doesn't show in this setup (API key, no claude.ai login):
  `/__remote-workflow` (hidden; must never show), `/advisor`, `/agent-view`, `/agents`,
  `/artifacts`, `/auto-mode-setup`, `/autofix-pr`, `/chrome`, `/cloud-plugins`, … claude's
  first page is `/add-dir`, `/autocompact`, `/background`, `/branch`, `/btw`, `/bug`,
  `/cd`, `/clear`, `/color`, `/compact`, `/config`, `/context`. Filter by the same
  availability rules (auth, gates, `isHidden`) as the engine's command list.
- Filtering `/model`: claude matches descriptions too (it lists `/model`, then
  `/code-review`, `/plugin-authoring`, `/update-config`, `/doctor`, `/loop`,
  `/claude-api`, `/effort`, `/fast`); mantle lists three name matches.

## 3. `@` mentions (AC-12)

`at-mention` / `suggestions`, `completed`: claude marks files with "+ " and shows the list
in the footer's place; mantle shows "main.go" without a marker under the prompt and keeps
the footer.

## 4. Large paste hint (ED-20, ED-06)

`large-paste` / `pasted`: same "[Pasted text #1 +29 lines]"; claude replaces the footer
with "paste again to expand"; mantle shows no hint.

## 5. History (HI-01, HI-06)

- `history` / `recalled`: claude titles the prompt's top rule "History 2/2" while a
  recalled entry is shown; mantle's rule is plain.
- `history` / `search` (inline): claude puts "search prompts: alp" at the start of the
  footer line, one line in all; mantle adds two lines ("search history (this project):
  alp" and its key hints) above the footer.

## 6. Prompt glyph and placeholder (ED-37)

Every scenario: claude's prompt glyph is "❯", mantle's ">". With an empty prompt claude
shows nothing after the glyph in these runs; mantle shows `Try "explain how this project
is organized"`.

## 7. Echo of panel commands (AC-22; with 08 and 09)

`help`, `model-picker`, `mcp` (`closed` checkpoints). claude echoes a panel command into
the transcript ("❯ /help") and, when the panel closes, one result line ("⎿  Help dialog
dismissed", "⎿  Kept model as Opus 5.5 (default)", "⎿  MCP dialog dismissed"). mantle
prints nothing for native panels (it does echo pass-through commands such as `/compact`).
