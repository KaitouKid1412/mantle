package settings

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	helpm "github.com/KaitouKid1412/mantle/features/settings/help"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogHelp = "dialog.help"
	helpID     = "settings.help"

	// HelpTabShortcuts opens the help dialog on the shortcuts tab:
	// ctx.OpenDialog("dialog.help", "shortcuts").
	HelpTabShortcuts = "shortcuts"
)

func (a *area) setupHelp(r ext.Registrar) error {
	r.AddDialog(dialogHelp, a.newHelp)
	r.AddCommand(ext.Command{
		Name: "help", Description: "List commands and keyboard shortcuts",
		Source: ext.SourceBuiltin,
		Run:    func(c ext.Ctx, args string) tea.Cmd { return c.OpenDialog(dialogHelp, strings.TrimSpace(args)) },
	})
	r.AddAction(ext.Action{
		ID: ext.ActAppHelp, Context: ext.ContextGlobal, Description: "Open help",
		Run: func(c ext.Ctx) (bool, tea.Cmd) { return true, c.OpenDialog(dialogHelp, nil) },
	})
	r.AddStory(ext.Story{ID: "settings.help/commands", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		h, _ := storyHelpArea().newHelp(c, nil)
		return h.View(c, ar)
	}})
	r.AddStory(ext.Story{ID: "settings.help/shortcuts-search", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		d, _ := storyHelpArea().newHelp(c, HelpTabShortcuts)
		h := d.(*helpDialog)
		h.query = "model"
		return h.View(c, ar)
	}})
	return nil
}

func storyHelpArea() *area {
	a := storyArea()
	a.engine.commands = []proto.SlashCommand{
		{Name: "compact", Description: "Summarise the conversation to free context", ArgumentHint: "[instructions]", Builtin: true},
		{Name: "context", Description: "Show context usage", Builtin: true},
		{Name: "review-pr", Description: "Review a pull request", ArgumentHint: "<number>"},
		{Name: "mcp__github__triage", Description: "Triage issues"},
	}
	return a
}

type helpDialog struct {
	a         *area
	tab       int // 0 commands, 1 shortcuts
	query     string
	searching bool
	offset    int
}

var helpTabs = []string{"Commands", "Shortcuts"}

func (a *area) newHelp(_ ext.Ctx, args any) (ext.Dialog, error) {
	h := &helpDialog{a: a}
	if s, _ := args.(string); s == HelpTabShortcuts {
		h.tab = 1
	}
	return h, nil
}

func (h *helpDialog) ID() string                      { return helpID }
func (h *helpDialog) Placement() ext.Placement        { return ext.PlaceInline }
func (h *helpDialog) KeyContext() string              { return ext.ContextHelp }
func (h *helpDialog) Init(ext.Ctx) tea.Cmd            { return nil }
func (h *helpDialog) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (h *helpDialog) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	if !h.searching {
		return false, nil
	}
	h.setQuery(c, h.query+strings.ReplaceAll(p.Content, "\n", " "))
	return true, nil
}

func (h *helpDialog) KeyContexts() []string {
	if h.searching {
		return []string{ext.ContextHelp}
	}
	return []string{ext.ContextHelp, ext.ContextTabs, ext.ContextSelect}
}

func (h *helpDialog) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActHelpDismiss, ext.ActSelectCancel:
		if h.searching || h.query != "" {
			h.searching = false
			h.setQuery(c, "")
			return true, nil
		}
		return true, c.CloseDialog(dialogHelp)
	case ext.ActTabsNext, ext.ActTabsPrevious:
		h.tab = 1 - h.tab
		h.offset = 0
	case ext.ActSelectNext:
		h.offset++
	case ext.ActSelectPrevious:
		h.offset--
	case ext.ActSelectPageDown:
		h.offset += 10
	case ext.ActSelectPageUp:
		h.offset -= 10
	case ext.ActSelectFirst:
		h.offset = 0
	case ext.ActSelectLast:
		h.offset = 1 << 20
	case ext.ActSelectAccept:
		// Nothing to choose; enter just keeps the dialog open.
	default:
		return false, nil
	}
	c.Invalidate(h.ID())
	return true, nil
}

func (h *helpDialog) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if !h.searching {
		if k.String() == "/" {
			h.searching = true
			c.Invalidate(h.ID())
			return true, nil
		}
		return false, nil
	}
	switch k.String() {
	case "enter", "esc", "escape":
		h.searching = false
		c.Invalidate(h.ID())
		return true, nil
	case "backspace":
		if r := []rune(h.query); len(r) > 0 {
			h.setQuery(c, string(r[:len(r)-1]))
		}
		return true, nil
	}
	if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
		h.setQuery(c, h.query+k.Text)
		return true, nil
	}
	return true, nil // swallow everything else while typing
}

func (h *helpDialog) setQuery(c ext.Ctx, q string) {
	h.query = q
	h.offset = 0
	c.Invalidate(h.ID())
}

