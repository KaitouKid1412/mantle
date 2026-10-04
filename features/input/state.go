package input

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/vim"
)

// Prompt modes (ext.Draft.Mode, ext.EditorStateMsg.Mode).
const (
	modePrompt = "prompt"
	modeBash   = "bash"
)

// doubleEscWindow is how close two esc presses must be to count as a double.
const doubleEscWindow = 800 * time.Millisecond

// state is shared by the prompt and menu components, the actions and the
// prompt stages. Everything runs on the UI goroutine.
type state struct {
	ed   *editor.Editor
	mode string

	// engine
	busy       bool
	queue      []queued
	engineCmds []proto.SlashCommand

	// history
	histPath  string
	hist      *history.Navigator
	histAll   []history.Entry // oldest first, every project
	draft     *savedDraft     // the user's draft while recalling history
	cwd       string
	sessionID string

	// panels
	comp   completion
	search *search
	help   bool
	stash  *savedDraft

	escAt       time.Time
	lastSent    *savedDraft // restored when the send stage rejects
	pending     *pendingSubmit
	pendingHist *history.Entry // set while the pipeline runs for this submit
	suggestion  string
	workflowKw  bool

	cfg       config
	spell     spell
	theme     *theme.Theme
	lastState ext.EditorStateMsg
	stateSent bool
	seq       int // debounce generation for @ suggestions
	now       func() time.Time
}

// config is what the feature reads from settings.
type config struct {
	vim           bool
	remaps        map[string]string
	emoji         bool
	suggestions   bool
	respondBash   bool
	editorContext bool
	writeHistory  bool
	virtualCursor bool
}

// savedDraft is editor content kept aside (history draft, stash, rejected
// submit).
type savedDraft struct {
	Display string            `json:"display"`
	Pastes  map[string]string `json:"pastes,omitempty"` // chip id -> text
	Mode    string            `json:"mode"`
	Cursor  editor.Pos        `json:"cursor"`
	chips   []*editor.Chip
}

func newState() *state {
	s := &state{
		ed:       editor.New(),
		mode:     modePrompt,
		histPath: history.Path(),
		hist:     history.NewNavigator(nil),
		now:      time.Now,
		cfg:      config{emoji: true, suggestions: true, respondBash: true, writeHistory: true},
		spell:    newSpell(),
	}
	s.ed.KeyMap.ClearActionKeys()
	s.ed.Paste.Store = editor.DirStore{Dir: history.PasteCacheDir()}
	s.ed.VirtualCursor = false
	s.ed.Decorate = s.decorate
	if wd, err := os.Getwd(); err == nil {
		s.cwd = wd
	}
	return s
}

// start runs once the program is up: read settings, load history, report
// the editor state.
func (s *state) start(c ext.Ctx) tea.Cmd {
	s.ed.Tick = c.Clock().Tick
	s.applySettings(c)
	s.workflowKw = ext.ClaudeBool(c.Settings(), "workflowKeywordTriggerEnabled", true)
	s.syncSession(c.Session())
	s.applyTheme(c.Theme())
	s.loadStash(c)
	if st, ok := cli.Current(); ok && st.Prefill != "" {
		s.setText(st.Prefill) // --prefill: shown, not sent
	}
	return tea.Batch(s.loadHistory(), s.stateCmd(true))
}

func (s *state) syncSession(info ext.SessionInfo) {
	if info.Cwd != "" && info.Cwd != s.cwd {
		s.cwd = info.Cwd
		s.rebuildNav()
	}
	if info.SessionID != "" {
		s.sessionID = info.SessionID
	}
}

