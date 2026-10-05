package chrome

import (
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// StatusLineID is the status line component's ID.
const StatusLineID = "chrome.statusLine"

// slConfig is the statusLine setting.
type slConfig struct {
	Command              string
	Padding              int
	RefreshInterval      time.Duration
	HideVimModeIndicator bool
}

// statusLineConfig reads statusLine from the merged Claude Code settings. Only
// type "command" runs. disableAllHooks turns it off (Claude Code still runs a managed
// statusLine then; mantle can't tell scopes apart through the merged view).
func statusLineConfig(s ext.Settings) slConfig {
	return parseSLConfig(s, "statusLine")
}

// parseSLConfig applies Claude Code's hook gates: with disableAllHooks (outside managed
// settings) or managed allowManagedHooksOnly, only a statusLine from managed settings
// runs; disableAllHooks in managed settings turns it off entirely.
func parseSLConfig(s ext.Settings, key string) slConfig {
	v, ok := s.Claude(key)
	if ss, scoped := s.(ext.ScopedSettings); scoped {
		policy := ss.ClaudeScope(ext.ScopePolicy)
		managedOnly := policy["allowManagedHooksOnly"] == true || ext.ClaudeBool(s, "disableAllHooks", false)
		switch {
		case policy["disableAllHooks"] == true:
			return slConfig{}
		case managedOnly:
			v, ok = policy[key]
		}
	} else if ext.ClaudeBool(s, "disableAllHooks", false) {
		return slConfig{}
	}
	if !ok {
		return slConfig{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return slConfig{}
	}
	var c slConfig
	if t, _ := m["type"].(string); t != "" && t != "command" {
		return slConfig{}
	}
	c.Command, _ = m["command"].(string)
	c.Command = strings.TrimSpace(c.Command)
	c.Padding = int(number(m["padding"]))
	if secs := number(m["refreshInterval"]); secs > 0 {
		c.RefreshInterval = time.Duration(max(secs, 1) * float64(time.Second))
	}
	c.HideVimModeIndicator, _ = m["hideVimModeIndicator"].(bool)
	return c
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}

type slResultMsg struct {
	gen int
	res statusline.Result
}

type slTickMsg struct{ gen int }

// statusLineComp runs the user's statusLine command and shows its output in the footer
// area, above the mode line, as Claude Code does. The command first runs when the engine
// is ready (before the first turn, so without a session id), and again once the session
// id is known.
type statusLineComp struct {
	d       *slData
	cfg     slConfig
	runner  *statusline.Runner
	gen     int
	lines   []string
	notice  string
	dialogs int
	cols    int
	rows    int
	tickAt  int64 // the rate-limit reset the pending timer is for

	newRunner func(statusline.Config) *statusline.Runner // tests swap this
}

func newStatusLine() *statusLineComp {
	return &statusLineComp{d: newSLData(), newRunner: statusline.NewRunner}
}

func (c *statusLineComp) ID() string { return StatusLineID }

func (c *statusLineComp) Init(ctx ext.Ctx) tea.Cmd {
	c.cfg = statusLineConfig(ctx.Settings())
	c.d.configDir = claudeConfigDir(terminal.OS())
	c.cols, c.rows = ctx.Size()
	return nil
}

func (c *statusLineComp) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case slResultMsg:
		if m.gen != c.gen || c.runner == nil {
			return nil
		}
		c.lines, c.notice = m.res.Lines, m.res.Notice()
		ctx.Invalidate(StatusLineID)
		return waitResult(c.gen, c.runner.Results())
	case slTickMsg:
		c.tickAt = 0
		if m.gen == c.gen && c.d.dropExpired(ctx.Clock().Now()) {
			return c.push(ctx, true)
		}
		return nil
	case tea.WindowSizeMsg:
		c.cols, c.rows = m.Width, m.Height
		if c.runner != nil {
			c.runner.Resize(c.cols, c.rows)
		}
		return nil
	case ext.SettingsMsg:
		cfg := statusLineConfig(ctx.Settings())
		if cfg == c.cfg {
			return nil
		}
		commandChanged := cfg.Command != c.cfg.Command
		c.cfg = cfg
		c.stop()
		c.lines, c.notice = nil, ""
		ctx.Invalidate(StatusLineID)
		if c.d.s.SessionID == "" || cfg.Command == "" {
			return nil
		}
		cmd := c.start(ctx)
		if commandChanged {
			c.runner.Set(c.payload(ctx))
			c.runner.Trigger() // a new command skips the debounce
			return cmd
		}
		return tea.Batch(cmd, c.push(ctx, true))
	case prStatusMsg:
		pr, repo := statusLinePR(m)
		if samePRInfo(c.d.pr, pr) && sameRepo(c.d.repo, repo) {
			return nil
		}
		c.d.pr, c.d.repo = pr, repo
		return c.push(ctx, false)
	case ext.DialogOpenedMsg:
		c.dialogs++
		ctx.Invalidate(StatusLineID)
		return nil
	case ext.DialogClosedMsg:
		c.dialogs = max(0, c.dialogs-1)
		ctx.Invalidate(StatusLineID)
		return nil
	case ext.ControlResultMsg:
		if !isMain(m.EngineID) || m.Subtype != proto.SubInitialize || m.Err != nil ||
			c.cfg.Command == "" || c.runner != nil {
			return nil
		}
		set(&c.d.s.Cwd, ctx.Session().Cwd)
		cmd := c.start(ctx)
		return tea.Batch(cmd, c.push(ctx, true))
	}
	hadSession := c.d.s.SessionID != ""
	before := c.d.s
	changed, rerun := c.d.observe(ctx.Clock().Now(), msg)
	if before.Panel != c.d.s.Panel || before.Effort != c.d.s.Effort {
		ctx.Invalidate(StatusLineID)
	}
	if !changed || c.cfg.Command == "" {
		return nil
	}
	var cmds []tea.Cmd
	if c.runner == nil && c.d.s.SessionID != "" {
		cmds = append(cmds, c.start(ctx))
		rerun = true // the script runs once when a session starts
	}
	if !hadSession && c.d.s.SessionID != "" {
		rerun = true
	}
	if c.runner != nil {
		cmds = append(cmds, c.push(ctx, rerun))
	}
	return tea.Batch(cmds...)
}

