package dialogs

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/gates"
)

// Choice is a single-choice confirmation dialog: the shape of the trust, bypass, API-key
// and auto-mode gates. Esc picks escID.
type Choice struct {
	title, kind string
	body        func(w int, st Styles) []string
	opts        optionList
	escID       string
	hint        string
	chosen      string
}

// Done reports whether an option was chosen.
func (c *Choice) Done() bool { return c.chosen != "" }

// Chosen returns the chosen option's ID ("" until Done).
func (c *Choice) Chosen() string { return c.chosen }

// HandleKey implements Model.
func (c *Choice) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if c.Done() {
		return false, None
	}
	a, digit := keyAct(k)
	if digit > 0 {
		if c.opts.pick(digit) {
			c.chosen = c.opts.selected().id
			return true, Answered
		}
		return true, None
	}
	return c.do(a)
}

func (c *Choice) do(a act) (bool, Effect) {
	switch a {
	case actUp:
		c.opts.move(-1)
	case actDown:
		c.opts.move(1)
	case actYes:
		c.chosen = c.opts.selected().id
		return true, Answered
	case actNo:
		c.chosen = c.escID
		return true, Answered
	default:
		return false, None
	}
	return true, None
}

// Action implements Model.
func (c *Choice) Action(id string) (bool, Effect) {
	if c.Done() {
		return false, None
	}
	return c.do(actionAct(id))
}

// HandlePaste implements Model.
func (c *Choice) HandlePaste(string) (bool, Effect) { return false, None }

// View implements Model.
func (c *Choice) View(width int, st Styles) string {
	w := bodyWidth(width)
	body := c.body(w, st)
	body = append(body, "")
	body = append(body, c.opts.lines(w, st, true)...)
	if c.hint != "" {
		body = append(body, styleLines(st.Dim, wrap(c.hint, w))...)
	}
	return frame(c.title, body, width, c.kind, st)
}

// Gate dialog option IDs.
const (
	ChoiceYes = "yes"
	ChoiceNo  = "no"
)

// maxListed caps each section of the trust report.
const maxListed = 5

func listSection(label string, items []string, w int, st Styles) []string {
	if len(items) == 0 {
		return nil
	}
	out := styleLines(st.Text, wrap(label, w))
	shown := items
	if len(items) > maxListed {
		shown = items[:maxListed-1]
	}
	for _, it := range shown {
		out = append(out, styleLines(st.Code, wrapIndent("  • ", SanitizeLine(it), w))...)
	}
	if len(shown) < len(items) {
		out = append(out, render(st.Dim, "  … and "+itoa(len(items)-len(shown))+" more"))
	}
	return out
}

// NewTrust builds the workspace-trust dialog from the gate's report. ChoiceYes trusts
// the folder; ChoiceNo (also esc) exits.
func NewTrust(r gates.TrustReport, homeDir bool) *Choice {
	var rules, hooks, env, helpers, dirs, servers []string
	for _, v := range r.AllowRules {
		rules = append(rules, v.Value)
	}
	for _, h := range r.Hooks {
		ev := h.Event
		if h.Matcher != "" {
			ev += " (" + h.Matcher + ")"
		}
		hooks = append(hooks, ev+": "+h.Detail)
	}
	for _, v := range r.Env {
		env = append(env, v.Value)
	}
	for _, v := range r.Helpers {
		helpers = append(helpers, v.Detail+": "+v.Value)
	}
	for _, v := range r.AdditionalDirs {
		dirs = append(dirs, v.Value)
	}
	for _, s := range r.McpServers {
		servers = append(servers, mcpSummary(s))
	}
	body := func(w int, st Styles) []string {
		out := styleLines(st.Code, wrap(SanitizeLine(r.Dir), w))
		out = append(out, "")
		out = append(out, wrap("Claude Code will be able to read, change and run files here. "+
			"Only continue if you trust the people who wrote this folder's code and settings.", w)...)
		var sections []string
		sections = append(sections, listSection("Allowed without asking:", rules, w, st)...)
		sections = append(sections, listSection("Hooks that run automatically:", hooks, w, st)...)
		sections = append(sections, listSection("Commands run by settings:", helpers, w, st)...)
		sections = append(sections, listSection("Environment variables set:", env, w, st)...)
		sections = append(sections, listSection("Extra directories:", dirs, w, st)...)
		sections = append(sections, listSection("MCP servers:", servers, w, st)...)
		sections = append(sections, listSection("Project commands and agents:", r.Extras, w, st)...)
		if len(sections) > 0 {
			out = append(out, "")
			out = append(out, styleLines(st.Warning, wrap("This folder's configuration will take effect:", w))...)
			out = append(out, sections...)
		}
		if homeDir {
			out = append(out, "")
			out = append(out, styleLines(st.Dim, wrap("This is your home directory: trust lasts for this session only.", w))...)
		}
		return out
	}
	return &Choice{
		title: "Do you trust this folder?",
		kind:  "warning",
		body:  body,
		opts:  optionList{items: []option{{id: ChoiceYes, label: "Yes, I trust this folder"}, {id: ChoiceNo, label: "No, exit"}}},
		escID: ChoiceNo,
		hint:  "enter to confirm · esc to exit",
	}
}

