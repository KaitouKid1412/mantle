# 07 → 01: terminal View fields, editor state, accessibility flag

**Status:** resolved 2026-10-03 in tag `contracts-v1.1`: `ext.TerminalStater` /
`ext.TerminalState`, `ext.EditorStateMsg` (Vim also allows "REPLACE"),
`Ctx.Accessibility()`. Focus/blur/resume messages are broadcast. The host returns
`tea.Suspend` on ctrl+z when no action is bound to it, so chrome adds no suspend action.
Plan 04 is asked to send `EditorStateMsg`.

## 1. Terminal-level View fields (B5 title/progress, B9 focus)

Chrome owns the window title, the OSC 9;4 progress bar and focus reporting; the host
owns the root `tea.View`. Proposal: an optional Component interface, polled when the
host builds the root View.

```go
// TerminalState is terminal-level output a feature contributes to the root tea.View.
type TerminalState struct {
	WindowTitle string           // "" = no opinion
	Progress    *tea.ProgressBar // nil = no opinion; State ProgressBarNone clears it
	ReportFocus bool             // request tea.FocusMsg / tea.BlurMsg
}

// TerminalStater is an optional Component interface. The host asks every component
// that implements it (inline slot order, then Weight) and uses the first non-empty
// WindowTitle and the first non-nil Progress; ReportFocus is OR-ed.
type TerminalStater interface {
	TerminalState(Ctx) TerminalState
}
```

Chrome registers a zero-height component (`chrome.terminal`, slot `status`, `View`
returns "") that implements it. Please also broadcast `tea.FocusMsg`, `tea.BlurMsg` and
`tea.ResumeMsg` to components/subscribers.

## 2. Editor state message (B1 frame colour, B2 vim indicator)

Plan 04's editor knows the input mode (`!` bash mode) and the vim mode; chrome draws
the frame and the footer indicator. Proposal, sent by `input.editor` whenever either
changes (and once at start):

```go
// EditorStateMsg describes the prompt editor's mode.
type EditorStateMsg struct {
	Mode  string // "prompt" | "bash"
	Vim   string // "" when vim mode is off, else "INSERT" | "NORMAL" | "VISUAL" | "VISUAL LINE"
	Empty bool   // the buffer is empty (footer shows the shortcuts hint only then)
}
```

## 3. Accessibility flags (B13 screen-reader flat mode)

`--ax-screen-reader` is a CLI flag only the host sees. Proposal (Ctx may gain methods):

```go
// Accessibility is resolved by the host from --ax-screen-reader,
// CLAUDE_AX_SCREEN_READER, axScreenReader and prefersReducedMotion.
type Accessibility struct{ ScreenReader, ReducedMotion bool }

// on Ctx:
Accessibility() Accessibility
```

Until it exists, chrome reads the env var and the settings key itself.

## 4. Question: ctrl+z

`ext.WarnKeys` lists ctrl+z. Does the host return `tea.Suspend` on ctrl+z itself? If
not, chrome will register `mantle:suspend` bound to ctrl+z in `Global` (B12) and redraw
on `tea.ResumeMsg`.

## How chrome uses `pkg/ext` otherwise (no change needed)

- Prompt frame: `Wrap("input.editor", func(next ext.Component) ext.Component)`; the
  wrapper delegates Focusable, ContextStack, ActionHandler and FocusAware and shifts the
  cursor by the frame's top row.
- Clipboard for other plans: `internal/term/clipcmd.Copy(text) tea.Cmd` (OSC 52 via
  `tea.Raw` when needed) returning `clipcmd.CopiedMsg`; nobody imports `features/chrome`.
