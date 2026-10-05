package settings

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/perms"
	"github.com/KaitouKid1412/mantle/features/settings/settingsfile"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogPermissions = "dialog.permissions"
	permissionsID     = "settings.permissions.panel"
)

// AutoModeRules mirrors the JSON "claude auto-mode defaults|config" prints.
type AutoModeRules struct {
	Allow       []string `json:"allow"`
	SoftDeny    []string `json:"soft_deny"`
	HardDeny    []string `json:"hard_deny"`
	Environment []string `json:"environment"`
}

func (r AutoModeRules) list(l perms.AutoList) []string {
	switch l {
	case perms.AutoAllow:
		return r.Allow
	case perms.AutoSoftDeny:
		return r.SoftDeny
	case perms.AutoHardDeny:
		return r.HardDeny
	}
	return r.Environment
}

const (
	permAllow = iota
	permAsk
	permDeny
	permDirs
	permAuto
)

var permTabs = []string{"Allow", "Ask", "Deny", "Workspace", "Auto mode"}

var permSubtitles = []string{
	"Claude uses these without asking.",
	"Claude always asks before using these, even in permissive modes.",
	"Claude never uses these.",
	"Directories Claude may read and edit besides the one it started in.",
	"Extra guidance for the auto-mode classifier, on top of its defaults.",
}

// writableScopes are offered when adding, most specific first.
var writableScopes = []struct {
	scope patch.Scope
	label string
}{
	{patch.Local, "This project, just me (.claude/settings.local.json)"},
	{patch.Project, "This project, shared (.claude/settings.json)"},
	{patch.User, "All my projects (~/.claude/settings.json)"},
}

func (a *area) setupPermissions(r ext.Registrar) error {
	r.AddDialog(dialogPermissions, a.newPermissions)
	r.AddCommand(ext.Command{
		Name: "permissions", Aliases: []string{"allowed-tools"}, Source: ext.SourceBuiltin,
		Description: "Manage allow, ask and deny rules and working directories",
		Run:         func(c ext.Ctx, _ string) tea.Cmd { return c.OpenDialog(dialogPermissions, nil) },
	})
	r.AddStory(ext.Story{ID: "settings.permissions/allow", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		p := &permPanel{a: storyArea(), loaded: true, view: perms.Merge(map[patch.Scope]map[string]any{
			patch.Policy: {"permissions": map[string]any{"deny": []any{"Bash(curl:*)"}}},
			patch.User:   {"permissions": map[string]any{"allow": []any{"Bash(git status)", "WebFetch(domain:example.com)"}}},
			patch.Local:  {"permissions": map[string]any{"allow": []any{"Bash(npm run test:*)", "mcp__github__*"}}},
		})}
		p.cursor = 1
		return p.View(c, ar)
	}})
	r.AddStory(ext.Story{ID: "settings.permissions/add-scope", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		p := &permPanel{a: storyArea(), loaded: true, step: permStepScope, draft: "Bash(make test)"}
		return p.View(c, ar)
	}})
	return nil
}

type permStep int

const (
	permStepBrowse permStep = iota
	permStepAutoList
	permStepInput
	permStepScope
	permStepDelete
)

// permItem is one row of the current tab.
type permItem struct {
	label, detail string
	header        bool
	add           bool
	readOnly      bool
	scope         patch.Scope
	raw           string // rule, directory or auto-mode text as stored
	autoList      perms.AutoList
}

// permsLoadedMsg carries the settings documents read off the UI goroutine.
type permsLoadedMsg struct {
	docs map[patch.Scope]map[string]any
}

// autoDefaultsMsg carries the auto-mode defaults.
type autoDefaultsMsg struct {
	rules AutoModeRules
	err   error
}

type permPanel struct {
	a      *area
	tab    int
	cursor int
	loaded bool

	view    perms.View
	session map[perms.Behavior][]perms.Entry // rules only the engine knows (session, CLI flags)
	cwd     string

	step     permStep
	draft    string
	inputErr string
	choice   int // cursor in the scope or auto-list step
	autoList perms.AutoList
	target   permItem // row being deleted

	defaults    *AutoModeRules
	defaultsErr string
}

func (a *area) newPermissions(c ext.Ctx, _ any) (ext.Dialog, error) {
	return &permPanel{a: a}, nil
}

