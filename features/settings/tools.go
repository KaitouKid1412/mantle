package settings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/editor"

	"github.com/KaitouKid1412/mantle/features/settings/options"
	"github.com/KaitouKid1412/mantle/features/settings/termsetup"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

const (
	dialogTerminalSetup = "dialog.terminalSetup"
	terminalSetupID     = "settings.terminalSetup.panel"
)

// claudeKeybindingsSkeleton is what /keybindings creates when the file is missing:
// Claude Code's file format with its schema and docs pointers and no bindings.
const claudeKeybindingsSkeleton = `{
  "$schema": "https://www.schemastore.org/claude-code-keybindings.json",
  "$docs": "https://code.claude.com/docs/en/keybindings",
  "bindings": []
}
`

// mantleKeybindingsSkeleton is the same shape for mantle:* actions.
const mantleKeybindingsSkeleton = `{
  "bindings": []
}
`

func (a *area) setupTools(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "keybindings", Description: "Edit your keyboard shortcuts file", ArgHint: "[mantle]",
		Source: ext.SourceBuiltin, Run: a.runKeybindings,
	})
	r.AddCommand(ext.Command{
		Name: "vim", Description: "Switch the prompt between standard and vim keys",
		Source: ext.SourceBuiltin, Run: func(c ext.Ctx, _ string) tea.Cmd { return a.toggleVim(c) },
	})
	r.AddDialog(dialogTerminalSetup, a.newTerminalSetup)
	r.AddCommand(ext.Command{
		Name: "terminal-setup", Description: "Set up Shift+Enter (or Option+Enter) for new lines in this terminal",
		Source: ext.SourceBuiltin, Run: func(c ext.Ctx, _ string) tea.Cmd { return c.OpenDialog(dialogTerminalSetup, nil) },
	})
	ext.Subscribe(r, "settings.keyboard-enhancements", func(c ext.Ctx, m tea.KeyboardEnhancementsMsg) tea.Cmd {
		a.kittyKeyboard = m.SupportsKeyDisambiguation()
		return nil
	})
	r.AddStory(ext.Story{ID: "settings.terminalSetup/edit", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
		p := &terminalSetup{a: storyArea(), loaded: true, prop: termsetup.Proposal{
			Terminal: termsetup.VSCode, Status: termsetup.Edit, Kind: termsetup.TargetFile,
			Path:    "~/Library/Application Support/Code/User/keybindings.json",
			Summary: "Add a Shift+Enter binding that sends a newline to the terminal.",
			Diff:    "--- keybindings.json\n+++ keybindings.json\n@@ -1,2 +1,8 @@\n [\n+  {\n+    \"key\": \"shift+enter\",\n+    \"command\": \"workbench.action.terminal.sendSequence\"\n+  }\n ]\n",
		}}
		return p.View(c, ar)
	}})
	return nil
}

// openEditor runs $EDITOR on path and reports back through done. Tests replace it.
func defaultOpenEditor(path string, done func(error) tea.Msg) tea.Cmd {
	cmd, err := editor.Cmd("mantle", path)
	if err != nil {
		return func() tea.Msg { return done(err) }
	}
	return tea.ExecProcess(cmd, done)
}

// editorDoneMsg reports that an editor launched by the settings area closed.
type editorDoneMsg struct {
	Path string
	Err  error
}

// ensureFile creates path with content unless it exists. It never overwrites.
func ensureFile(path, content string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return true, err
	}
	return true, f.Close()
}

// keybindingsReadyMsg says the file exists and the editor can start.
type keybindingsReadyMsg struct {
	Path    string
	Created bool
	Err     error
}

