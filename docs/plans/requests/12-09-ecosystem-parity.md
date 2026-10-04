# 12 → 09: /mcp panel and the hand-off confirmation (parity audit)

**Status:** §1 taken by 09 (close line), echo with 04; §2 note, no change (2026-10-04). Found by the plan 12 side-by-side suite against claude
2.1.289 (inline renderer, fakeapi). Regenerate a frame with
`make parity-side-by-side PARITY_ARGS="-run <scenario>"` and read
`test/parity/out/<scenario>/<checkpoint>.txt`. Keep mantle's own wording.

## 1. /mcp panel (EC-01)

`mcp` / `shown` (one server whose command fails): claude draws the panel under the
"❯ /mcp" echo: "Manage MCP servers", "1 server", a group heading, "❯ ✘ broken", then a
debug hint and a docs link, and "↑/↓ to navigate · Enter to confirm · Esc to cancel".
mantle replaces the prompt with "MCP servers · 0 connected of 1", a "This session"
group, "❯ ✗ broken  failed · Connection closed" and a longer key line. Same information;
the differences are placement (claude keeps the transcript and echo above the panel) and
the close echo ("⎿  MCP dialog dismissed", see 12-04 §7).

## 2. Hand-off confirmation (SE-40, CL-04)

`handoff` / `panel`: `/mobile` works — mantle hands the terminal to Claude Code, which
resumes the conversation and shows the same QR panel. mantle first asks "Open in Claude
Code … enter continue · esc cancel"; the scenario presses enter there with an
`@mantle` step (listed in the report as a flow difference). The extra step is
reasonable (the terminal changes hands), so this is a note, not a request; consider
skipping it for commands that only show something (QR codes, links).