func mcpSummary(s gates.McpServer) string {
	detail := s.Transport
	switch {
	case s.Command != "":
		detail += ": " + strings.TrimSpace(s.Command+" "+strings.Join(s.Args, " "))
	case s.URL != "":
		detail += ": " + s.URL
	}
	return s.Name + " (" + detail + ")"
}

// NewBypassWarning builds the bypass-permissions warning. ChoiceYes accepts; ChoiceNo
// (the default, also esc) exits.
func NewBypassWarning() *Choice {
	body := func(w int, st Styles) []string {
		out := styleLines(st.Error, wrap("Claude Code is starting in bypass permissions mode.", w))
		out = append(out, "")
		out = append(out, wrap("Every tool call runs without asking, including commands that change or "+
			"delete files and reach the network. Use this only in an isolated environment such as a "+
			"container or VM without credentials you care about. You are responsible for what runs.", w)...)
		return out
	}
	return &Choice{
		title: "Bypass permissions mode",
		kind:  "error",
		body:  body,
		opts:  optionList{items: []option{{id: ChoiceNo, label: "No, exit"}, {id: ChoiceYes, label: "Yes, I accept"}}},
		escID: ChoiceNo,
		hint:  "enter to confirm · esc to exit",
	}
}

// NewAPIKeyPrompt asks whether to use ANTHROPIC_API_KEY. suffix is the key's last 20
// characters (the rest is never shown). ChoiceNo is the default and esc.
func NewAPIKeyPrompt(suffix string) *Choice {
	body := func(w int, st Styles) []string {
		out := wrap("ANTHROPIC_API_KEY is set in your environment:", w)
		out = append(out, styleLines(st.Code, wrap("  sk-ant-…"+SanitizeLine(suffix), w))...)
		out = append(out, "")
		out = append(out, wrap("Should Claude Code use this key? If not, it uses your Claude login.", w)...)
		return out
	}
	return &Choice{
		title: "API key detected",
		kind:  "warning",
		body:  body,
		opts:  optionList{items: []option{{id: ChoiceYes, label: "Yes, use this key"}, {id: ChoiceNo, label: "No, don't use it"}}, cursor: 1},
		escID: ChoiceNo,
	}
}

// McpApproval asks which new .mcp.json servers may run. One server gets three options
// (all future servers / this one / none); several get a checklist.
type McpApproval struct {
	servers   []gates.McpServer
	opts      optionList // single-server mode
	checked   []bool
	cursor    int
	done      bool
	approved  map[string]bool
	enableAll bool
}

// McpApproval option IDs (single-server mode).
const (
	McpChoiceAll  = "all"
	McpChoiceOne  = "one"
	McpChoiceNone = "none"
)

// NewMcpApproval builds the dialog for the pending servers.
func NewMcpApproval(servers []gates.McpServer) *McpApproval {
	m := &McpApproval{servers: servers, approved: map[string]bool{}}
	if len(servers) == 1 {
		m.opts = optionList{items: []option{
			{id: McpChoiceAll, label: "Use this and all future MCP servers in this project"},
			{id: McpChoiceOne, label: "Use this MCP server"},
			{id: McpChoiceNone, label: "Continue without this MCP server"},
		}}
	} else {
		m.checked = make([]bool, len(servers))
		for i := range m.checked {
			m.checked[i] = true
		}
	}
	return m
}

// Done reports whether the user answered.
func (m *McpApproval) Done() bool { return m.done }

// Approved maps each server name to the user's choice.
func (m *McpApproval) Approved() map[string]bool { return m.approved }

// EnableAll is true when the user chose to approve all future servers of the project
// (enableAllProjectMcpServers).
func (m *McpApproval) EnableAll() bool { return m.enableAll }

func (m *McpApproval) finish(choice string) (bool, Effect) {
	m.done = true
	for i, s := range m.servers {
		switch {
		case m.checked != nil:
			m.approved[s.Name] = m.checked[i] && choice != McpChoiceNone
		default:
			m.approved[s.Name] = choice == McpChoiceAll || choice == McpChoiceOne
		}
	}
	m.enableAll = choice == McpChoiceAll
	return true, Answered
}

// HandleKey implements Model.
func (m *McpApproval) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	if m.done {
		return false, None
	}
	a, digit := keyAct(k)
	if m.checked == nil {
		if digit > 0 {
			if m.opts.pick(digit) {
				return m.finish(m.opts.selected().id)
			}
			return true, None
		}
	} else if digit > 0 && digit <= len(m.checked) {
		m.cursor = digit - 1
		m.checked[m.cursor] = !m.checked[m.cursor]
		return true, None
	}
	return m.do(a)
}