func (p *permPanel) ID() string               { return permissionsID }
func (p *permPanel) Placement() ext.Placement { return ext.PlaceInline }
func (p *permPanel) KeyContext() string       { return ext.ContextTabs }

func (p *permPanel) KeyContexts() []string {
	switch p.step {
	case permStepInput:
		return nil // every key goes to the text field
	case permStepScope, permStepAutoList:
		return []string{ext.ContextSelect}
	case permStepDelete:
		return []string{ext.ContextConfirmation}
	}
	return []string{ext.ContextTabs, ext.ContextSelect}
}

func (p *permPanel) Init(c ext.Ctx) tea.Cmd {
	cmds := []tea.Cmd{p.load(c)}
	if eng := c.Engine(""); eng != nil && eng.Supports(proto.SubListPermissionRules) {
		cmds = append(cmds, eng.Control(proto.SubListPermissionRules, proto.ListPermissionRulesRequest{}))
	}
	return tea.Batch(cmds...)
}

// load reads every scope's file fresh (the host's copy can lag right after a write),
// plus the inline flag settings the host knows.
func (p *permPanel) load(c ext.Ctx) tea.Cmd {
	env := p.a.env(c)
	var flag map[string]any
	if ss, ok := c.Settings().(ext.ScopedSettings); ok {
		flag = ss.ClaudeScope(ext.ScopeFlag)
	}
	return func() tea.Msg {
		docs := settingsfile.Docs(settingsfile.ReadAll(env))
		if flag != nil {
			docs[patch.Flag] = flag
		}
		return ext.AddressedMsg{To: permissionsID, Msg: permsLoadedMsg{docs: docs}}
	}
}

func (p *permPanel) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.AddressedMsg:
		if m.To == p.ID() {
			return p.Update(c, m.Msg)
		}
	case permsLoadedMsg:
		p.view = perms.Merge(m.docs)
		p.loaded = true
		c.Invalidate(p.ID())
	case autoDefaultsMsg:
		if m.err != nil {
			p.defaultsErr = m.err.Error()
		} else {
			r := m.rules
			p.defaults = &r
		}
		c.Invalidate(p.ID())
	case settingsWrittenMsg:
		if m.Err == nil {
			return p.load(c)
		}
	case ext.ControlResultMsg:
		if m.Subtype == proto.SubListPermissionRules && isMain(m.EngineID) && m.Err == nil {
			if v, err := perms.FromListResponse(m.Resp); err == nil {
				p.session = map[perms.Behavior][]perms.Entry{}
				for _, b := range perms.Behaviors {
					for _, e := range v.Rules(b) {
						if !isFileScope(e.Scope) {
							p.session[b] = append(p.session[b], e)
						}
					}
				}
				p.cwd = v.OriginalCwd
				c.Invalidate(p.ID())
			}
		}
	}
	return nil
}

func isFileScope(s patch.Scope) bool {
	switch s {
	case patch.User, patch.Project, patch.Local, patch.Flag, patch.Policy:
		return true
	}
	return false
}

func behaviorOf(tab int) perms.Behavior {
	return perms.Behaviors[tab]
}

// items lists the current tab's rows.
func (p *permPanel) items() []permItem {
	switch p.tab {
	case permAllow, permAsk, permDeny:
		b := behaviorOf(p.tab)
		out := []permItem{{label: "Add a new rule…", add: true}}
		for _, e := range p.view.Rules(b) {
			out = append(out, permItem{label: e.Raw, detail: e.Scope.Label() + " · " + e.Describe(),
				readOnly: e.ReadOnly, scope: e.Scope, raw: e.Raw})
		}
		for _, e := range p.session[b] {
			out = append(out, permItem{label: e.Raw, detail: string(e.Scope) + " · " + e.Describe(), readOnly: true, raw: e.Raw})
		}
		return out
	case permDirs:
		out := []permItem{{label: "Add a directory…", add: true}}
		if p.cwd != "" {
			out = append(out, permItem{label: p.cwd, detail: "where this session started", readOnly: true})
		}
		for _, d := range p.view.Directories {
			out = append(out, permItem{label: d.Path, detail: d.Scope.Label(), readOnly: d.ReadOnly, scope: d.Scope, raw: d.Path})
		}
		return out
	}
	out := []permItem{{label: "Add a rule…", add: true}}
	for _, l := range perms.AutoLists {
		entries := p.view.AutoMode[l]
		if len(entries) == 0 {
			continue
		}
		out = append(out, permItem{label: autoListName(l), header: true})
		for _, e := range entries {
			out = append(out, permItem{label: e.Text, detail: e.Scope.Label(), readOnly: e.ReadOnly,
				scope: e.Scope, raw: e.Text, autoList: l})
		}
	}
	if p.defaults != nil {
		for _, l := range perms.AutoLists {
			rules := p.defaults.list(l)
			if len(rules) == 0 {
				continue
			}
			out = append(out, permItem{label: "Built-in " + strings.ToLower(autoListName(l)), header: true})
			for _, s := range rules {
				out = append(out, permItem{label: s, detail: "default", readOnly: true, autoList: l})
			}
		}
	}
	return out
}

