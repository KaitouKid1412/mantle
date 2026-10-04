package ui

import (
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Stories returns a story per widget state, for goldens, `mantle-ui story` and the
// builder's previews. The host registers them under the "core.widgets" feature.
func Stories() []ext.Story {
	opts := []Item{
		{Label: "Yes"},
		{Label: "Yes, and don't ask again for this command", Description: "in this project"},
		{Label: "No, and tell Claude what to do differently", Description: "esc"},
	}
	return []ext.Story{
		{ID: "ui.select/numbered", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			s := NewSelect("story.select", opts...)
			s.Numbered = true
			s.SetCurrent(1)
			return s.View(c, a)
		}},
		{ID: "ui.select/filter", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			s := NewSelect("story.filter",
				Item{Label: "claude-opus-5-5", Description: "most capable"},
				Item{Label: "claude-sonnet-5-5", Description: "balanced"},
				Item{Label: "claude-haiku-4-5", Description: "fastest"},
				Item{Label: "default", Disabled: true})
			s.Filterable = true
			s.filter = "son"
			s.refilter()
			return s.View(c, a)
		}},
		{ID: "ui.select/scrolled", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			var items []Item
			for _, w := range strings.Fields("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu") {
				items = append(items, Item{Label: w})
			}
			s := NewSelect("story.scrolled", items...)
			s.MaxVisible = 5
			s.SetCurrent(7)
			return s.View(c, a)
		}},
		{ID: "ui.tabs/default", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			t := &Tabs{IDValue: "story.tabs", Titles: []string{"Config", "Status", "Usage", "Stats"}, Active: 1}
			return t.View(c, a)
		}},
		{ID: "ui.frame/permission", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			body := "Bash command\n\n  rm -rf build/\n  Remove the build directory\n\n" + Hints(c.Theme(), FrameInner(a.Width), Hint{"enter", "to confirm"}, Hint{"esc", "to cancel"})
			return ext.Rendered{Text: Frame(c.Theme(), theme.Permission, "Permission", body, a.Width)}
		}},
		{ID: "ui.textfield/value", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			f := &TextField{IDValue: "story.field", Prompt: "Search: "}
			f.SetValue("keybindings")
			return f.View(c, a)
		}},
		{ID: "ui.textfield/placeholder", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			f := &TextField{IDValue: "story.field2", Prompt: "> ", Placeholder: "Type a session name"}
			return f.View(c, a)
		}},
		{ID: "ui.scroll/status", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			var ls []string
			for i := range 30 {
				ls = append(ls, "line "+itoa(i+1))
			}
			p := &ScrollPane{IDValue: "story.scroll", Lines: ls, ShowStatus: true}
			a.MaxHeight = 6
			p.View(c, a)
			p.offset = 10
			return p.View(c, a)
		}},
		{ID: "ui.table/selectable", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			t := &Table{IDValue: "story.table", Selectable: true,
				Columns: []Column{{Title: "Server"}, {Title: "Status"}, {Title: "Tools", Right: true}},
				Rows:    [][]string{{"github", "connected", "42"}, {"linear", "needs auth", "0"}, {"filesystem", "failed", "0"}},
				cursor:  1}
			return t.View(c, a)
		}},
		{ID: "ui.hints/default", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			return ext.Rendered{Text: Hints(c.Theme(), a.Width, Hint{"↑↓", "to navigate"}, Hint{"enter", "to select"}, Hint{"esc", "to cancel"})}
		}},
		{ID: "ui.spinner/frame", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			s := &Spinner{IDValue: "story.spin"}
			t := c.Theme()
			return ext.Rendered{Text: s.Frame(c) + " " + Shimmer("Thinking…", 3, 3, t.Color(theme.Accent), t.Color(theme.AccentShimmer))}
		}},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
