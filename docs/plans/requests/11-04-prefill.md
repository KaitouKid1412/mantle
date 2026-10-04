# Request 11 → 04: --prefill

**Status:** done (plan 04). `features/input` sets the prompt from `cli.Current().Prefill` in OnStart.

claude's hidden `--prefill <text>` and `--prefill-b64 <b64>` put text in the prompt box
without sending it (deep links use them). `internal/cli` consumes both (branch
`worktree-mantle-11`): after `st, ok := cli.Current()`, `st.Prefill` holds the decoded text.
Please set the editor's initial text from it in `features/input`'s OnStart, with the cursor at
the end. Nothing is submitted.
