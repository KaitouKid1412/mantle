// Command s16ui is a tiny Bubble Tea program for the launcher spike S16
// test: ctrl+z suspends, "e" runs an editor-like child process, "p" panics
// in raw mode, "q" quits.
package main

import (
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type model struct{ status string }

type editorDone struct{ err error }

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+z":
			return m, tea.Suspend
		case "e":
			c := exec.Command("sh", "-c", "echo editor-running; sleep 30")
			return m, tea.ExecProcess(c, func(err error) tea.Msg { return editorDone{err} })
		case "p":
			panic("s16 boom")
		case "q":
			return m, tea.Quit
		}
	case tea.ResumeMsg:
		m.status = "resumed"
	case editorDone:
		m.status = "editor-done"
	case tea.InterruptMsg:
		m.status = "interrupt-msg"
	}
	return m, nil
}

func (m model) View() tea.View {
	return tea.NewView(fmt.Sprintf("s16 ready pid=%d status=%s\n", os.Getpid(), m.status))
}

func main() {
	if _, err := tea.NewProgram(model{status: "none"}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
