package input

import (
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	xeditor "github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// ---- stash (ctrl+s) ----

const stashKey = "stash"

// toggleStash stashes a non-empty prompt, or restores the stash into an
// empty one. A stash also comes back by itself after the next submit.
func (s *state) toggleStash(c ext.Ctx) tea.Cmd {
	if !s.ed.Empty() {
		s.stash = s.save()
		_ = c.Store(FeatureID).Set(stashKey, s.stash)
		s.ed.Clear()
		s.mode = modePrompt
		return tea.Batch(s.changed(c), c.Notify(ext.Notice{
			Key: "input.stash", Text: "Prompt stashed; ctrl+s brings it back (it also returns after your next prompt)",
			Source: FeatureID,
		}))
	}
	if s.stash == nil {
		return nil
	}
	return s.popStash(c)
}

func (s *state) popStash(c ext.Ctx) tea.Cmd {
	d := s.stash
	s.stash = nil
	_ = c.Store(FeatureID).Delete(stashKey)
	s.restore(d)
	return s.changed(c)
}

func (s *state) loadStash(c ext.Ctx) {
	var d savedDraft
	if ok, err := c.Store(FeatureID).Get(stashKey, &d); ok && err == nil && d.Display != "" {
		s.stash = &d
	}
}

// ---- external editor (ctrl+g, ctrl+x ctrl+e) ----

// contextMarker separates the prompt from the read-only context appended
// when externalEditorContext is on; everything from it down is dropped.
const contextMarker = "# ---- mantle: everything below this line is ignored ----"

// editorDoneMsg returns from $EDITOR.
type editorDoneMsg struct {
	text string
	err  error
}

func (s *state) openExternalEditor(c ext.Ctx) tea.Cmd {
	f, err := os.CreateTemp("", "mantle-prompt-*.md")
	if err != nil {
		return c.Notify(ext.Notice{Key: "input.editor", Text: "Could not open an editor: " + err.Error(), Level: ext.NoticeError, Source: FeatureID})
	}
	path := f.Name()
	content := s.ed.Display()
	if s.cfg.editorContext {
		if last := lastResponse(c); last != "" {
			content += "\n\n" + contextMarker + "\n# Claude's last response:\n#\n"
			for _, l := range strings.Split(last, "\n") {
				content += "# " + l + "\n"
			}
		}
	}
	_, werr := f.WriteString(content)
	f.Close()
	if werr != nil {
		os.Remove(path)
		return c.Notify(ext.Notice{Key: "input.editor", Text: "Could not open an editor: " + werr.Error(), Level: ext.NoticeError, Source: FeatureID})
	}
	cmd, err := xeditor.Command("mantle", path)
	if err != nil {
		os.Remove(path)
		return c.Notify(ext.Notice{Key: "input.editor", Text: err.Error(), Level: ext.NoticeError, Source: FeatureID})
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		b, rerr := os.ReadFile(path)
		os.Remove(path)
		if err == nil {
			err = rerr
		}
		return editorDoneMsg{text: string(b), err: err}
	})
}

func (s *state) externalEditorDone(c ext.Ctx, m editorDoneMsg) tea.Cmd {
	if m.err != nil {
		return c.Notify(ext.Notice{Key: "input.editor", Text: "Editor failed: " + m.err.Error(), Level: ext.NoticeError, Source: FeatureID})
	}
	text := m.text
	if i := strings.Index(text, contextMarker); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimRight(text, "\n ")
	s.ed.SetValueWithChips(text, s.ed.Chips())
	return s.changed(c)
}

