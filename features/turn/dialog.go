package turn

import (
	"image/color"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/render"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/diffview"
)

// vmDialog adapts a dialogs.Model view-model to ext.Dialog. The view-model holds all
// state and key handling; the adapter maps its Effects onto host actions.
type vmDialog struct {
	id       string // registry ID, also the instance ID (one instance at a time)
	vm       dialogs.Model
	contexts []string
	place    ext.Placement
	// onDone runs once the view-model is answered (reply, close, advance).
	onDone func(ext.Ctx) tea.Cmd
	// onCycle handles shift+tab inside the dialog (switch the permission mode).
	onCycle func(ext.Ctx) tea.Cmd
	// onInterrupt handles ctrl+c while the dialog is open; nil leaves it to app:interrupt.
	onInterrupt func(ext.Ctx) tea.Cmd
	result      func() any
	finished    bool
}

var (
	_ ext.Dialog        = (*vmDialog)(nil)
	_ ext.ActionHandler = (*vmDialog)(nil)
	_ ext.ContextStack  = (*vmDialog)(nil)
	_ ext.Resulter      = (*vmDialog)(nil)
)

func (d *vmDialog) ID() string               { return d.id }
func (d *vmDialog) Init(ext.Ctx) tea.Cmd     { return nil }
func (d *vmDialog) Placement() ext.Placement { return d.place }
func (d *vmDialog) KeyContext() string       { return d.contexts[0] }
func (d *vmDialog) KeyContexts() []string    { return d.contexts }

// Result implements ext.Resulter.
func (d *vmDialog) Result() any {
	if d.result == nil {
		return nil
	}
	return d.result()
}

// editedMsg carries $EDITOR's result back to the dialog that asked for it.
type editedMsg struct {
	text string
	err  error
}

func (d *vmDialog) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(editedMsg); ok {
		if m.err != nil {
			return c.Notify(ext.Notice{Key: "turn.editor", Text: "Editor failed: " + m.err.Error(), Level: ext.NoticeError, Source: FeatureID})
		}
		if e, ok := d.vm.(interface{ SetEditedText(string) }); ok {
			e.SetEditedText(m.text)
			c.Invalidate(d.id)
		}
	}
	return nil
}

func (d *vmDialog) View(c ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: d.vm.View(a.Width, stylesFor(c.Theme()))}
}

func (d *vmDialog) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	h, eff := d.vm.HandleKey(k)
	return h, d.after(c, h, eff)
}

func (d *vmDialog) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	h, eff := d.vm.HandlePaste(p.Content)
	return h, d.after(c, h, eff)
}

func (d *vmDialog) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	if id == ext.ActAppInterrupt && d.onInterrupt != nil && !d.finished {
		d.finished = true
		return true, d.onInterrupt(c)
	}
	h, eff := d.vm.Action(string(id))
	return h, d.after(c, h, eff)
}

func (d *vmDialog) after(c ext.Ctx, handled bool, eff dialogs.Effect) tea.Cmd {
	if handled {
		c.Invalidate(d.id)
	}
	switch eff {
	case dialogs.Answered:
		return d.done(c)
	case dialogs.CycleMode:
		if d.onCycle != nil {
			return d.onCycle(c)
		}
	case dialogs.EditExternal:
		if e, ok := d.vm.(interface{ EditText() string }); ok {
			return editExternal(d.id, e.EditText())
		}
	case dialogs.OpenURL:
		if u, ok := d.vm.(interface{ URL() string }); ok {
			return tea.Batch(openURL(u.URL()), d.done(c))
		}
		return d.done(c)
	}
	return nil
}

func (d *vmDialog) done(c ext.Ctx) tea.Cmd {
	if d.finished {
		return nil
	}
	d.finished = true
	return d.onDone(c)
}

// editExternal opens text in $VISUAL/$EDITOR and sends the edited text back to the
// dialog with ID to.
func editExternal(to, text string) tea.Cmd {
	f, err := os.CreateTemp("", "mantle-plan-*.md")
	if err != nil {
		return ext.Address(to, editedMsg{err: err})
	}
	path := f.Name()
	_, err = f.WriteString(text)
	f.Close()
	if err != nil {
		os.Remove(path)
		return ext.Address(to, editedMsg{err: err})
	}
	cmd, err := editor.Cmd("mantle", path)
	if err != nil {
		os.Remove(path)
		return ext.Address(to, editedMsg{err: err})
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return ext.AddressedMsg{To: to, Msg: editedMsg{err: err}}
		}
		b, rerr := os.ReadFile(path)
		return ext.AddressedMsg{To: to, Msg: editedMsg{text: string(b), err: rerr}}
	})
}

