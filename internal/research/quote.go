package research

import "strings"

// FormatQuote quotes a selection for the prompt: every line gets a "> " prefix (an empty
// line gets ">"), and a blank line follows so the question starts below it. An empty
// selection yields "".
func FormatQuote(sel string) string {
	sel = strings.Trim(strings.ReplaceAll(sel, "\r\n", "\n"), "\n")
	if strings.TrimSpace(sel) == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(sel, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			b.WriteString(">\n")
			continue
		}
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.String()
}

// IsQuestion reports whether text, a prompt as the user sent it, starts a node: not a
// slash command, not engine-generated text (command tags, bash mode, interruption
// markers) and not empty. It is the live counterpart of what Build treats as a prompt.
func IsQuestion(text string) bool {
	t := strings.TrimSpace(text)
	return t != "" && !strings.HasPrefix(t, "/") && !syntheticRE.MatchString(t)
}

// SplitQuote splits a prompt into its leading quote block (markers removed) and the
// question after it. A prompt without a leading quote returns "" and the prompt
// trimmed.
func SplitQuote(prompt string) (quote, question string) {
	lines := strings.Split(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	var q []string
	for ; i < len(lines); i++ {
		l := strings.TrimLeft(lines[i], " ")
		if !strings.HasPrefix(l, ">") {
			break
		}
		l = strings.TrimPrefix(l, ">")
		l = strings.TrimPrefix(l, " ")
		q = append(q, l)
	}
	quote = strings.Trim(strings.Join(q, "\n"), "\n")
	question = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	return quote, question
}