// lastResponse is the text of the newest assistant text item.
func lastResponse(c ext.Ctx) string {
	tr := c.Transcript()
	if tr == nil {
		return ""
	}
	items := tr.Items()
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if it.Key != ext.KeyAssistantText {
			continue
		}
		if b, ok := it.Data.(*proto.ContentBlock); ok && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

// ---- ? help ----

// helpEntries are the shortcuts the ? panel lists, by action.
var helpEntries = []struct {
	action ext.ActionID
	desc   string
}{
	{ext.ActChatNewline, "newline"},
	{ext.ActChatUndo, "undo"},
	{ext.ActChatCycleMode, "cycle modes"},
	{ext.ActChatModelPicker, "switch model"},
	{ext.ActHistorySearch, "search history"},
	{ext.ActChatExternalEditor, "edit in $EDITOR"},
	{ext.ActChatStash, "stash prompt"},
	{ext.ActChatImagePaste, "paste image"},
	{ext.ActChatSendNow, "send now"},
	{ext.ActChatQueueSubmit, "queue prompt"},
	{ext.ActAppToggleTranscript, "transcript"},
	{ext.ActAppToggleTodos, "toggle todos"},
	{ext.ActChatClearInput, "clear prompt"},
	{ext.ActAppExit, "exit"},
}

func (s *state) helpLines(c ext.Ctx, width int) []string {
	t := c.Theme()
	cells := []string{
		"! for bash mode",
		"/ for commands",
		"@ for file paths",
		`\⏎ for newline`,
		"esc esc to clear",
	}
	for _, h := range helpEntries {
		keys := c.KeysFor("", h.action)
		if len(keys) == 0 {
			continue
		}
		cells = append(cells, keys[0]+" "+h.desc)
	}
	// As many columns (up to 3) as fit, each as wide as its widest cell.
	const gap = 3
	var rows int
	var widths []int
	for cols := 3; cols >= 1; cols-- {
		rows = (len(cells) + cols - 1) / cols
		widths = make([]int, cols)
		total := 2 + gap*(cols-1)
		for i, x := range cells {
			widths[i/rows] = max(widths[i/rows], ansi.StringWidth(x))
		}
		for _, w := range widths {
			total += w
		}
		if total <= width {
			break
		}
	}
	var out []string
	for r := 0; r < rows; r++ {
		var b strings.Builder
		b.WriteString("  ")
		for col := range widths {
			i := col*rows + r
			if i >= len(cells) {
				break
			}
			b.WriteString(cells[i])
			if col < len(widths)-1 {
				b.WriteString(strings.Repeat(" ", widths[col]-ansi.StringWidth(cells[i])+gap))
			}
		}
		out = append(out, t.Paint(theme.Inactive, strings.TrimRight(b.String(), " ")))
	}
	return out
}

// ---- keyword highlighting ----

// rainbow colours "ultrathink" one letter at a time.
var rainbow = []theme.Token{
	theme.AgentRed, theme.AgentOrange, theme.AgentYellow, theme.AgentGreen,
	theme.AgentCyan, theme.AgentBlue, theme.AgentPurple,
}

// decorate styles keywords in the prompt: "ultrathink" in rainbow and
// "ultracode" in the accent colour while its trigger is on.
func (s *state) decorate(row int, gs []string) []editor.Span {
	t := s.theme
	if t == nil || len(gs) < 9 {
		return nil
	}
	lower := make([]string, len(gs))
	for i, g := range gs {
		lower[i] = strings.ToLower(g)
	}
	var spans []editor.Span
	find := func(word string, f func(start int)) {
		n := len(word)
		for i := 0; i+n <= len(lower); i++ {
			if strings.Join(lower[i:i+n], "") != word {
				continue
			}
			if (i > 0 && isWordGrapheme(gs[i-1])) || (i+n < len(gs) && isWordGrapheme(gs[i+n])) {
				continue
			}
			f(i)
			i += n - 1
		}
	}
	find("ultrathink", func(start int) {
		for k := 0; k < 10; k++ {
			spans = append(spans, editor.Span{Start: start + k, End: start + k + 1, Style: lipgloss.NewStyle().Foreground(t.Color(rainbow[k%len(rainbow)]))})
		}
	})
	if s.workflowKw {
		find("ultracode", func(start int) {
			spans = append(spans, editor.Span{Start: start, End: start + 9, Style: lipgloss.NewStyle().Foreground(t.Color(theme.Accent)).Bold(true)})
		})
	}
	return spans
}

func isWordGrapheme(g string) bool {
	for _, r := range g {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	return false
}

// toggleWorkflowKeyword is meta+w: turn the ultracode keyword trigger on or
// off for this session.
func (s *state) toggleWorkflowKeyword(c ext.Ctx) tea.Cmd {
	s.workflowKw = !s.workflowKw
	var cmds []tea.Cmd
	if eng := c.Engine(ext.MainEngine); eng != nil {
		cmds = append(cmds, eng.Control(proto.SubApplyFlagSettings, proto.ApplyFlagSettingsRequest{
			Settings: map[string]any{"workflowKeywordTriggerEnabled": s.workflowKw},
		}))
	}
	text := "ultracode keyword off"
	if s.workflowKw {
		text = "ultracode keyword on"
	}
	cmds = append(cmds, c.Notify(ext.Notice{Key: "input.ultracode", Text: text, Source: FeatureID}))
	s.invalidate(c)
	return tea.Batch(cmds...)
}

// ---- /vim ----

// cmdVim toggles vim editing for this session and remembers the choice in
// mantle's settings (input.editorMode), which overrides editorMode.
func (s *state) cmdVim(c ext.Ctx, args string) tea.Cmd {
	s.cfg.vim = !s.cfg.vim
	s.ed.SetVim(s.cfg.vim, s.cfg.remaps)
	mode, text := "normal", "Editor mode set to normal. Esc no longer enters NORMAL mode."
	if s.cfg.vim {
		mode, text = "vim", "Editor mode set to vim. Esc enters NORMAL mode."
	}
	return tea.Batch(
		c.Settings().SetMantle(SettingEditorMode, mode),
		c.Notify(ext.Notice{Key: "input.vim", Text: text, Source: FeatureID}),
		s.changed(c),
	)
}