// openURL opens u in the default browser without touching the terminal.
func openURL(u string) tea.Cmd {
	return func() tea.Msg {
		name := "xdg-open"
		if runtime.GOOS == "darwin" {
			name = "open"
		}
		cmd := exec.Command(name, u)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		_ = cmd.Start()
		go func() { _ = cmd.Wait() }()
		return nil
	}
}

// palette adapts the theme to pkg/render's token lookup.
func palette(t *theme.Theme) render.Palette {
	if t == nil {
		return render.NoColor
	}
	return render.PaletteFunc(func(tok string) color.Color { return t.Color(theme.Token(tok)) })
}

// renderHooks returns the edit-preview and markdown renderers for dialogs: plan 03's
// diffview and markdown renderer, in the current theme.
func renderHooks(c ext.Ctx) (diff func(old, new, path string, width, maxLines int) []string, md func(src string, width int) []string) {
	pal := palette(c.Theme())
	noHL, _ := settingValue(c, "syntaxHighlightingDisabled").(bool)
	files := map[string][2]string{} // per edit: the whole file before and after
	diff = func(old, new, path string, width, maxLines int) []string {
		key := path + "\x00" + old + "\x00" + new
		pair, ok := files[key]
		if !ok {
			pair = wholeFileEdit(path, old, new)
			// Tabs as two columns, like Claude Code's diff previews.
			pair = [2]string{render.ExpandTabs(pair[0], 2), render.ExpandTabs(pair[1], 2)}
			files[key] = pair
		}
		return diffview.Render(diffview.FromStrings(pair[0], pair[1], 3), diffview.Options{
			Width: width, Palette: pal, Filename: path, NoHighlight: noHL, MaxLines: maxLines,
		})
	}
	md = func(src string, width int) []string {
		return render.Markdown(src, render.MarkdownOptions{Width: width, Palette: pal, NoHighlight: noHL})
	}
	return diff, md
}

// maxPreviewFile caps the file read for an edit preview.
const maxPreviewFile = 2 << 20

// wholeFileEdit returns the file before and after an edit, so the preview shows real
// line numbers and surrounding context. old == "" is a Write (the whole new content).
// When the file can't be read or old isn't in it, the edit strings themselves are
// diffed.
func wholeFileEdit(path, old, new string) [2]string {
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() || st.Size() > maxPreviewFile {
		return [2]string{old, new}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return [2]string{old, new}
	}
	content := dialogs.SanitizeText(string(b))
	if old == "" {
		return [2]string{content, new}
	}
	i := strings.Index(content, old)
	if i < 0 {
		return [2]string{old, new}
	}
	return [2]string{content, content[:i] + new + content[i+len(old):]}
}

// stylesFor maps theme tokens onto the dialog styles.
func stylesFor(t *theme.Theme) dialogs.Styles {
	if t == nil {
		return dialogs.PlainStyles()
	}
	return dialogs.Styles{
		Border:   t.Fg(theme.Permission),
		Title:    lipgloss.NewStyle().Bold(true),
		Text:     lipgloss.NewStyle(),
		Dim:      t.Fg(theme.Inactive),
		Code:     t.Fg(theme.Text),
		Selected: t.Fg(theme.Suggestion),
		Accent:   t.Fg(theme.Suggestion),
		Error:    t.Fg(theme.Error),
		Warning:  t.Fg(theme.Warning),
		DiffAdd:  t.Bg(theme.DiffAdded),
		DiffDel:  t.Bg(theme.DiffRemoved),
		Cursor:   lipgloss.NewStyle().Reverse(true),
		BorderColor: func(kind string) lipgloss.Style {
			switch kind {
			case "planMode":
				return t.Fg(theme.PlanMode)
			case "warning":
				return t.Fg(theme.Warning)
			case "error":
				return t.Fg(theme.Error)
			}
			return t.Fg(theme.Permission)
		},
	}
}
