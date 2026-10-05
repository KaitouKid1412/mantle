package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/options"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogConfig = "dialog.config"
	configID     = "settings.config.panel"

	// mantleSection heads the rows generated from SettingSpecs.
	mantleSection = "mantle"
)

// Config panel tabs.
const (
	tabConfig = iota
	tabStatus
	tabUsage
	tabStats
)

var configTabs = []string{"Config", "Status", "Usage", "Stats"}

func (a *area) setupConfig(r ext.Registrar) error {
	r.AddDialog(dialogConfig, a.newConfigPanel)
	r.AddCommand(ext.Command{
		Name: "config", Aliases: []string{"settings"}, Description: "Open settings, or set one with key=value",
		ArgHint: "[key=value]", Source: ext.SourceBuiltin, Run: a.runConfigCommand,
		Complete: func(c ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, o := range options.Table {
				if o.Row != "" && strings.HasPrefix(strings.ToLower(o.Row), strings.ToLower(prefix)) {
					out = append(out, ext.Completion{Value: o.Row + "=", Display: o.Row, Description: o.Label})
				}
			}
			return out
		},
	})
	r.AddStory(ext.Story{ID: "settings.config/panel", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		d, _ := storyArea().newConfigPanel(c, nil)
		p := d.(*configPanel)
		p.cursor = 3
		return p.View(c, ar)
	}})
	return nil
}

// runConfigCommand opens the panel, or hands "key=value" to the engine, which knows
// every row it accepts (and writes ~/.claude.json itself where needed).
func (a *area) runConfigCommand(c ext.Ctx, args string) tea.Cmd {
	args = strings.TrimSpace(args)
	if _, ok := options.ParseAssignments(args); !ok {
		var tab any
		if strings.EqualFold(args, "status") {
			tab = tabStatus
		}
		return c.OpenDialog(dialogConfig, tab)
	}
	if eng := c.Engine(""); eng != nil {
		return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text("/config " + args)}})
	}
	return ext.Msg(noEngineMsg{What: "/config"})
}

// configRow is one settings row: a Claude Code option or a mantle SettingSpec.
type configRow struct {
	opt  options.Option
	spec *ext.SettingSpec
}

func (r configRow) section() string {
	if r.spec != nil {
		return mantleSection
	}
	return r.opt.Section
}

func (r configRow) label() string {
	if r.spec != nil {
		return r.spec.Key
	}
	return r.opt.Label
}

func (r configRow) description() string {
	if r.spec != nil {
		return r.spec.Description
	}
	return r.opt.Description
}

func (r configRow) key() string {
	if r.spec != nil {
		return "mantle:" + r.spec.Key
	}
	return r.opt.Key
}

// globalLoadedMsg carries ~/.claude.json (read-only) for keys stored there.
type globalLoadedMsg struct{ doc map[string]any }

type configPanel struct {
	a      *area
	tab    int
	rows   []configRow
	cursor int
	offset int

	query     string
	searching bool
	editing   bool // typing a text value
	draft     string

	global   map[string]any
	override map[string]any // values set in this panel, until settings reload
	status   *statusPanel
}

func (a *area) newConfigPanel(c ext.Ctx, args any) (ext.Dialog, error) {
	p := &configPanel{a: a, override: map[string]any{}, global: map[string]any{}}
	if t, ok := args.(int); ok && t >= 0 && t < len(configTabs) {
		p.tab = t
	}
	for _, o := range options.Table {
		if visible(c, o) {
			p.rows = append(p.rows, configRow{opt: o})
		}
	}
	for i := range a.mantleSpecs {
		p.rows = append(p.rows, configRow{spec: &a.mantleSpecs[i]})
	}
	d, _ := a.newStatusPanel(c, nil)
	p.status = d.(*statusPanel)
	return p, nil
}

func visible(c ext.Ctx, o options.Option) bool {
	switch o.When {
	case options.FullscreenOnly:
		return c.Layout() == ext.Fullscreen
	case options.IDEConnected:
		return false // the IDE bridge is not part of mantle yet
	case options.NotInIDE:
		return os.Getenv("TERM_PROGRAM") != "vscode"
	case options.InIDETerminal:
		return os.Getenv("TERM_PROGRAM") == "vscode"
	}
	return true
}

func (p *configPanel) ID() string               { return configID }
func (p *configPanel) Placement() ext.Placement { return ext.PlaceInline }
func (p *configPanel) KeyContext() string       { return ext.ContextSettings }

