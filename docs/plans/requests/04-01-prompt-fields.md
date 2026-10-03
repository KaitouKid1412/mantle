# Request 04 → 01: user-message fields on `ext.Prompt` (additive)

**From:** plan 04 (input). **To:** plan 01 (pkg/ext steward). **Status:** open.

## What

Add three fields to `ext.Prompt` (additive; zero values keep today's behaviour):

```go
type Prompt struct {
	Blocks   []proto.ContentBlock
	Priority string
	UUID     string
	Composed bool

	// Since contracts-v1.x.

	// ShouldQuery false adds the message to the conversation without starting a turn
	// (stdin "shouldQuery"). nil = engine default (true).
	ShouldQuery *bool
	// InlinePastes lists pasted texts that are still in Blocks where the user put
	// them, one entry per paste (stdin "inline_pastes").
	InlinePastes []string
	// PastedContent lists pastes the client took out of Blocks: each entry a string or
	// an array of content blocks (stdin "pasted_content").
	PastedContent json.RawMessage
}
```

Plan 02's engine copies them onto `proto.UserInput` (which already has `ShouldQuery`,
`InlinePastes` and `PastedContent`).

## Why

- **ED-23 (paste metadata).** mantle expands paste chips inline on submit, so the
  engine should get `inline_pastes` with each chip's text. The 2.1.288 schema describes
  `inline_pastes` as text the user pasted that is still in `message.content`, one entry
  per paste. The CLI may wrap those spans in its paste tags.
- **AC-17 (`!` bash mode).** The interactive `!cmd` path records the command and its
  output in the conversation and only starts a reply when `respondToBashCommands` is
  true. Headless has no equivalent: the stdin `bash_command` frame runs a one-shot shell
  but deliberately does not touch the transcript. mantle therefore runs the command and
  sends the result as a user message with `ShouldQuery = &respondToBashCommands`.

## Until it lands

`features/input` sends prompts without these fields: pastes arrive as plain text, and
`!` output always starts a turn.
