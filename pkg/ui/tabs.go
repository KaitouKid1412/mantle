package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Tabs is a row of tab titles (the /config panel's Config · Status · Usage · Stats).
// It handles tabs:next / tabs:previous in the "Tabs" context.
type Tabs struct {
	IDValue  string
	Titles   []string
	Active   int
	OnChange func(ext.Ctx, int) tea.Cmd
}

var _ ext.Focusable = (*Tabs)(nil)
var _ ext.ActionHandler = (*Tabs)(nil)

func (t *Tabs) ID() string                      { return t.IDValue }
func (t *Tabs) Init(ext.Ctx) tea.Cmd            { return nil }
func (t *Tabs) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (t *Tabs) KeyContext() string              { return ext.ContextTabs }
func (t *Tabs) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) {
	return false, nil
}
func (t *Tabs) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return false, nil }

// HandleAction cycles tabs (wrapping).
func (t *Tabs) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	if len(t.Titles) == 0 {
		return false, nil
	}
	switch a {
	case ext.ActTabsNext:
		return true, t.Set(c, (t.Active+1)%len(t.Titles))
	case ext.ActTabsPrevious:
		return true, t.Set(c, (t.Active-1+len(t.Titles))%len(t.Titles))
	}
	return false, nil
}

// Set activates tab i.
func (t *Tabs) Set(c ext.Ctx, i int) tea.Cmd {
	if i == t.Active || i < 0 || i >= len(t.Titles) {
		return nil
	}
	t.Active = i
	c.Invalidate(t.IDValue)
	if t.OnChange != nil {
		return t.OnChange(c, i)
	}
	return nil
}

// View renders the titles; the active one is highlighted.
func (t *Tabs) View(c ext.Ctx, a ext.Area) ext.Rendered {
	th := c.Theme()
	parts := make([]string, len(t.Titles))
	for i, title := range t.Titles {
		if i == t.Active {
			parts[i] = th.Bg(theme.Suggestion).Foreground(th.Color(theme.InverseText)).Bold(true).Render(" " + title + " ")
		} else {
			parts[i] = th.Paint(theme.Inactive, " "+title+" ")
		}
	}
	return ext.Rendered{Text: ansi.Truncate(strings.Join(parts, " "), a.Width, "…")}
}
