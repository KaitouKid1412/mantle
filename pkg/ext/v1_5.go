package ext

// Additions in contracts-v1.5.

// EditorSetTextMsg replaces the prompt editor's text, cursor at the end, without
// submitting it. Rewind sends it with the rewound prompt (as Claude Code puts the
// chosen prompt back in the input box); --prefill can use it too. The input feature
// handles it.
type EditorSetTextMsg struct {
	Text string
}
