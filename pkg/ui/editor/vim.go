package editor

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ui/editor/vim"
)

// vimState connects the vim machine to the editor.
type vimState struct {
	m   *vim.Machine
	seq int // remap timeout generation
}

// VimTimeoutMsg fires when an insert-mode remap prefix times out. Hosts
// pass it to Editor.Update.
type VimTimeoutMsg struct {
	ed  *Editor
	seq int
}

// VimEventMsg asks the host to act for vim mode: open history search ("/"
// in NORMAL mode) or recall history (k/j at the first/last line).
type VimEventMsg struct {
	Event vim.Event
}

// SetVim turns vim mode on or off (Claude Code's editorMode "vim") and sets
// the insert-mode remaps (vimInsertModeRemaps, e.g. {"jj": "<Esc>"}). Vim
// mode starts in INSERT.
func (e *Editor) SetVim(on bool, remaps map[string]string) {
	if !on {
		if e.vim != nil {
			e.vim.m.Reset()
			e.vim = nil
			e.touch()
		}
		return
	}
	if e.vim == nil {
		e.vim = &vimState{m: vim.New(vimTarget{e})}
	}
	e.vim.m.SetRemaps(remaps)
	e.touch()
}

// VimEnabled reports whether vim mode is on.
func (e *Editor) VimEnabled() bool { return e.vim != nil }

// VimMode returns the vim mode; INSERT when vim mode is off.
func (e *Editor) VimMode() vim.Mode {
	if e.vim == nil {
		return vim.Insert
	}
	return e.vim.m.Mode()
}

// SetVimMode switches the vim mode (no-op when vim mode is off).
func (e *Editor) SetVimMode(m vim.Mode) {
	if e.vim != nil {
		e.vim.m.SetMode(m)
		e.touch()
	}
}

// Update handles the editor's own messages (VimTimeoutMsg). It returns
// nil for anything else.
func (e *Editor) Update(msg tea.Msg) tea.Cmd {
	if m, ok := msg.(VimTimeoutMsg); ok && m.ed == e && e.vim != nil && m.seq == e.vim.seq {
		e.vim.m.Flush()
		e.touch()
	}
	return nil
}

func (v *vimState) handle(e *Editor, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if e.attach >= 0 {
		return e.handleBasicKey(msg), nil
	}
	res := v.m.Feed(vim.Key{Text: typedText(msg), Name: msg.Keystroke()})
	e.touch()
	var cmds []tea.Cmd
	if res.Pending {
		v.seq++
		seq, d := v.seq, v.m.RemapTimeout
		cmds = append(cmds, tea.Tick(d, func(time.Time) tea.Msg { return VimTimeoutMsg{ed: e, seq: seq} }))
	}
	if res.Event != vim.EventNone {
		ev := res.Event
		cmds = append(cmds, func() tea.Msg { return VimEventMsg{Event: ev} })
	}
	return res.Handled, tea.Batch(cmds...)
}

func (v *vimState) reset() { v.m.Reset() }

func (v *vimState) blockCursor() bool {
	switch v.m.Mode() {
	case vim.Normal, vim.Visual, vim.VisualLine:
		return true
	}
	return false
}

func (e *Editor) selection() (Pos, Pos, bool) {
	if e.vim == nil {
		return Pos{}, Pos{}, false
	}
	from, to, _, ok := e.vim.m.Selection()
	return Pos(from), Pos(to), ok
}

// vimTarget implements vim.Target over the editor. Its edits never move the
// cursor; the machine positions it explicitly.
type vimTarget struct{ e *Editor }

func (t vimTarget) LineCount() int    { return len(t.e.lines) }
func (t vimTarget) LineLen(r int) int { return len(t.e.lines[r].cells) }

func (t vimTarget) Grapheme(r, c int) string {
	cl := t.e.lines[r].cells[c]
	if cl.chip != nil {
		return vim.ChipGrapheme
	}
	return cl.g
}

func (t vimTarget) Cursor() vim.Pos { return vim.Pos(t.e.cur) }

func (t vimTarget) SetCursor(p vim.Pos) {
	t.e.cur = t.e.clamp(Pos(p))
	t.e.goalX = -1
	t.e.touch()
}

func (t vimTarget) Copy(from, to vim.Pos) vim.Clip {
	return t.e.copyRange(t.e.clamp(Pos(from)), t.e.clamp(Pos(to)))
}

func (t vimTarget) Delete(from, to vim.Pos) vim.Clip {
	e := t.e
	e.checkpoint(editOther)
	cur := e.cur
	f := e.deleteRange(e.clamp(Pos(from)), e.clamp(Pos(to)))
	e.cur = e.clamp(cur)
	e.afterEdit()
	return f
}

func (t vimTarget) Insert(p vim.Pos, c vim.Clip) vim.Pos {
	f, _ := c.(fragment)
	return t.insert(Pos(p), f)
}

func (t vimTarget) InsertText(p vim.Pos, s string) vim.Pos {
	return t.insert(Pos(p), fragment(splitLines(s)))
}

func (t vimTarget) insert(p Pos, f fragment) vim.Pos {
	e := t.e
	if len(f) == 0 {
		return vim.Pos(p)
	}
	e.checkpoint(editOther)
	cur := e.cur
	e.cur = e.clamp(p)
	e.insertFragment(f)
	end := e.cur
	e.cur = e.clamp(cur)
	e.afterEdit()
	return vim.Pos(end)
}

func (t vimTarget) Key(k vim.Key) bool { return t.e.handleBasicKey(teaKey(k)) }
func (t vimTarget) BeginGroup()        { t.e.BeginGroup() }
func (t vimTarget) EndGroup()          { t.e.EndGroup() }
func (t vimTarget) Undo() bool         { return t.e.Undo() }
func (t vimTarget) Redo() bool         { return t.e.Redo() }

var keyNames = map[string]rune{
	"enter": tea.KeyEnter, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
	"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
	"home": tea.KeyHome, "end": tea.KeyEnd, "esc": tea.KeyEscape, "tab": tea.KeyTab,
	"space": tea.KeySpace, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
}

// teaKey rebuilds a key press from a vim key (for replays).
func teaKey(k vim.Key) tea.KeyPressMsg {
	if k.Text != "" {
		r, _ := utf8.DecodeRuneInString(k.Text)
		if utf8.RuneCountInString(k.Text) > 1 {
			r = tea.KeyExtended
		}
		return tea.KeyPressMsg{Code: r, Text: k.Text}
	}
	var key tea.Key
	name := k.Name
	for {
		i := strings.IndexByte(name, '+')
		if i <= 0 || i == len(name)-1 {
			break
		}
		switch name[:i] {
		case "ctrl":
			key.Mod |= tea.ModCtrl
		case "alt":
			key.Mod |= tea.ModAlt
		case "shift":
			key.Mod |= tea.ModShift
		case "meta":
			key.Mod |= tea.ModMeta
		case "super":
			key.Mod |= tea.ModSuper
		case "hyper":
			key.Mod |= tea.ModHyper
		default:
			i = -1
		}
		if i < 0 {
			break
		}
		name = name[i+1:]
	}
	if c, ok := keyNames[name]; ok {
		key.Code = c
	} else {
		key.Code, _ = utf8.DecodeRuneInString(name)
	}
	return tea.KeyPressMsg(key)
}
