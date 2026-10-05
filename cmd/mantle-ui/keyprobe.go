package main

import (
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/keymap"
)

// keyprobe shows how this terminal's keys reach mantle: the decoded keystroke, the
// canonical keybinding form, and whether the terminal reports keyboard enhancements
// (kitty protocol / modifyOtherKeys). Spike S15's terminal matrix (shift+enter and
// ctrl+enter in iTerm2, Ghostty, Terminal.app, kitty, WezTerm, VS Code, tmux) is
// filled in by running it in each terminal. ctrl+c twice quits.
type keyprobe struct {
	lines    []string
	enh      string
	lastCtrl bool
}

func (k *keyprobe) Init() tea.Cmd { return nil }

func (k *keyprobe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		k.enh = fmt.Sprintf("%+v", m)
	case tea.KeyPressMsg:
		if m.Keystroke() == "ctrl+c" {
			if k.lastCtrl {
				return k, tea.Quit
			}
			k.lastCtrl = true
		} else {
			k.lastCtrl = false
		}
		k.add(fmt.Sprintf("press %-18s canonical %-24s text %q", m.Keystroke(), strings.Join(keymap.EventKeys(m), " | "), m.Text))
	case tea.KeyReleaseMsg:
		k.add(fmt.Sprintf("release %s", m.Keystroke()))
	case tea.PasteMsg:
		k.add(fmt.Sprintf("paste %q", m.Content))
	}
	return k, nil
}

func (k *keyprobe) add(s string) {
	k.lines = append(k.lines, s)
	if len(k.lines) > 15 {
		k.lines = k.lines[len(k.lines)-15:]
	}
}

func (k *keyprobe) View() tea.View {
	enh := k.enh
	if enh == "" {
		enh = "(no report: legacy key encoding)"
	}
	v := tea.NewView("mantle keyprobe — press keys; ctrl+c twice to quit\nkeyboard enhancements: " + enh + "\n\n" + strings.Join(k.lines, "\n"))
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}

func cmdKeyprobe(_ []string, _, stderr io.Writer) int {
	if _, err := tea.NewProgram(&keyprobe{}).Run(); err != nil {
		fmt.Fprintln(stderr, "mantle-ui:", err)
		return 1
	}
	return 0
}