// commandLines lists commands grouped by source, filtered by the query.
func (h *helpDialog) commandLines(c ext.Ctx, t *theme.Theme, width int) []string {
	known := helpm.Known{Skills: map[string]bool{}, Plugins: map[string]bool{}}
	if s := h.a.engine.sys; s != nil {
		for _, sk := range s.Skills {
			known.Skills[sk] = true
		}
		for _, p := range s.Plugins {
			known.Plugins[p.Name] = true
		}
	}
	var native []helpm.Command
	for _, cmd := range c.Commands() {
		hc := helpm.Command{Name: cmd.Name, Description: cmd.Description, ArgHint: cmd.ArgHint, Aliases: cmd.Aliases, Hidden: cmd.Hidden}
		switch cmd.Source {
		case ext.SourceMod:
			hc.Source = helpm.Mod
		case ext.SourceEngine:
			hc.Source = helpm.Classify(proto.SlashCommand{Name: cmd.Name}, known)
		default:
			hc.Source = helpm.Native
		}
		native = append(native, hc)
	}
	cmds := helpm.Filter(helpm.Merge(native, h.a.engine.commands, known), h.query)
	var out []string
	usageW := 0
	for _, cmd := range cmds {
		if w := ansi.StringWidth(cmd.Usage()); w > usageW {
			usageW = w
		}
	}
	if usageW > width/2 {
		usageW = width / 2
	}
	for _, g := range helpm.Groups(cmds) {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lipgloss.NewStyle().Bold(true).Render(g.Title))
		for _, cmd := range g.Commands {
			u := fit(cmd.Usage(), usageW)
			out = append(out, "  "+u+strings.Repeat(" ", usageW-ansi.StringWidth(u))+"  "+t.Paint(theme.Inactive, cmd.Description))
		}
	}
	return out
}

// shortcutLines lists every default action with the keys currently bound to it.
func (h *helpDialog) shortcutLines(c ext.Ctx, t *theme.Theme, width int) []string {
	type pair struct{ ctx, action string }
	seen := map[pair]bool{}
	var bs []helpm.Binding
	for _, b := range ext.DefaultBindings {
		p := pair{b.Context, string(b.Action)}
		if seen[p] {
			continue
		}
		seen[p] = true
		for _, k := range c.KeysFor(b.Context, b.Action) {
			bs = append(bs, helpm.Binding{Context: b.Context, Keys: k, Action: string(b.Action),
				Description: helpm.ActionDescription(string(b.Action))})
		}
	}
	groups := helpm.FilterKeys(helpm.KeyGroups(bs), h.query)
	keyW := width / 3
	var out []string
	for _, g := range groups {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, lipgloss.NewStyle().Bold(true).Render(g.Context))
		for _, l := range g.Lines {
			k := fit(strings.Join(l.Keys, ", "), keyW)
			out = append(out, "  "+t.Paint(theme.Suggestion, k)+strings.Repeat(" ", keyW-ansi.StringWidth(k))+"  "+l.Description)
		}
	}
	return out
}

func (h *helpDialog) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	inner := ar.Width - 4
	var tabs []string
	for i, name := range helpTabs {
		if i == h.tab {
			tabs = append(tabs, t.Paint(theme.Suggestion, "["+name+"]"))
		} else {
			tabs = append(tabs, t.Paint(theme.Inactive, " "+name+" "))
		}
	}
	head := strings.Join(tabs, " ")
	switch {
	case h.searching:
		head += "   " + "Search: " + h.query + t.Paint(theme.Suggestion, "▏")
	case h.query != "":
		head += "   " + t.Paint(theme.Inactive, "Search: "+h.query)
	}
	var lines []string
	if h.tab == 0 {
		lines = h.commandLines(c, t, inner)
	} else {
		lines = h.shortcutLines(c, t, inner)
	}
	if len(lines) == 0 {
		lines = []string{t.Paint(theme.Inactive, "Nothing matches “"+h.query+"”.")}
	}
	rows := listHeight(ar.MaxHeight, termH, 9)
	if h.offset > len(lines)-rows {
		h.offset = len(lines) - rows
	}
	if h.offset < 0 {
		h.offset = 0
	}
	end := h.offset + rows
	if end > len(lines) {
		end = len(lines)
	}
	body := append([]string{head, ""}, lines[h.offset:end]...)
	if more := len(lines) - end; more > 0 {
		body = append(body, t.Paint(theme.Inactive, fmt.Sprintf("↓ %d more", more)))
	}
	hint := hintLine("tab", "switch tabs", "/", "search", "↑/↓", "scroll",
		keyName(c.KeysFor(ext.ContextHelp, ext.ActHelpDismiss), "esc"), "close")
	if h.searching {
		hint = hintLine("enter", "keep the filter", "esc", "clear it")
	}
	return ext.Rendered{Text: frame(t, ar.Width, "Help", "", body, hint)}
}

var _ ext.ActionHandler = (*helpDialog)(nil)
var _ ext.ContextStack = (*helpDialog)(nil)
