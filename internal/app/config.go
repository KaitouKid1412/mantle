package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// configUpdate applies reloads from internal/config's watcher (and SetClaude writes):
// settings snapshots, keybinding files and custom themes.
func (r *Root) configUpdate(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case config.ReloadMsg:
		store, ok := r.configStore()
		if !ok {
			return nil, true
		}
		changed := store.Apply(m)
		if len(changed) == 0 {
			return nil, true
		}
		return ext.Msg(ext.SettingsMsg{Changed: changed}), true
	case config.KeybindingsMsg:
		r.SetKeymapSources(m.Sources)
		var cmds []tea.Cmd
		for _, i := range r.resolver.Keymap().Issues {
			if i.Severity == "error" && i.Source != "defaults" && i.Source != "features" {
				cmds = append(cmds, r.addNotice(ext.Notice{Key: "keybindings", Text: "keybindings: " + i.Message + " (" + i.Keys + ")", Level: ext.NoticeWarning, Source: "core"}))
				break
			}
		}
		return tea.Batch(cmds...), true
	case config.ThemesMsg:
		cmds := []tea.Cmd{r.SetCustomThemes(config.ThemeMap(m.Themes))}
		if len(m.Errs) > 0 {
			cmds = append(cmds, r.addNotice(ext.Notice{Key: "themes", Text: "custom theme: " + m.Errs[0].Error(), Level: ext.NoticeWarning, Source: "core"}))
		}
		return tea.Batch(cmds...), true
	case config.WriteFailedMsg:
		return r.addNotice(ext.Notice{Key: "settings-write", Text: "Could not save " + m.Key + ": " + m.Err.Error(), Level: ext.NoticeError, Source: "core"}), true
	}
	return nil, false
}

// configStore returns the settings as a *config.Store when that is what the host runs
// with (tests may use other ext.Settings).
func (r *Root) configStore() (*config.Store, bool) {
	p, ok := r.opts.Settings.(previewSettings)
	if !ok {
		return nil, false
	}
	s, ok := p.Settings.(*config.Store)
	return s, ok
}
