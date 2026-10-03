package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/features/settings/themes"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogTheme = "dialog.theme"
	themeID     = "settings.theme.picker"
)

func (a *area) setupTheme(r ext.Registrar) error {
	r.AddDialog(dialogTheme, a.newThemePicker)
	a.subscribeThemePreview(r)
	r.AddCommand(ext.Command{
		Name: "theme", Description: "Change the colour theme", Source: ext.SourceBuiltin,
		Run: func(c ext.Ctx, _ string) tea.Cmd { return c.OpenDialog(dialogTheme, nil) },
	})
	r.AddStory(ext.Story{ID: "settings.theme/picker", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		p := &themePicker{a: storyArea(), dir: "/nonexistent",
			picker: themes.NewPicker(themes.List("/nonexistent"), "dark", false)}
		p.picker.Move(1)
		return p.View(c, ar)
	}})
	return nil
}

func (a *area) themesDir(c ext.Ctx) string {
	return filepath.Join(a.env(c).ClaudeDir(), "themes")
}

type themePicker struct {
	a      *area
	dir    string
	picker *themes.Picker
}

// themeEditedMsg reports that the editor closed.
type themeEditedMsg struct{ err error }

func (a *area) newThemePicker(c ext.Ctx, _ any) (ext.Dialog, error) {
	dir := a.themesDir(c)
	cur := ext.ClaudeString(c.Settings(), "theme", "dark")
	syntaxOff := ext.ClaudeBool(c.Settings(), "syntaxHighlightingDisabled", false)
	return &themePicker{a: a, dir: dir, picker: themes.NewPicker(themes.List(dir), cur, syntaxOff)}, nil
}

func (p *themePicker) ID() string               { return themeID }
func (p *themePicker) Placement() ext.Placement { return ext.PlaceInline }
func (p *themePicker) KeyContext() string       { return ext.ContextThemePicker }
func (p *themePicker) KeyContexts() []string {
	return []string{ext.ContextThemePicker, ext.ContextSelect}
}
func (p *themePicker) Init(ext.Ctx) tea.Cmd                               { return nil }
func (p *themePicker) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (p *themePicker) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }
func (p *themePicker) Result() any                                        { return p.picker.Highlighted().Name }

func (p *themePicker) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(themeEditedMsg); ok {
		// Pick up a new or changed file and keep the cursor where it was.
		cur := p.picker.Highlighted().Name
		p.picker = themes.NewPicker(themes.List(p.dir), p.picker.Original, p.picker.SyntaxOff)
		for i, e := range p.picker.Items {
			if e.Name == cur {
				p.picker.Cursor = i
			}
		}
		c.Invalidate(p.ID())
		if m.err != nil {
			return c.Notify(ext.Notice{Key: "settings.theme", Level: ext.NoticeError, Source: "settings",
				Text: "Editor failed: " + m.err.Error()})
		}
	}
	return nil
}

func (p *themePicker) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActSelectNext:
		p.picker.Move(1)
	case ext.ActSelectPrevious:
		p.picker.Move(-1)
	case ext.ActSelectPageDown, ext.ActSelectLast:
		p.picker.Move(len(p.picker.Items))
	case ext.ActSelectPageUp, ext.ActSelectFirst:
		p.picker.Move(-len(p.picker.Items))
	case ext.ActThemeToggleSyntaxHighlighting:
		p.picker.ToggleSyntax()
	case ext.ActThemeEditCustom:
		return true, p.editCustom(c)
	case ext.ActSelectAccept:
		name, effects := p.picker.Confirm()
		cmds := []tea.Cmd{p.a.apply(c, effects), c.CloseDialog(dialogTheme)}
		if len(effects) == 0 {
			return true, tea.Sequence(append(cmds, ext.Msg(ext.ThemePreviewMsg{}))...)
		}
		// Keep previewing until the settings reload carries the new theme, so the
		// screen doesn't flash back to the old one; a timer covers a failed write.
		p.a.endPreviewPending = true
		cmds = append(cmds, c.Notify(ext.Notice{Key: "settings.theme", Level: ext.NoticeSuccess,
			Source: "settings", Text: "Theme set to " + themes.Label(name)}),
			c.Clock().Tick(3*time.Second, func(time.Time) tea.Msg { return themePreviewTimeoutMsg{} }))
		return true, tea.Sequence(cmds...)
	case ext.ActSelectCancel:
		p.picker.Cancel()
		return true, tea.Sequence(ext.Msg(ext.ThemePreviewMsg{}), c.CloseDialog(dialogTheme))
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, p.previewCmd()
}

// previewCmd asks the host to show the highlighted theme and syntax toggle.
func (p *themePicker) previewCmd() tea.Cmd {
	on := !p.picker.SyntaxOff
	return ext.Msg(ext.ThemePreviewMsg{Name: p.picker.Highlighted().Name, SyntaxHighlight: &on})
}

// themePreviewTimeoutMsg ends a preview still running a while after confirm.
type themePreviewTimeoutMsg struct{}