func (s *state) applySettings(c ext.Ctx) {
	st := c.Settings()
	s.cfg.vim = ext.ClaudeString(st, "editorMode", "normal") == "vim"
	s.cfg.remaps = nil
	if v, ok := st.Claude("vimInsertModeRemaps"); ok {
		if m, ok := v.(map[string]any); ok {
			s.cfg.remaps = map[string]string{}
			for k, val := range m {
				if str, ok := val.(string); ok {
					s.cfg.remaps[k] = str
				}
			}
		}
	}
	s.cfg.emoji = ext.ClaudeBool(st, "emojiCompletionEnabled", true)
	s.cfg.suggestions = ext.ClaudeBool(st, "promptSuggestionEnabled", true)
	s.cfg.respondBash = ext.ClaudeBool(st, "respondToBashCommands", true)
	s.cfg.editorContext = ext.ClaudeBool(st, "externalEditorContext", false)
	s.cfg.writeHistory = mantleBool(st, SettingWriteHistory, true)
	s.cfg.virtualCursor = mantleBool(st, SettingVirtualCursor, false)
	s.spell.configure(st)
	s.ed.VirtualCursor = s.cfg.virtualCursor
	s.ed.SetVim(s.cfg.vim, s.cfg.remaps)
}

func mantleBool(st ext.Settings, key string, def bool) bool {
	if b, ok := st.Mantle(key).(bool); ok {
		return b
	}
	return def
}

func (s *state) applyTheme(t *theme.Theme) {
	if t == nil {
		return
	}
	s.theme = t
	st := editor.DefaultStyles()
	st.Text = t.Fg(theme.Text)
	st.Placeholder = t.Fg(theme.Inactive)
	st.Ghost = t.Fg(theme.Inactive)
	st.Chip = t.Fg(theme.Suggestion)
	st.ChipSelected = t.Fg(theme.InverseText).Background(t.Color(theme.Suggestion))
	st.Selection = lipgloss.NewStyle().Background(t.Color(theme.SelectionBg))
	s.ed.Styles = st
}

// editorState is what chrome needs to know about the prompt.
func (s *state) editorState() ext.EditorStateMsg {
	m := ext.EditorStateMsg{Mode: s.mode, Empty: s.ed.Empty()}
	if s.ed.VimEnabled() {
		m.Vim = s.ed.VimMode().String()
	}
	return m
}

// stateCmd broadcasts EditorStateMsg when it changed (or always with force).
func (s *state) stateCmd(force bool) tea.Cmd {
	m := s.editorState()
	if !force && s.stateSent && m == s.lastState {
		return nil
	}
	s.lastState, s.stateSent = m, true
	return ext.Msg(m)
}

// invalidate re-renders both components.
func (s *state) invalidate(c ext.Ctx) {
	c.Invalidate(ComponentID)
	c.Invalidate(MenuID)
}

// vimNormal reports whether vim mode is on and not inserting.
func (s *state) vimNormal() bool {
	if !s.ed.VimEnabled() {
		return false
	}
	m := s.ed.VimMode()
	return m != vim.Insert && m != vim.Replace
}

// ---- saved drafts ----

func (s *state) save() *savedDraft {
	d := &savedDraft{Display: s.ed.Display(), Mode: s.mode, Cursor: s.ed.Cursor(), chips: s.ed.Chips()}
	for _, ch := range d.chips {
		if ch.Kind == editor.ChipPaste {
			if d.Pastes == nil {
				d.Pastes = map[string]string{}
			}
			d.Pastes[itoa(ch.ID)] = ch.Text
		}
	}
	return d
}

func (s *state) restore(d *savedDraft) {
	if d == nil {
		return
	}
	chips := d.chips
	if chips == nil {
		for id, text := range d.Pastes {
			ch := editor.NewPasteChip(atoi(id), text)
			chips = append(chips, ch)
		}
	}
	s.ed.SetValueWithChips(d.Display, chips)
	s.ed.SetCursor(d.Cursor)
	s.mode = d.Mode
	if s.mode == "" {
		s.mode = modePrompt
	}
}

// setText replaces the prompt with text from elsewhere (rewind, deep links):
// undoable, cursor at the end, never submitted. Chip labels in text that
// match chips in the editor come back as chips.
func (s *state) setText(text string) {
	s.search = nil
	s.help = false
	s.comp.close()
	s.mode = modePrompt
	s.hist.Reset()
	s.draft = nil
	s.ed.SetValueWithChips(text, s.ed.Chips())
}

func (s *state) clearEditor() {
	s.ed.Reset()
	s.mode = modePrompt
	s.comp.close()
	s.help = false
}