func (p *configPanel) KeyContexts() []string {
	if p.searching || p.editing {
		return []string{ext.ContextSettings}
	}
	return []string{ext.ContextSettings, ext.ContextTabs, ext.ContextSelect}
}

func (p *configPanel) Init(c ext.Ctx) tea.Cmd {
	env := p.a.env(c)
	return tea.Batch(p.status.Init(c), func() tea.Msg {
		doc := map[string]any{}
		if b, err := os.ReadFile(env.GlobalConfigPath()); err == nil {
			_ = json.Unmarshal(b, &doc)
		}
		return ext.AddressedMsg{To: configID, Msg: globalLoadedMsg{doc: doc}}
	})
}

func (p *configPanel) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.AddressedMsg:
		switch m.To {
		case configID:
			return p.Update(c, m.Msg)
		case statusID:
			return p.status.Update(c, m.Msg)
		}
	case globalLoadedMsg:
		p.global = m.doc
		c.Invalidate(p.ID())
	case ext.SettingsMsg:
		p.override = map[string]any{} // the files now say what we set
		c.Invalidate(p.ID())
	default:
		cmd := p.status.Update(c, msg)
		c.Invalidate(p.ID())
		return cmd
	}
	return nil
}

// visibleRows applies the search.
func (p *configPanel) visibleRows() []configRow {
	q := strings.ToLower(strings.TrimSpace(p.query))
	if q == "" {
		return p.rows
	}
	var out []configRow
	for _, r := range p.rows {
		hay := strings.ToLower(r.label() + " " + r.key() + " " + r.description() + " " + r.opt.Row)
		if strings.Contains(hay, q) {
			out = append(out, r)
		}
	}
	return out
}

func (p *configPanel) value(c ext.Ctx, r configRow) any {
	if v, ok := p.override[r.key()]; ok {
		return v
	}
	if r.spec != nil {
		return c.Settings().Mantle(r.spec.Key)
	}
	return r.opt.Value(settingsDoc(c, strings.Split(r.opt.Key, ".")[0]), p.global)
}

func (p *configPanel) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	if p.tab == tabStatus && id != ext.ActTabsNext && id != ext.ActTabsPrevious {
		if id == ext.ActConfirmNo || id == ext.ActSelectCancel {
			return true, tea.Sequence(c.CloseDialog(dialogConfig), resultLine(c, "Settings closed"))
		}
		return p.status.HandleAction(c, id)
	}
	rows := p.visibleRows()
	switch id {
	case ext.ActTabsNext:
		p.tab = (p.tab + 1) % len(configTabs)
	case ext.ActTabsPrevious:
		p.tab = (p.tab + len(configTabs) - 1) % len(configTabs)
	case ext.ActSelectNext:
		p.cursor = min(p.cursor+1, len(rows)-1)
	case ext.ActSelectPrevious:
		p.cursor = max(p.cursor-1, 0)
	case ext.ActSelectPageDown, ext.ActScrollHalfPageDown:
		p.cursor = min(p.cursor+10, len(rows)-1)
	case ext.ActSelectPageUp, ext.ActScrollHalfPageUp:
		p.cursor = max(p.cursor-10, 0)
	case ext.ActSelectFirst:
		p.cursor = 0
	case ext.ActSelectLast:
		p.cursor = len(rows) - 1
	case ext.ActSettingsSearch:
		p.searching = true
	case ext.ActSelectAccept:
		if p.tab == tabUsage || p.tab == tabStats {
			return true, p.openOther(c)
		}
		return true, p.activate(c)
	case ext.ActConfirmNo, ext.ActSelectCancel:
		if p.query != "" {
			p.query, p.cursor = "", 0
			break
		}
		return true, tea.Sequence(c.CloseDialog(dialogConfig), resultLine(c, "Settings closed"))
	default:
		return false, nil
	}
	p.cursor = max(p.cursor, 0)
	c.Invalidate(p.ID())
	return true, nil
}

// openOther runs /usage or /stats, which plan 06 provides.
func (p *configPanel) openOther(c ext.Ctx) tea.Cmd {
	name := "usage"
	if p.tab == tabStats {
		name = "stats"
	}
	cmd, ok := c.Command(name)
	if !ok || cmd.Run == nil {
		return c.Notify(ext.Notice{Key: "settings.config", Level: ext.NoticeWarning, Source: "settings",
			Text: "/" + name + " is not available yet."})
	}
	return tea.Sequence(c.CloseDialog(dialogConfig), cmd.Run(c, ""))
}

