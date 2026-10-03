package termsetup

import (
	"fmt"
	"strings"
)

const vscodeSendSequence = "workbench.action.terminal.sendSequence"

// vscodeBinding is the keybindings.json element mantle adds. The JSON text spells the
// newline sequence with escapes, so the file stays plain ASCII.
func vscodeBinding(ind string) string {
	unit := ind
	if unit == "" {
		unit = "    "
	}
	body := strings.Join([]string{
		"{",
		unit + `"key": "shift+enter",`,
		unit + `"command": "` + vscodeSendSequence + `",`,
		unit + `"args": {`,
		unit + unit + `"text": "\u001b\r"`,
		unit + `},`,
		unit + `"when": "terminalFocus"`,
		"}",
	}, "\n")
	return indentLines(body, ind)
}

type vscodeEntry struct {
	Key     string `json:"key"`
	Command string `json:"command"`
	When    string `json:"when"`
	Args    any    `json:"args"`
}

func planVSCode(in Input) Proposal {
	if in.KittyKeyboard {
		return nativeProposal(in.Terminal)
	}
	app := in.Terminal.Name()
	summary := "Add a Shift+Enter binding to " + app + "'s integrated terminal that sends Esc+Return, which mantle reads as a newline."
	notes := []string{app + " picks up keybindings.json changes immediately."}
	manual := func(summary string, extra ...string) Proposal {
		return Proposal{Status: Manual, Kind: TargetFile, Summary: summary, Notes: extra}
	}

	content := in.Content
	if !in.Exists {
		content = nil
	}
	if isBlank(stripComments(content)) {
		// Missing or empty (or only comments): start a new array after whatever is there.
		var b strings.Builder
		b.Write(content)
		if len(content) > 0 && content[len(content)-1] != '\n' {
			b.WriteByte('\n')
		}
		b.WriteString("[\n" + vscodeBinding("    ") + "\n]\n")
		p := editProposal(TargetFile, in.Path, content, []byte(b.String()), summary)
		p.Notes = notes
		return p
	}

	var entries []vscodeEntry
	if err := parseJSONC(content, &entries); err != nil {
		return manual("keybindings.json couldn't be read as a list of bindings ("+err.Error()+"), so mantle left it alone.",
			"Fix the file in "+app+", then run /terminal-setup again.")
	}
	for _, e := range entries {
		if normalizeVSKey(e.Key) != "shift+enter" || strings.HasPrefix(e.Command, "-") || !terminalWhen(e.When) {
			continue
		}
		if e.Command == vscodeSendSequence {
			if text, ok := sendSequenceText(e.Args); ok && (text == NewlineSeq || text == "\n") {
				return Proposal{Status: AlreadyInstalled, Kind: TargetFile,
					Summary: app + " already sends a newline for Shift+Enter in the terminal."}
			}
			text, _ := sendSequenceText(e.Args)
			return manual(fmt.Sprintf("Shift+Enter in %s's terminal already sends %q, so mantle left it alone.", app, text),
				"Change or remove that binding in keybindings.json, then run /terminal-setup again.")
		}
		return manual("Shift+Enter in "+app+"'s terminal is already bound to "+e.Command+", so mantle left it alone.",
			"Change or remove that binding in keybindings.json, then run /terminal-setup again.")
	}

	out, err := appendToArray(content, vscodeBinding)
	if err != nil {
		return manual("keybindings.json has an unexpected layout (" + err.Error() + "), so mantle left it alone.")
	}
	p := editProposal(TargetFile, in.Path, content, out, summary)
	p.Notes = notes
	return p
}

func normalizeVSKey(k string) string {
	return strings.ToLower(strings.ReplaceAll(k, " ", ""))
}

// terminalWhen reports whether a binding's "when" clause can apply in the terminal.
func terminalWhen(when string) bool {
	w := strings.TrimSpace(when)
	return w == "" || strings.Contains(w, "terminalFocus") || strings.Contains(w, "terminal")
}

func sendSequenceText(args any) (string, bool) {
	m, ok := args.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["text"].(string)
	return s, ok
}
