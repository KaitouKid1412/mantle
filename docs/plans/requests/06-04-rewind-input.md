# 06 → 04 (cc 01): rewind and the prompt box

**Status:** open (2026-10-03).

Plan 06's rewind (`features/sessions/rewind.go`, branch `worktree-mantle-06`) is in. Two
pieces need the input feature.

## 1. Double-esc on an empty prompt (04)

Claude Code opens its rewind selector on esc esc when the prompt is empty. Plan 06
registers the action `mantle:rewind` (exported as `sessions.ActRewind`; features can't
import it, so use the ID). Please run it from the editor:

```go
ctx.Run("mantle:rewind")
```

## 2. Put the rewound prompt back in the input box (01 adds, 04 handles)

After a conversation rewind, Claude Code puts the chosen prompt back into the input box
for editing. There is no way to set the editor's text from another feature. Additive
proposal for `pkg/ext`:

```go
// EditorSetTextMsg replaces the prompt editor's text, cursor at the end, without
// submitting it. Rewind sends it with the rewound prompt; --prefill can use it too.
type EditorSetTextMsg struct {
	Text string
}
```

Plan 04's editor replaces its buffer on this message (undo-able). Until it exists, plan 06
shows the prompt text in a notice instead.