// activate toggles, cycles, edits or opens the panel for the highlighted row.
func (p *configPanel) activate(c ext.Ctx) tea.Cmd {
	rows := p.visibleRows()
	if p.cursor >= len(rows) {
		return nil
	}
	r := rows[p.cursor]
	if r.spec != nil {
		return p.activateSpec(c, *r.spec)
	}
	o := r.opt
	if o.Write == options.WritePanel {
		switch o.Panel {
		case "model":
			return c.OpenDialog(dialogModel, nil)
		case "effort":
			return c.OpenDialog(dialogEffort, nil)
		case "theme":
			return c.OpenDialog(dialogTheme, nil)
		case "outputStyle":
			return c.OpenDialog(dialogOutputStyle, nil)
		case "tui":
			return c.OpenDialog(dialogTUI, nil)
		case "fast":
			on := !p.a.fastOn(c)
			p.override[o.Key] = on
			return p.a.setFast(c, on)
		}
		return nil
	}
	if o.Type == options.Text {
		p.editing = true
		p.draft, _ = p.value(c, r).(string)
		return nil
	}
	return p.set(c, o, o.Next(p.value(c, r)))
}

func (p *configPanel) set(c ext.Ctx, o options.Option, v any) tea.Cmd {
	effects, err := options.Plan(o, v, true)
	if err != nil {
		return c.Notify(ext.Notice{Key: "settings.config", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	p.override[o.Key] = v
	return p.a.apply(c, effects)
}

func (p *configPanel) activateSpec(c ext.Ctx, s ext.SettingSpec) tea.Cmd {
	cur := c.Settings().Mantle(s.Key)
	if v, ok := p.override["mantle:"+s.Key]; ok {
		cur = v
	}
	var next any
	switch s.Type {
	case "bool":
		b, _ := cur.(bool)
		next = !b
	case "enum":
		next = s.Default
		for i, o := range s.Options {
			if o == cur {
				next = s.Options[(i+1)%len(s.Options)]
			}
		}
	case "string":
		p.editing = true
		p.draft, _ = cur.(string)
		return nil
	default:
		return c.Notify(ext.Notice{Key: "settings.config", Source: "settings",
			Text: s.Key + " is edited in ~/.mantle/settings.json."})
	}
	p.override["mantle:"+s.Key] = next
	return p.a.apply(c, []action.Effect{{Mantle: &action.Mantle{Key: s.Key, Value: next}}})
}

func (p *configPanel) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case p.editing:
		return true, p.editKey(c, k)
	case p.searching:
		switch k.String() {
		case "enter":
			p.searching = false
		case "esc", "escape":
			p.searching, p.query = false, ""
		case "backspace":
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
			}
		default:
			if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
				p.query += k.Text
			}
		}
		p.cursor = 0
		c.Invalidate(p.ID())
		return true, nil
	}
	return false, nil
}