func (a *area) runKeybindings(c ext.Ctx, args string) tea.Cmd {
	env := a.env(c)
	path := filepath.Join(env.ClaudeDir(), "keybindings.json")
	skeleton := claudeKeybindingsSkeleton
	if strings.EqualFold(strings.TrimSpace(args), "mantle") {
		path = filepath.Join(env.Home, ".mantle", "keybindings.json")
		skeleton = mantleKeybindingsSkeleton
	}
	if err := env.CheckWritable(path); err != nil {
		return c.Notify(ext.Notice{Key: "settings.keybindings", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	return func() tea.Msg {
		created, err := ensureFile(path, skeleton)
		return keybindingsReadyMsg{Path: path, Created: created, Err: err}
	}
}

func (a *area) subscribeEditor(r ext.Registrar) {
	ext.Subscribe(r, "settings.keybindings-ready", func(c ext.Ctx, m keybindingsReadyMsg) tea.Cmd {
		if m.Err != nil {
			return c.Notify(ext.Notice{Key: "settings.keybindings", Level: ext.NoticeError, Source: "settings",
				Text: "Could not create " + m.Path + ": " + m.Err.Error()})
		}
		return a.openEditor(m.Path, func(err error) tea.Msg { return editorDoneMsg{Path: m.Path, Err: err} })
	})
	ext.Subscribe(r, "settings.editor-done", func(c ext.Ctx, m editorDoneMsg) tea.Cmd {
		if m.Err != nil {
			return c.Notify(ext.Notice{Key: "settings.editor", Level: ext.NoticeError, Source: "settings",
				Text: "Editor failed: " + m.Err.Error()})
		}
		if strings.HasSuffix(m.Path, "keybindings.json") {
			return c.Notify(ext.Notice{Key: "settings.editor", Source: "settings",
				Text: "Saved key bindings apply right away."})
		}
		return nil
	})
}

// toggleVim switches editorMode between normal and vim in user settings.
func (a *area) toggleVim(c ext.Ctx) tea.Cmd {
	o, err := options.Find("editorMode")
	if err != nil {
		return nil
	}
	next := "vim"
	if o.Value(settingsDoc(c, "editorMode"), nil) == "vim" {
		next = "normal"
	}
	effects, err := options.Plan(o, next, true)
	if err != nil {
		return c.Notify(ext.Notice{Key: "settings.vim", Level: ext.NoticeError, Source: "settings", Text: err.Error()})
	}
	text := "Vim keys on"
	if next == "normal" {
		text = "Vim keys off"
	}
	return tea.Batch(a.apply(c, effects),
		c.Notify(ext.Notice{Key: "settings.vim", Level: ext.NoticeSuccess, Source: "settings", Text: text}))
}

// ---- /terminal-setup ----

// defaultTermLoad detects the terminal and plans its change.
func defaultTermLoad(home string, kitty bool) (termsetup.Proposal, error) {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	d := termsetup.Detect(env, runtime.GOOS)
	in, err := termsetup.Applier{}.Load(d, termsetup.Env{Home: home, XDGConfigHome: env["XDG_CONFIG_HOME"], GOOS: runtime.GOOS}, kitty)
	if err != nil {
		return termsetup.Proposal{Terminal: d.Terminal}, err
	}
	return termsetup.Plan(in), nil
}

func defaultTermApply(home string, p termsetup.Proposal) (string, error) {
	return termsetup.Applier{BackupDir: filepath.Join(home, ".mantle", "backups", "terminal-setup")}.Apply(p)
}

type termLoadedMsg struct {
	prop termsetup.Proposal
	err  error
}

type termAppliedMsg struct {
	backup string
	err    error
}

type terminalSetup struct {
	a       *area
	loaded  bool
	prop    termsetup.Proposal
	loadErr error
	applied bool
	offset  int
}

func (a *area) newTerminalSetup(ext.Ctx, any) (ext.Dialog, error) { return &terminalSetup{a: a}, nil }

func (p *terminalSetup) ID() string               { return terminalSetupID }
func (p *terminalSetup) Placement() ext.Placement { return ext.PlaceInline }
func (p *terminalSetup) KeyContext() string       { return ext.ContextConfirmation }
func (p *terminalSetup) KeyContexts() []string {
	return []string{ext.ContextConfirmation, ext.ContextSelect}
}
func (p *terminalSetup) HandleKey(ext.Ctx, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
func (p *terminalSetup) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd)  { return false, nil }

func (p *terminalSetup) Init(c ext.Ctx) tea.Cmd {
	load, home, kitty := p.a.termLoad, p.a.env(c).Home, p.a.kittyKeyboard
	return func() tea.Msg {
		prop, err := load(home, kitty)
		return ext.AddressedMsg{To: terminalSetupID, Msg: termLoadedMsg{prop, err}}
	}
}

func (p *terminalSetup) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ext.AddressedMsg:
		if m.To == p.ID() {
			return p.Update(c, m.Msg)
		}
	case termLoadedMsg:
		p.prop, p.loadErr, p.loaded = m.prop, m.err, true
		c.Invalidate(p.ID())
	case termAppliedMsg:
		if m.err != nil {
			return tea.Sequence(c.CloseDialog(dialogTerminalSetup), c.Notify(ext.Notice{Key: "settings.terminalSetup",
				Level: ext.NoticeError, Source: "settings", Text: "Terminal setup failed: " + m.err.Error()}))
		}
		text := "Installed. Restart " + p.prop.Terminal.Name() + " (or open a new window) to use it."
		if m.backup != "" {
			text += " Backup: " + shortHome(m.backup, p.a.env(c).Home)
		}
		return tea.Sequence(c.CloseDialog(dialogTerminalSetup), c.Notify(ext.Notice{Key: "settings.terminalSetup",
			Level: ext.NoticeSuccess, Source: "settings", Text: text}))
	}
	return nil
}

func (p *terminalSetup) HandleAction(c ext.Ctx, id ext.ActionID) (bool, tea.Cmd) {
	switch id {
	case ext.ActConfirmYes, ext.ActSelectAccept:
		if !p.loaded || p.prop.Status != termsetup.Edit || p.loadErr != nil {
			return true, c.CloseDialog(dialogTerminalSetup)
		}
		if p.applied {
			return true, nil
		}
		p.applied = true
		apply, home, prop := p.a.termApply, p.a.env(c).Home, p.prop
		return true, func() tea.Msg {
			backup, err := apply(home, prop)
			return ext.AddressedMsg{To: terminalSetupID, Msg: termAppliedMsg{backup, err}}
		}
	case ext.ActConfirmNo, ext.ActSelectCancel:
		return true, c.CloseDialog(dialogTerminalSetup)
	case ext.ActConfirmNext, ext.ActSelectNext:
		p.offset++
	case ext.ActConfirmPrevious, ext.ActSelectPrevious:
		p.offset = max(p.offset-1, 0)
	default:
		return false, nil
	}
	c.Invalidate(p.ID())
	return true, nil
}

func (p *terminalSetup) View(c ext.Ctx, ar ext.Area) ext.Rendered {
	t := c.Theme()
	_, termH := c.Size()
	inner := ar.Width - 4
	if !p.loaded {
		return ext.Rendered{Text: frame(t, ar.Width, "Terminal setup", "", []string{t.Paint(theme.Inactive, "Looking at your terminal…")}, "")}
	}
	var body []string
	name := p.prop.Terminal.Name()
	if name == "" {
		name = "an unrecognised terminal"
	}
	body = append(body, "Terminal: "+name)
	if p.prop.Path != "" {
		body = append(body, t.Paint(theme.Inactive, "Config: "+shortHome(p.prop.Path, p.a.env(c).Home)))
	}
	body = append(body, "")
	if p.loadErr != nil {
		body = append(body, t.Paint(theme.Error, "Could not read the terminal's config: "+p.loadErr.Error()))
	} else {
		body = append(body, wrap(p.prop.Summary, inner)...)
	}
	for _, n := range p.prop.Notes {
		for _, l := range wrap("• "+n, inner) {
			body = append(body, t.Paint(theme.Inactive, l))
		}
	}
	hint := hintLine("enter", "close")
	if p.prop.Status == termsetup.Edit && p.loadErr == nil {
		body = append(body, "", "Proposed change:")
		diff := strings.Split(strings.TrimRight(p.prop.Diff, "\n"), "\n")
		rows := listHeight(ar.MaxHeight, termH, len(body)+6)
		start := min(p.offset, max(len(diff)-rows, 0))
		end := min(start+rows, len(diff))
		for _, l := range diff[start:end] {
			switch {
			case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
				l = t.Paint(theme.Success, l)
			case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
				l = t.Paint(theme.Error, l)
			default:
				l = t.Paint(theme.Inactive, l)
			}
			body = append(body, fit(l, inner))
		}
		if end < len(diff) {
			body = append(body, t.Paint(theme.Inactive, "↓ more"))
		}
		hint = hintLine("enter", "apply (a backup is kept)", "↑/↓", "scroll", "esc", "cancel")
	}
	return ext.Rendered{Text: frame(t, ar.Width, "Terminal setup", "", body, hint)}
}

var _ ext.ActionHandler = (*terminalSetup)(nil)