// start creates the runner for the current config.
func (c *statusLineComp) start(ctx ext.Ctx) tea.Cmd {
	c.gen++
	c.runner = c.newRunner(statusline.Config{
		Command:         c.cfg.Command,
		Padding:         c.cfg.Padding,
		RefreshInterval: c.cfg.RefreshInterval,
		Dir:             c.d.s.Cwd,
	})
	c.runner.Resize(c.cols, c.rows)
	return waitResult(c.gen, c.runner.Results())
}

func (c *statusLineComp) stop() {
	if c.runner != nil {
		c.runner.Close()
		c.runner = nil
	}
}

// push sends the current payload to the runner, scheduling a run when rerun is set,
// and arms a timer for the next rate-limit reset.
func (c *statusLineComp) push(ctx ext.Ctx, rerun bool) tea.Cmd {
	if c.runner == nil {
		return nil
	}
	p := c.payload(ctx)
	if rerun {
		c.runner.Update(p)
	} else {
		c.runner.Set(p)
	}
	return c.resetTimer(ctx)
}

func (c *statusLineComp) payload(ctx ext.Ctx) []byte {
	data, err := statusline.Build(c.d.input(ctx.Clock().Now())).Marshal()
	if err != nil {
		ctx.Log().Warn("statusline payload", "err", err)
	}
	return data
}

// resetTimer arms one timer for the next rate-limit reset (re-armed only when that
// time changes), so the command re-runs when a window drops out of the payload.
func (c *statusLineComp) resetTimer(ctx ext.Ctx) tea.Cmd {
	next := int64(0)
	if r := c.d.rate; r != nil {
		for _, w := range []*statusline.RateWindow{r.FiveHour, r.SevenDay} {
			if w != nil && (next == 0 || w.ResetsAt < next) {
				next = w.ResetsAt
			}
		}
	}
	if next == 0 || next == c.tickAt {
		return nil
	}
	c.tickAt = next
	gen := c.gen
	d := time.Unix(next, 0).Sub(ctx.Clock().Now())
	return ctx.Clock().Tick(max(d, time.Second), func(time.Time) tea.Msg {
		return ext.AddressedMsg{To: StatusLineID, Msg: slTickMsg{gen}}
	})
}

func samePRInfo(a, b *statusline.PR) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameRepo(a, b *statusline.Repo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func waitResult(gen int, ch <-chan statusline.Result) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return nil
		}
		return ext.AddressedMsg{To: StatusLineID, Msg: slResultMsg{gen: gen, res: res}}
	}
}

func (c *statusLineComp) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	if c.cfg.Command == "" || c.dialogs > 0 || c.d.s.Panel || a.Width <= 0 {
		return ext.Rendered{}
	}
	text := renderStatusLines(ctx, c.lines, c.notice, a)
	if text != "" {
		// The footer's first row carries the effort hint; with a status line, that's
		// this one.
		first, rest, _ := strings.Cut(text, "\n")
		text = withRightHint(ctx, first, effortHint(ctx, c.d.s), a.Width)
		if rest != "" {
			text += "\n" + rest
		}
	}
	return ext.Rendered{Text: text}
}

// withRightHint right-aligns a dim hint after line when both fit in w.
func withRightHint(ctx ext.Ctx, line, hint string, w int) string {
	lw, hw := ansi.StringWidth(line), ansi.StringWidth(hint)
	if hint == "" || lw+2+hw > w {
		return line
	}
	if !ctx.Accessibility().ScreenReader {
		hint = ctx.Theme().Paint(theme.Inactive, hint)
	}
	return line + strings.Repeat(" ", w-lw-hw) + hint
}

func renderStatusLines(ctx ext.Ctx, lines []string, notice string, a ext.Area) string {
	if len(lines) == 0 && notice != "" {
		lines = []string{ctx.Theme().Paint(theme.Inactive, notice)}
	}
	if ctx.Accessibility().ScreenReader {
		plain := make([]string, len(lines))
		for i, l := range lines {
			plain[i] = ansi.Strip(l)
		}
		lines = plain
	}
	return strings.Join(truncateLines(lines, a.Width, a.MaxHeight), "\n")
}
