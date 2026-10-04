# 12 → 10: EXTENDING.md section "Sidebar panes in fullscreen"

**Status:** open (2026-10-04). Plan 12 B9 asks for an EXTENDING.md example written with
plan 10: a mod that adds a left sidebar. The code below is tested in
`features/fullscreen/sidebar_example_test.go` (`TestSidebarExample`), which runs it in
the real host's fullscreen layout. Please paste the section into `docs/EXTENDING.md`
(wherever slots are described).

---

## Sidebar panes in fullscreen

In the fullscreen layout (`tui: "fullscreen"`) the host draws two extra slots beside the
transcript: `ext.SlotSidebarL` and `ext.SlotSidebarR`. A pane is an ordinary component
registered for `ext.Fullscreen` only, so inline mode never shows it. The host gives it
the full height between the header and the prompt, and delivers mouse events under it as
`ext.MouseEvent` (coordinates relative to the pane).

```go
package notespane

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

func init() {
	ext.Register(ext.Feature{
		ID: "mod.notes-pane", Order: ext.ModOrder,
		Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotSidebarL, &pane{},
				ext.SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}})
			return nil
		},
	})
}

type pane struct{ clicks int }

func (p *pane) ID() string           { return "mod.notes-pane.pane" }
func (p *pane) Init(ext.Ctx) tea.Cmd { return nil }

func (p *pane) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ext.MouseEvent); ok {
		if _, click := m.Msg.(tea.MouseClickMsg); click {
			p.clicks++
			ctx.Invalidate(p.ID())
		}
	}
	return nil
}

func (p *pane) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	t := ctx.Theme()
	return ext.Rendered{Text: t.Paint(theme.Accent, "Notes") + "\n" +
		t.Paint(theme.Inactive, fmt.Sprintf("clicked %d times", p.clicks))}
}
```

Notes:
- Width: the host decides (today `min(40, max(20, w/4))`; resizable widths are request
  12-01 to plan 01). Draw within `Area.Width` and at most `Area.MaxHeight` lines.
- Wheel events also arrive as `scroll:*` actions for the transcript; a pane that scrolls
  should handle `ext.MouseEvent` wheel messages itself and ignore the actions.
- Test a pane like any component: a Story, plus `app.NewHost` + `testkit.New` with
  `app.Options{Layout: ext.Fullscreen}` for an end-to-end check.
