package research

import "testing"

func TestFormatQuote(t *testing.T) {
	tests := []struct{ sel, want string }{
		{"", ""},
		{"  \n\n", ""},
		{"one line", "> one line\n\n"},
		{"a\nb", "> a\n> b\n\n"},
		{"\na\n\nb  \n", "> a\n>\n> b\n\n"},
		{"a\r\nb", "> a\n> b\n\n"},
		{"> nested", "> > nested\n\n"},
	}
	for _, tt := range tests {
		if got := FormatQuote(tt.sel); got != tt.want {
			t.Errorf("FormatQuote(%q) = %q, want %q", tt.sel, got, tt.want)
		}
	}
}

func TestSplitQuote(t *testing.T) {
	tests := []struct{ prompt, quote, question string }{
		{"why?", "", "why?"},
		{"  why?  \n", "", "why?"},
		{"> a\n> b\n\nwhy?", "a\nb", "why?"},
		{"> a\n>\n> b\n\nwhy?", "a\n\nb", "why?"},
		{">a\nwhy?", "a", "why?"},
		{"\n> a\n\n\nwhy?\nreally?", "a", "why?\nreally?"},
		{"> only a quote", "only a quote", ""},
		{"why > because", "", "why > because"},
		{"> > nested\n\nq", "> nested", "q"},
	}
	for _, tt := range tests {
		q, s := SplitQuote(tt.prompt)
		if q != tt.quote || s != tt.question {
			t.Errorf("SplitQuote(%q) = %q, %q; want %q, %q", tt.prompt, q, s, tt.quote, tt.question)
		}
	}
	// Round trip.
	sel := "line one\n\nline three"
	if q, s := SplitQuote(FormatQuote(sel) + "the question"); q != sel || s != "the question" {
		t.Errorf("round trip: %q %q", q, s)
	}
}

func TestIsQuestion(t *testing.T) {
	for text, want := range map[string]bool{
		"why is the sky blue?":                true,
		"> quoted\n\nand a question":          true,
		"":                                    false,
		"  \n":                                false,
		"/compact":                            false,
		"<command-name>/model</command-name>": false,
		"<bash-input>ls</bash-input>":         false,
		"[Request interrupted by user]":       false,
		"<local-command-stdout>x</local-command-stdout>": false,
	} {
		if got := IsQuestion(text); got != want {
			t.Errorf("IsQuestion(%q) = %v, want %v", text, got, want)
		}
	}
}