func (p *configPanel) editKey(c ext.Ctx, k tea.KeyPressMsg) tea.Cmd {
	defer c.Invalidate(p.ID())
	switch k.String() {
	case "enter":
		p.editing = false
		r := p.visibleRows()[p.cursor]
		if r.spec != nil {
			p.override["mantle:"+r.spec.Key] = p.draft
			return p.a.apply(c, []action.Effect{{Mantle: &action.Mantle{Key: r.spec.Key, Value: p.draft}}})
		}
		v, err := r.opt.Parse(p.draft)
		if err != nil {
			return c.Notify(ext.Notice{Key: "settings.config", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
		}
		return p.set(c, r.opt, v)
	case "esc", "escape":
		p.editing = false
	case "backspace":
		if r := []rune(p.draft); len(r) > 0 {
			p.draft = string(r[:len(r)-1])
		}
	default:
		if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
			p.draft += k.Text
		}
	}
	return nil
}

func (p *configPanel) HandlePaste(c ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	switch {
	case p.editing:
		p.draft += strings.ReplaceAll(m.Content, "\n", " ")
	case p.searching:
		p.query += strings.ReplaceAll(m.Content, "\n", " ")
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

func formatValue(t *theme.Theme, v any, typ options.Type) string {
	switch b := v.(type) {
	case bool:
		if b {
			return t.Paint(theme.Success, "on")
		}
		return t.Paint(theme.Inactive, "off")
	case nil:
		return t.Paint(theme.Inactive, "default")
	case string:
		if b == "" {
			return t.Paint(theme.Inactive, "default")
		}
	}
	return fmt.Sprint(v)
}

func (p *configPanel) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	inner := ar.Width - 4
	var tabs []string
	for i, name := range configTabs {
		if i == p.tab {
			tabs = append(tabs, t.Paint(theme.Suggestion, "["+name+"]"))
		} else {
			tabs = append(tabs, t.Paint(theme.Inactive, " "+name+" "))
		}
	}
	head := strings.Join(tabs, " ")
	var body []string
	hint := ""
	switch p.tab {
	case tabStatus:
		lines := p.status.lines(c, t, inner)
		rows := listHeight(ar.MaxHeight, termH, 8)
		start := max(min(p.status.offset, len(lines)-rows), 0)
		body = append([]string{head, ""}, lines[start:min(start+rows, len(lines))]...)
		hint = hintLine("tab", "switch tabs", "↑/↓", "scroll", "r", "refresh MCP status", "esc", "close")
	case tabUsage, tabStats:
		name := "/usage"
		what := "Cost, plan limits and token use for this session."
		if p.tab == tabStats {
			name, what = "/stats", "Your activity across sessions."
		}
		body = []string{head, "", what, "", t.Paint(theme.Inactive, "Press enter to open "+name+".")}
		hint = hintLine("tab", "switch tabs", "enter", "open", "esc", "close")
	default:
		body = append([]string{head}, p.configLines(c, t, inner, listHeight(ar.MaxHeight, termH, 12))...)
		hint = hintLine("↑/↓", "move", "enter", "change", "/", "search", "tab", "switch tabs", "esc", "close")
		if p.searching {
			hint = hintLine("enter", "keep the filter", "esc", "clear it")
		}
		if p.editing {
			hint = hintLine("enter", "save", "esc", "cancel")
		}
	}
	return ext.Rendered{Text: frame(t, ar.Width, "Settings", "", body, hint)}
}

func (p *configPanel) configLines(c ext.Ctx, t *theme.Theme, width, maxRows int) []string {
	rows := p.visibleRows()
	var out []string
	switch {
	case p.searching:
		out = append(out, "Search: "+p.query+t.Paint(theme.Suggestion, "▏"))
	case p.query != "":
		out = append(out, t.Paint(theme.Inactive, "Search: "+p.query))
	default:
		out = append(out, "")
	}
	if len(rows) == 0 {
		return append(out, t.Paint(theme.Inactive, "No setting matches “"+p.query+"”."))
	}
	if p.cursor >= len(rows) {
		p.cursor = len(rows) - 1
	}
	// Each row is one line; sections get a heading line.
	type line struct {
		text string
		row  int // -1 for headings
	}
	var lines []line
	sec := ""
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, ansi.StringWidth(r.label()))
	}
	labelW = min(labelW, width/2)
	for i, r := range rows {
		if r.section() != sec {
			sec = r.section()
			lines = append(lines, line{lipgloss.NewStyle().Bold(true).Render(sec), -1})
		}
		mark := "  "
		label := fit(r.label(), labelW)
		label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
		if i == p.cursor {
			mark = t.Paint(theme.Suggestion, cursorMark) + " "
			label = t.Paint(theme.Suggestion, label)
		}
		val := formatValue(t, p.value(c, r), r.opt.Type)
		if i == p.cursor && p.editing {
			val = p.draft + t.Paint(theme.Suggestion, "▏")
		} else if r.opt.Write == options.WritePanel && r.spec == nil {
			val += t.Paint(theme.Inactive, " ›")
		}
		lines = append(lines, line{fit(mark+label+"  "+val, width), i})
	}
	// Scroll so the cursor's line is visible.
	cur := 0
	for i, l := range lines {
		if l.row == p.cursor {
			cur = i
		}
	}
	start, end := window(len(lines), cur, maxRows)
	if start > 0 {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("↑ %d more", start)))
	}
	for _, l := range lines[start:end] {
		out = append(out, l.text)
	}
	if end < len(lines) {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("↓ %d more", len(lines)-end)))
	}
	r := rows[p.cursor]
	desc := r.description()
	if r.spec == nil && r.opt.Store == options.StoreGlobal {
		desc += " Saved by Claude Code in its global config."
	}
	out = append(out, "")
	for _, l := range wrap(desc, width) {
		out = append(out, t.Paint(theme.Inactive, l))
	}
	return out
}

var _ ext.ActionHandler = (*configPanel)(nil)