func autoListName(l perms.AutoList) string {
	switch l {
	case perms.AutoAllow:
		return "Allow"
	case perms.AutoSoftDeny:
		return "Ask first (soft deny)"
	case perms.AutoHardDeny:
		return "Never (hard deny)"
	}
	return "Environment"
}

func (p *permPanel) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch p.step {
	case permStepScope, permStepAutoList:
		return p.handleChoice(c, id)
	case permStepDelete:
		switch id {
		case ext.ActConfirmYes:
			return true, p.remove(c)
		case ext.ActConfirmNo:
			p.step = permStepBrowse
			c.Invalidate(p.ID())
			return true, nil
		}
		return false, nil
	}
	items := p.items()
	switch id {
	case ext.ActTabsNext:
		p.switchTab(c, 1)
		return true, p.maybeLoadDefaults(c)
	case ext.ActTabsPrevious:
		p.switchTab(c, -1)
		return true, p.maybeLoadDefaults(c)
	case ext.ActSelectNext:
		p.moveTo(items, p.cursor+1, 1)
	case ext.ActSelectPrevious:
		p.moveTo(items, p.cursor-1, -1)
	case ext.ActSelectFirst, ext.ActSelectPageUp:
		p.moveTo(items, 0, 1)
	case ext.ActSelectLast, ext.ActSelectPageDown:
		p.moveTo(items, len(items)-1, -1)
	case ext.ActSelectAccept:
		return true, p.activate(c, items)
	case ext.ActSelectCancel:
		return true, tea.Sequence(c.CloseDialog(dialogPermissions), resultLine(c, "Permissions closed"))
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

func (p *permPanel) switchTab(c ext.Ctx, d int) {
	p.tab = (p.tab + d + len(permTabs)) % len(permTabs)
	p.cursor = 0
	c.Invalidate(p.ID())
}

// moveTo puts the cursor on i, skipping headings in direction dir.
func (p *permPanel) moveTo(items []permItem, i, dir int) {
	for i >= 0 && i < len(items) && items[i].header {
		i += dir
	}
	if i >= 0 && i < len(items) {
		p.cursor = i
	}
}

func (p *permPanel) activate(c ext.Ctx, items []permItem) tea.Cmd {
	if p.cursor >= len(items) {
		return nil
	}
	it := items[p.cursor]
	switch {
	case it.add && p.tab == permAuto:
		p.step, p.choice = permStepAutoList, 0
	case it.add:
		p.step, p.draft, p.inputErr = permStepInput, "", ""
	case it.readOnly:
		return c.Notify(ext.Notice{Key: "settings.permissions", Source: "settings",
			Text: "This entry comes from " + sourceName(it) + " and can't be changed here."})
	default:
		p.step, p.target = permStepDelete, it
	}
	c.Invalidate(p.ID())
	return nil
}

func sourceName(it permItem) string {
	switch it.scope {
	case patch.Policy:
		return "managed settings"
	case patch.Flag:
		return "the command line"
	case "":
		return "this session"
	}
	return string(it.scope)
}

func (p *permPanel) handleChoice(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	n := len(writableScopes)
	if p.step == permStepAutoList {
		n = len(perms.AutoLists)
	}
	switch id {
	case ext.ActSelectNext:
		p.choice = min(p.choice+1, n-1)
	case ext.ActSelectPrevious:
		p.choice = max(p.choice-1, 0)
	case ext.ActSelectAccept:
		if p.step == permStepAutoList {
			p.autoList = perms.AutoLists[p.choice]
			p.step, p.draft, p.inputErr = permStepInput, "", ""
			break
		}
		return true, p.add(c, writableScopes[p.choice].scope)
	case ext.ActSelectCancel:
		p.step = permStepBrowse
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

func (p *permPanel) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.step != permStepInput {
		return false, nil
	}
	defer c.Invalidate(p.ID())
	switch k.String() {
	case "esc", "escape":
		p.step = permStepBrowse
	case "enter":
		p.draft = strings.TrimSpace(p.draft)
		if err := p.validate(); err != nil {
			p.inputErr = err.Error()
			return true, nil
		}
		p.step, p.choice = permStepScope, 0
	case "backspace":
		if r := []rune(p.draft); len(r) > 0 {
			p.draft = string(r[:len(r)-1])
		}
		p.inputErr = ""
	default:
		if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
			p.draft += k.Text
			p.inputErr = ""
		}
	}
	return true, nil
}

func (p *permPanel) HandlePaste(c ext.Ctx, m tea.PasteMsg) (bool, tea.Cmd) {
	if p.step != permStepInput {
		return false, nil
	}
	p.draft += strings.TrimSpace(strings.ReplaceAll(m.Content, "\n", " "))
	c.Invalidate(p.ID())
	return true, nil
}

func (p *permPanel) validate() error {
	if p.draft == "" {
		return fmt.Errorf("type something first")
	}
	if p.tab <= permDeny {
		_, err := perms.Parse(p.draft)
		return err
	}
	return nil
}

// add saves the drafted entry to scope.
func (p *permPanel) add(c ext.Ctx, scope patch.Scope) tea.Cmd {
	var pt patch.Patch
	var err error
	switch {
	case p.tab <= permDeny:
		pt, err = perms.AddRule(scope, behaviorOf(p.tab), p.draft)
	case p.tab == permDirs:
		pt, err = perms.AddDirectory(scope, p.draft)
	default:
		pt, err = perms.AddAutoModeRule(scope, p.autoList, p.draft)
	}
	p.step = permStepBrowse
	c.Invalidate(p.ID())
	if err != nil {
		return c.Notify(ext.Notice{Key: "settings.permissions", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	return tea.Batch(p.a.apply(c, []action.Effect{action.PatchEffect(pt)}),
		c.Notify(ext.Notice{Key: "settings.permissions", Level: ext.NoticeSuccess, Source: "settings",
			Text: fmt.Sprintf("Added %s to %s settings", p.draft, strings.ToLower(scope.Label()))}))
}

func (p *permPanel) remove(c ext.Ctx) tea.Cmd {
	it := p.target
	var pt patch.Patch
	var err error
	switch {
	case p.tab <= permDeny:
		pt, err = perms.RemoveRule(it.scope, behaviorOf(p.tab), it.raw)
	case p.tab == permDirs:
		pt, err = perms.RemoveDirectory(it.scope, it.raw)
	default:
		pt, err = perms.RemoveAutoModeRule(it.scope, it.autoList, it.raw)
	}
	p.step = permStepBrowse
	p.cursor = max(p.cursor-1, 0)
	c.Invalidate(p.ID())
	if err != nil {
		return c.Notify(ext.Notice{Key: "settings.permissions", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	return tea.Batch(p.a.apply(c, []action.Effect{action.PatchEffect(pt)}),
		c.Notify(ext.Notice{Key: "settings.permissions", Level: ext.NoticeSuccess, Source: "settings",
			Text: fmt.Sprintf("Removed %s from %s settings", it.raw, strings.ToLower(it.scope.Label()))}))
}

// maybeLoadDefaults fetches the auto-mode defaults the first time the tab opens.
func (p *permPanel) maybeLoadDefaults(c ext.Ctx) tea.Cmd {
	if p.tab != permAuto || p.defaults != nil || p.defaultsErr != "" {
		return nil
	}
	fetch := p.a.autoModeDefaults
	if fetch == nil {
		p.defaultsErr = "the built-in rules can't be listed yet"
		return nil
	}
	dir := c.Session().Cwd
	return func() tea.Msg {
		r, err := fetch(context.Background(), dir)
		return ext.AddressedMsg{To: permissionsID, Msg: autoDefaultsMsg{rules: r, err: err}}
	}
}

func (p *permPanel) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	inner := ar.Width - 4
	var tabs []string
	for i, name := range permTabs {
		if i == p.tab {
			tabs = append(tabs, t.Paint(theme.Suggestion, "["+name+"]"))
		} else {
			tabs = append(tabs, t.Paint(theme.Inactive, " "+name+" "))
		}
	}
	body := []string{strings.Join(tabs, " "), t.Paint(theme.Inactive, permSubtitles[p.tab]), ""}
	hint := hintLine("tab", "switch tabs", "↑/↓", "move", "enter", "add or remove", "esc", "close")
	switch p.step {
	case permStepInput:
		what := "Rule, e.g. Bash(npm run test:*), Read(./docs/**), WebFetch(domain:example.com), mcp__github"
		switch {
		case p.tab == permDirs:
			what = "Directory path"
		case p.tab == permAuto:
			what = autoListName(p.autoList) + " rule, in plain words"
		}
		body = append(body, what, "> "+p.draft+t.Paint(theme.Suggestion, "▏"))
		if p.inputErr != "" {
			body = append(body, t.Paint(theme.Error, p.inputErr))
		}
		hint = hintLine("enter", "continue", "esc", "cancel")
	case permStepScope:
		body = append(body, "Save "+lipgloss.NewStyle().Bold(true).Render(p.draft)+" where?", "")
		rows := make([]listRow, len(writableScopes))
		for i, s := range writableScopes {
			rows[i] = listRow{Label: s.label}
		}
		body = append(body, listLines(t, inner, rows, p.choice, 0)...)
		hint = hintLine("enter", "save", "esc", "back")
	case permStepAutoList:
		body = append(body, "Which list?", "")
		rows := make([]listRow, len(perms.AutoLists))
		for i, l := range perms.AutoLists {
			rows[i] = listRow{Label: autoListName(l)}
		}
		body = append(body, listLines(t, inner, rows, p.choice, 0)...)
		hint = hintLine("enter", "choose", "esc", "back")
	case permStepDelete:
		body = append(body, "Remove "+lipgloss.NewStyle().Bold(true).Render(p.target.raw)+" from "+
			strings.ToLower(p.target.scope.Label())+" settings?")
		hint = hintLine("enter", "remove", "esc", "keep it")
	default:
		body = append(body, p.listLines(t, inner, listHeight(ar.MaxHeight, termH, 10))...)
		if p.tab == permAuto && p.defaultsErr != "" {
			body = append(body, "", t.Paint(theme.Inactive, "Built-in rules: "+p.defaultsErr))
		}
	}
	return ext.Rendered{Text: frame(t, ar.Width, "Permissions", "", body, hint)}
}

func (p *permPanel) listLines(t *theme.Theme, width, maxRows int) []string {
	if !p.loaded {
		return []string{t.Paint(theme.Inactive, "Reading settings…")}
	}
	items := p.items()
	if p.cursor >= len(items) {
		p.cursor = len(items) - 1
	}
	start, end := window(len(items), p.cursor, maxRows)
	var out []string
	if start > 0 {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		it := items[i]
		if it.header {
			out = append(out, lipgloss.NewStyle().Bold(true).Render(it.label))
			continue
		}
		mark := "  "
		label := it.label
		switch {
		case i == p.cursor:
			mark = t.Paint(theme.Suggestion, cursorMark) + " "
			label = t.Paint(theme.Suggestion, label)
		case it.add:
			label = t.Paint(theme.Inactive, label)
		case it.readOnly:
			label = t.Paint(theme.Subtle, label)
		}
		line := mark + label
		if it.detail != "" {
			line += "  " + t.Paint(theme.Inactive, it.detail)
		}
		out = append(out, fit(line, width))
	}
	if end < len(items) {
		out = append(out, t.Paint(theme.Inactive, fmt.Sprintf("  ↓ %d more", len(items)-end)))
	}
	return out
}

var _ ext.ActionHandler = (*permPanel)(nil)