// subscribeThemePreview ends a confirmed preview once settings carry the new theme.
func (a *area) subscribeThemePreview(r ext.Registrar) {
	end := func() tea.Cmd {
		if !a.endPreviewPending {
			return nil
		}
		a.endPreviewPending = false
		return ext.Msg(ext.ThemePreviewMsg{})
	}
	ext.Subscribe(r, "settings.theme-preview-settings", func(c ext.Ctx, m ext.SettingsMsg) tea.Cmd {
		for _, k := range m.Changed {
			if k == "theme" || k == "syntaxHighlightingDisabled" {
				return end()
			}
		}
		return nil
	})
	ext.Subscribe(r, "settings.theme-preview-timeout", func(c ext.Ctx, _ themePreviewTimeoutMsg) tea.Cmd { return end() })
}

// editCustom opens the highlighted custom theme's file in $EDITOR.
func (p *themePicker) editCustom(c ext.Ctx) tea.Cmd {
	e := p.picker.Highlighted()
	path, ok := themes.CustomPath(p.dir, e.Name)
	if !ok {
		return c.Notify(ext.Notice{Key: "settings.theme", Source: "settings",
			Text: "Highlight a custom theme to edit it. Custom themes live in " + shortHome(p.dir, p.a.env(c).Home) + "."})
	}
	cmd, err := editor.Cmd("mantle", path)
	if err != nil {
		return c.Notify(ext.Notice{Key: "settings.theme", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return ext.AddressedMsg{To: themeID, Msg: themeEditedMsg{err}} })
}

func shortHome(p, home string) string {
	if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// previewTheme resolves a theme setting value to a theme for the preview. Custom
// themes preview their base; the host's theme engine applies their overrides.
func (p *themePicker) previewTheme(name string) theme.Theme {
	if t, ok := theme.Builtin(name); ok {
		return t
	}
	if path, ok := themes.CustomPath(p.dir, name); ok {
		if b, err := os.ReadFile(path); err == nil {
			var f struct {
				Base string `json:"base"`
			}
			if json.Unmarshal(b, &f) == nil {
				if t, ok := theme.Builtin(f.Base); ok {
					return t
				}
			}
		}
	}
	return theme.Default()
}

func (p *themePicker) preview(name string, syntaxOff bool, width int) []string {
	pt := p.previewTheme(name)
	t := &pt
	kw := func(s string) string {
		if syntaxOff {
			return s
		}
		return t.Paint(theme.SyntaxKeyword, s)
	}
	str := func(s string) string {
		if syntaxOff {
			return s
		}
		return t.Paint(theme.SyntaxString, s)
	}
	pad := func(s string) string { return fit(s, width) }
	return []string{
		pad(t.Paint(theme.Inactive, " 1 ") + kw("func") + " greet(name " + kw("string") + ") " + kw("string") + " {"),
		pad(t.Bg(theme.DiffRemoved).Render(t.Paint(theme.Text, " 2 -    "+kw("return")+" "+str(`"hi "`)+" + name"))),
		pad(t.Bg(theme.DiffAdded).Render(t.Paint(theme.Text, " 2 +    "+kw("return")+" "+str(`"Hello, "`)+" + name + "+str(`"!"`)))),
		pad(t.Paint(theme.Inactive, " 3 ") + "}"),
		pad(t.Paint(theme.Accent, "✻") + " " + t.Paint(theme.Success, "done") + "  " + t.Paint(theme.Warning, "warning") + "  " + t.Paint(theme.Error, "error")),
	}
}

func (p *themePicker) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	rows := make([]listRow, len(p.picker.Items))
	for i, e := range p.picker.Items {
		rows[i] = listRow{Label: e.Label, Current: e.Name == p.picker.Original, Disabled: !e.Selectable()}
		if e.Err != "" {
			rows[i].Detail = e.Err
		}
	}
	body := listLines(t, ar.Width-4, rows, p.picker.Cursor, listHeight(ar.MaxHeight, termH, 16))
	syntax := "on"
	if p.picker.SyntaxOff {
		syntax = "off"
	}
	body = append(body, "", t.Paint(theme.Inactive, "Preview · syntax highlighting "+syntax))
	body = append(body, p.preview(p.picker.Highlighted().Name, p.picker.SyntaxOff, ar.Width-4)...)
	return ext.Rendered{Text: frame(t, ar.Width, "Theme", "Choose the colours mantle uses. Custom themes come from "+
		shortHome(p.dir, p.a.env(c).Home)+".", body, hintLine(
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectAccept), "enter"), "choose",
		keyName(c.KeysFor(ext.ContextThemePicker, ext.ActThemeToggleSyntaxHighlighting), "ctrl+t"), "toggle syntax highlighting",
		keyName(c.KeysFor(ext.ContextThemePicker, ext.ActThemeEditCustom), "ctrl+e"), "edit custom theme",
		keyName(c.KeysFor(ext.ContextSelect, ext.ActSelectCancel), "esc"), "cancel"))}
}

var _ ext.ActionHandler = (*themePicker)(nil)
