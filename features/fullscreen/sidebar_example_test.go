package fullscreen

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// The example from docs/EXTENDING.md ("Sidebar panes in fullscreen"): a mod that adds a
// left sidebar. It uses only pkg/ext and pkg/theme, as mods must.

func notesPaneFeature() ext.Feature {
	return ext.Feature{
		ID: "mod.notes-pane", Order: ext.ModOrder,
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotSidebarL, &notesPane{},
				ext.SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}})
			return nil
		},
	}
}

type notesPane struct{ clicks int }

func (p *notesPane) ID() string           { return "mod.notes-pane.pane" }
func (p *notesPane) Init(ext.Ctx) tea.Cmd { return nil }

// Update gets mouse events for the pane (coordinates relative to the pane).
func (p *notesPane) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.MouseEvent); ok {
		if _, click := m.Msg.(tea.MouseClickMsg); click {
			p.clicks++
			ctx.Invalidate(p.ID())
		}
	}
	return nil
}

func (p *notesPane) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t := ctx.Theme()
	return ext.Rendered{Text: t.Paint(theme.Accent, "Notes") + "\n" +
		t.Paint(theme.Inactive, fmt.Sprintf("clicked %d times", p.clicks))}
}

func TestSidebarExample(t *testing.T) {
	st := &store{}
	host := app.NewHost([]ext.Feature{testFeature(st), fullscreenFeature(t), notesPaneFeature()},
		app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true, Layout: ext.Fullscreen,
		Settings: exttest.NewSettings(nil)})
	hs := testkit.New(t, root, testkit.WithSize(100, 20))
	hs.WaitForText("clicked 0 times", 5*time.Second)
	hs.SendMsg(addMsg{items: []*ext.Item{textItem("a1", ext.KeyAssistantText, "transcript text")}})
	hs.WaitForText("transcript text", 3*time.Second)

	// The pane is on the left; the transcript starts right of it.
	var paneRow, textRow string
	for _, l := range hs.ScreenLines() {
		if strings.Contains(l, "Notes") {
			paneRow = l
		}
		if strings.Contains(l, "transcript text") {
			textRow = l
		}
	}
	sw := app.SidebarWidth(100)
	if !strings.HasPrefix(paneRow, "Notes") || strings.Index(textRow, "transcript text") <= sw {
		t.Errorf("layout:\n%s", hs.Screen())
	}
	mouse(hs, tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	hs.WaitForText("clicked 1 times", 3*time.Second)
}