func (m *McpApproval) do(a act) (bool, Effect) {
	switch a {
	case actUp:
		if m.checked == nil {
			m.opts.move(-1)
		} else {
			m.cursor = max(0, m.cursor-1)
		}
	case actDown:
		if m.checked == nil {
			m.opts.move(1)
		} else {
			m.cursor = min(len(m.checked)-1, m.cursor+1)
		}
	case actToggle:
		if m.checked == nil {
			return false, None
		}
		m.checked[m.cursor] = !m.checked[m.cursor]
	case actYes:
		if m.checked == nil {
			return m.finish(m.opts.selected().id)
		}
		return m.finish(McpChoiceOne)
	case actNo:
		return m.finish(McpChoiceNone)
	default:
		return false, None
	}
	return true, None
}

// Action implements Model.
func (m *McpApproval) Action(id string) (bool, Effect) {
	if m.done {
		return false, None
	}
	return m.do(actionAct(id))
}

// HandlePaste implements Model.
func (m *McpApproval) HandlePaste(string) (bool, Effect) { return false, None }

// View implements Model.
func (m *McpApproval) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	title := "New MCP servers in .mcp.json"
	if len(m.servers) == 1 {
		title = "New MCP server in .mcp.json"
		s := m.servers[0]
		body = append(body, styleLines(st.Title, wrap(SanitizeLine(s.Name), w))...)
		body = append(body, styleLines(st.Code, wrapIndent("  ", SanitizeLine(mcpSummary(s)), w))...)
		if len(s.EnvKeys) > 0 {
			body = append(body, styleLines(st.Dim, wrapIndent("  env: ", SanitizeLine(strings.Join(s.EnvKeys, ", ")), w))...)
		}
		body = append(body, "")
		body = append(body, wrap("MCP servers can run code and reach the network. Only use servers you trust.", w)...)
		body = append(body, "")
		body = append(body, m.opts.lines(w, st, true)...)
		body = append(body, render(st.Dim, "esc to continue without it"))
		return frame(title, body, width, "warning", st)
	}
	body = append(body, wrap("This project declares MCP servers. Choose the ones Claude Code may start; "+
		"they can run code and reach the network.", w)...)
	body = append(body, "")
	for i, s := range m.servers {
		pointer := "  "
		if i == m.cursor {
			pointer = "❯ "
		}
		box := "[ ] "
		if m.checked[i] {
			box = "[x] "
		}
		lines := wrapIndent(pointer+box, SanitizeLine(mcpSummary(s)), w)
		if i == m.cursor {
			lines = styleLines(st.Selected, lines)
		}
		body = append(body, lines...)
	}
	body = append(body, "")
	body = append(body, styleLines(st.Dim, wrap("space to toggle · enter to confirm · esc to use none", w))...)
	return frame(title, body, width, "warning", st)
}

// What a Notice was closed with.
const (
	NoticeOK      = "ok"
	NoticeDismiss = "dismiss"
)

// Notice is an acknowledgement dialog for engine text, in Claude Code's shape: the title,
// the paragraphs (sanitized, wrapped, a blank line apart) and a key hint, with no option
// list. Enter closes it with NoticeOK, esc with NoticeDismiss.
type Notice struct {
	title, hint string
	paragraphs  []string
	chosen      string
}

// NewNotice builds a Notice; hint is the key line under the text.
func NewNotice(title string, paragraphs []string, hint string) *Notice {
	return &Notice{title: SanitizeLine(title), paragraphs: paragraphs, hint: hint}
}

// Done reports whether the notice was closed.
func (n *Notice) Done() bool { return n.chosen != "" }

// Chosen returns NoticeOK or NoticeDismiss ("" until Done).
func (n *Notice) Chosen() string { return n.chosen }

// HandleKey implements Model.
func (n *Notice) HandleKey(k tea.KeyPressMsg) (bool, Effect) {
	a, _ := keyAct(k)
	return n.do(a)
}

// Action implements Model.
func (n *Notice) Action(id string) (bool, Effect) { return n.do(actionAct(id)) }

func (n *Notice) do(a act) (bool, Effect) {
	if n.Done() {
		return false, None
	}
	switch a {
	case actYes:
		n.chosen = NoticeOK
	case actNo:
		n.chosen = NoticeDismiss
	default:
		return false, None
	}
	return true, Answered
}

// HandlePaste implements Model.
func (n *Notice) HandlePaste(string) (bool, Effect) { return false, None }

// View implements Model.
func (n *Notice) View(width int, st Styles) string {
	w := bodyWidth(width)
	var body []string
	for i, p := range n.paragraphs {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, wrap(Sanitize(p), w)...)
	}
	body = append(body, "")
	body = append(body, styleLines(st.Dim, wrap(n.hint, w))...)
	return frame(n.title, body, width, "permission", st)
}
