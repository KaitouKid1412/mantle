package app

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Theme engine (B7). The active theme comes from, in order: a /theme preview
// (ext.ThemePreviewMsg), the "theme" setting, else "dark" (Claude Code's default).
// "auto" picks dark or light from the terminal background (tea.BackgroundColorMsg),
// defaulting to dark until the terminal answers. "custom:<name>" themes come from
// Options.CustomThemes (loaded from ~/.claude/themes) or AddTheme registrations.
// Bubble Tea downgrades colours to the terminal's colour profile.

type themeState struct {
	bgKnown bool
	bgDark  bool
	preview string
	syntax  *bool // previewed syntax highlighting
	rev     int
}

// themeName is the name selected by preview or settings.
func (r *Root) themeName() string {
	if r.th.preview != "" {
		return r.th.preview
	}
	return ext.ClaudeString(r.opts.Settings, "theme", theme.NameDark)
}

// lookupTheme resolves a theme name to a theme; ok is false for unknown names.
func (r *Root) lookupTheme(name string) (theme.Theme, bool) {
	if name == theme.NameAuto {
		if r.th.bgKnown && !r.th.bgDark {
			name = theme.NameLight
		} else {
			name = theme.NameDark
		}
	}
	if t, ok := r.themes[name]; ok {
		return t, true
	}
	if t, ok := r.opts.CustomThemes[strings.TrimPrefix(name, theme.CustomPrefix)]; ok {
		t.Name = name
		return t, true
	}
	return theme.Theme{}, false
}

// applyTheme re-resolves the theme and reports whether it changed.
func (r *Root) applyTheme() tea.Cmd {
	name := r.themeName()
	t, ok := r.lookupTheme(name)
	if !ok {
		t = theme.Default()
	}
	same := t.Name == r.theme.Name && t.Dark == r.theme.Dark && len(t.Colors) == len(r.theme.Colors)
	if same {
		for k, c := range t.Colors {
			if r.theme.Colors[k] != c {
				same = false
				break
			}
		}
	}
	if same {
		return nil
	}
	r.theme = t
	r.th.rev++
	r.invalidateAll()
	return r.broadcast(ext.ThemeChangedMsg{Name: t.Name})
}

// themeUpdate handles theme-related messages; handled reports whether msg was one.
func (r *Root) themeUpdate(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.BackgroundColorMsg:
		r.th.bgKnown, r.th.bgDark = true, m.IsDark()
		return tea.Batch(r.broadcast(msg), r.applyTheme()), true
	case ext.ThemePreviewMsg:
		r.th.preview, r.th.syntax = m.Name, m.SyntaxHighlight
		if m.Name == "" {
			r.th.syntax = nil
		}
		r.invalidateAll()
		return tea.Batch(r.broadcast(msg), r.applyTheme()), true
	case ext.SettingsMsg:
		cmds := []tea.Cmd{r.broadcast(msg)}
		if slices.Contains(m.Changed, "theme") || len(m.Changed) == 0 {
			cmds = append(cmds, r.applyTheme())
		}
		r.invalidateAll()
		return tea.Batch(cmds...), true
	}
	return nil, false
}

// SetCustomThemes replaces the custom themes (hot reload of ~/.claude/themes).
func (r *Root) SetCustomThemes(ts map[string]theme.Theme) tea.Cmd {
	r.opts.CustomThemes = ts
	return r.applyTheme()
}

// previewSettings overlays a previewed syntax-highlighting choice on the settings.
type previewSettings struct {
	ext.Settings
	r *Root
}

func (p previewSettings) Claude(key string) (any, bool) {
	if key == "syntaxHighlightingDisabled" && p.r.th.syntax != nil {
		return !*p.r.th.syntax, true
	}
	return p.Settings.Claude(key)
}

func (p previewSettings) ClaudeScope(scope string) map[string]any {
	if s, ok := p.Settings.(ext.ScopedSettings); ok {
		return s.ClaudeScope(scope)
	}
	return nil
}

func (p previewSettings) ClaudeSources() []ext.SettingsSource {
	if s, ok := p.Settings.(ext.ScopedSettings); ok {
		return s.ClaudeSources()
	}
	return nil
}
