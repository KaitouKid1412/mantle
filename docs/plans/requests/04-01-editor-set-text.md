# Request 04 → 01: `ext.EditorSetTextMsg` (additive)

**From:** plan 04 (input), for plan 06 (rewind). **To:** plan 01 (pkg/ext steward).
**Status:** done. Shipped in contracts-v1.5; input.editor handles it.

## What

```go
// EditorSetTextMsg replaces the prompt editor's text: cursor at the end, not
// submitted, undoable. Sent by rewind (plan 06); handled by input.editor.
type EditorSetTextMsg struct{ Text string }
```

## Why

After a rewind, Claude Code puts the rewound prompt back in the prompt box. Plan 06's
rewind lives in `features/sessions` and cannot import `features/input`, so it needs a
message. Deep-link prefill could use the same message.

## Plan 04 side

`features/input` already has `state.setText` (used for `--prefill`). Wiring the message
is one `case` in `state.update` once the type is tagged.
