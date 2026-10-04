package app

import (
	"io"
	"log/slog"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/keymap"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Options configure the root model.
type Options struct {
	Host     *Host
	Settings ext.Settings // nil = empty
	Clock    ext.Clock    // nil = real time
	Logger   *slog.Logger // nil = discard
	Theme    *theme.Theme // nil = theme.Default()
	A11y     ext.Accessibility
	Layout   ext.LayoutMode // Inline (default) or Fullscreen
	// KeymapSources are the user keybinding files (~/.claude, ~/.mantle), applied
	// after ext.DefaultBindings and features' AddBinding defaults.
	KeymapSources []keymap.Source
	// StateDir is ~/.mantle/state, for Ctx.Store ("" = in-memory stores).
	StateDir string
	// OnDisable is called when a feature is disabled after a panic (to persist it).
	OnDisable func(feature, reason string)
	// FrameInterval is the renderer's frame interval (default 1/60 s); used to wait
	// for a flush before ScreenClearedMsg and FirstFrameMsg.
	FrameInterval time.Duration
	// CustomThemes are themes from ~/.claude/themes, by name without "custom:".
	CustomThemes map[string]theme.Theme
	// Session is the main engine's initial session (resume target, cwd), set before
	// OnStart hooks run.
	Session ext.SessionInfo
	// Spawn starts engines; MainSpawn, when set, is spawned after OnStart (through
	// the startup gates when a feature subscribes to ext.SpawnGateMsg).
	Spawn     SpawnFunc
	MainSpawn *ext.SpawnOpts
	// Stop stops an engine and forgets it (ext.EngineStopMsg). It runs in a Cmd.
	Stop func(engineID string) error
	// NoBackgroundQuery skips asking the terminal for its background colour (tests).
	NoBackgroundQuery bool
}

// comp is a mounted component with its render cache.
type comp struct {
	e       *entry
	m       *mounted
	feature string
	valid   bool
	text    string
	lines   []string
	cursor  *tea.Cursor
	cw, ch  int // width and max height the cache was rendered for
}

type openDialog struct {
	id      string // registry ID
	feature string
	d       ext.Dialog
	c       *comp // render cache
}

// Root is mantle's tea.Model. It routes messages to features, lays out slots and
// owns the committer, dialog stack, keymap and notices.
type Root struct {
	opts Options
	host *Host
	ctx  *uiCtx

	w, h int

	comps   []*comp // mounted components, in inline slot order then weight
	byID    map[string]*comp
	focus   string
	dialogs []*openDialog

	subs         map[reflect.Type][]*entry
	interceptors []*entry
	stages       []*entry
	actions      map[ext.ActionID]*entry
	commands     map[string]*ext.Command // name and aliases → command (builtin/mod)
	runtimeCmds  map[string][]ext.Command
	dialogFacts  map[string]*entry

	resolver *keymap.Resolver
	ambient  map[string]bool

	engines    map[string]ext.Engine
	sessions   map[string]ext.SessionInfo
	transcript ext.Transcript
	theme      theme.Theme
	themes     map[string]theme.Theme
	stores     map[string]ext.KV

	notices  []*notice
	noticeID int

	printer printer
	heights []heightAt // recent frame heights, for print chunking
	th      themeState

	frameShown int // height of the last inline frame
	shrink     shrinkState

	exitCode   int
	exitReason string
	quitting   bool
	started    bool
	firstFrame bool
	interrupt  time.Time // last unhandled ctrl+c, for "press again to exit"
	logger     *slog.Logger
}

type heightAt struct {
	h  int
	at time.Time
}

// New builds the root model from a set-up host.
func New(o Options) *Root {
	if o.Host == nil {
		o.Host = NewHost(nil, HostOptions{})
	}
	if o.Clock == nil {
		o.Clock = realClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.FrameInterval == 0 {
		o.FrameInterval = time.Second / 60
	}
	r := &Root{
		opts:        o,
		host:        o.Host,
		w:           80,
		h:           24,
		ambient:     map[string]bool{},
		engines:     map[string]ext.Engine{},
		sessions:    map[string]ext.SessionInfo{},
		stores:      map[string]ext.KV{},
		runtimeCmds: map[string][]ext.Command{},
		logger:      o.Logger,
	}
	r.ctx = &uiCtx{r: r}
	if o.Settings == nil {
		o.Settings = emptySettings{}
	}
	r.opts.Settings = previewSettings{Settings: o.Settings, r: r}
	if o.Session.EngineID == "" {
		o.Session.EngineID = ext.MainEngine
	}
	r.sessions[ext.MainEngine] = o.Session
	r.themes = r.host.Themes()
	if o.Theme != nil {
		r.theme = *o.Theme
	} else if t, ok := r.lookupTheme(r.themeName()); ok {
		r.theme = t
	} else {
		r.theme = theme.Default()
	}
	r.rebuild()
	r.resolver = keymap.NewResolver(r.buildKeymap())
	r.pickFocus()
	return r
}

func (r *Root) log() *slog.Logger { return r.logger }

// Host returns the host.
func (r *Root) Host() *Host { return r.host }

// Ctx returns the UI context (for tests and the story runner).
func (r *Root) Ctx() ext.Ctx { return r.ctx }

// ExitCode is the code mantle-ui should exit with after the program returns.
func (r *Root) ExitCode() int { return r.exitCode }

// Keymap returns the resolved keymap (for catalog and /keybindings).
func (r *Root) Keymap() *keymap.Keymap { return r.resolver.Keymap() }

func (r *Root) buildKeymap() *keymap.Keymap {
	srcs := []keymap.Source{
		keymap.DefaultsSource("defaults", ext.DefaultBindings),
		keymap.DefaultsSource("features", r.host.Bindings()),
	}
	srcs = append(srcs, r.opts.KeymapSources...)
	return keymap.New(srcs...)
}

// SetKeymapSources swaps user keybinding files (hot reload).
func (r *Root) SetKeymapSources(srcs []keymap.Source) {
	r.opts.KeymapSources = srcs
	r.resolver.SetKeymap(r.buildKeymap())
	r.invalidateAll()
}

// rebuild recomputes the routing tables from the host's live entries (after setup or
// after a feature is disabled).
func (r *Root) rebuild() {
	old := r.byID
	r.byID = map[string]*comp{}
	r.comps = nil
	for _, e := range r.host.live(KindComponent) {
		m := e.value.(*mounted)
		c := old[m.comp.ID()]
		if c == nil || c.m != m {
			c = &comp{e: e, m: m, feature: e.feature}
		}
		r.comps = append(r.comps, c)
		r.byID[m.comp.ID()] = c
	}
	slotIndex := func(s ext.Slot) int {
		if i := slices.Index(ext.InlineSlots, s); i >= 0 {
			return i
		}
		return len(ext.InlineSlots) + 1
	}
	sort.SliceStable(r.comps, func(i, j int) bool {
		a, b := r.comps[i].m, r.comps[j].m
		if si, sj := slotIndex(a.slot), slotIndex(b.slot); si != sj {
			return si < sj
		}
		return a.opts.Weight < b.opts.Weight
	})

	r.subs = map[reflect.Type][]*entry{}
	for _, e := range r.host.live(KindSubscriber) {
		s := e.value.(subscription)
		r.subs[s.typ] = append(r.subs[s.typ], e)
	}
	byPrio := func(es []*entry) []*entry {
		sort.SliceStable(es, func(i, j int) bool { return es[i].priority < es[j].priority })
		return es
	}
	r.interceptors = byPrio(r.host.live(KindInterceptor))
	r.stages = byPrio(r.host.live(KindStage))
	r.actions = map[ext.ActionID]*entry{}
	for _, e := range r.host.live(KindAction) {
		r.actions[e.value.(ext.Action).ID] = e
	}
	r.dialogFacts = map[string]*entry{}
	for _, e := range r.host.live(KindDialog) {
		r.dialogFacts[e.id] = e
	}
	r.commands = map[string]*ext.Command{}
	for _, e := range r.host.live(KindCommand) {
		c := e.value.(ext.Command)
		cp := &c
		r.commands[c.Name] = cp
		for _, a := range c.Aliases {
			if _, taken := r.commands[a]; !taken {
				r.commands[a] = cp
			}
		}
	}
}

func (r *Root) compFeature(id string) string {
	if c := r.byID[id]; c != nil {
		return c.feature
	}
	return ""
}

// pickFocus keeps a valid focus, else focuses the first Focusable in the input slot.
// Components elsewhere (footer, panes) take focus only through Ctx.Focus; with no
// input component nothing is focused and keys reach only keymap actions.
func (r *Root) pickFocus() {
	if c := r.byID[r.focus]; c != nil && !r.host.Disabled(c.feature) {
		if _, ok := c.m.comp.(ext.Focusable); ok {
			return
		}
	}
	r.focus = ""
	for _, c := range r.comps {
		if c.m.slot != ext.SlotInput {
			continue
		}
		if _, ok := c.m.comp.(ext.Focusable); ok && r.modeOK(c) {
			r.focus = c.m.comp.ID()
			return
		}
	}
}

func (r *Root) modeOK(c *comp) bool {
	return len(c.m.opts.Modes) == 0 || slices.Contains(c.m.opts.Modes, r.layoutMode())
}

func (r *Root) layoutMode() ext.LayoutMode {
	if d := r.topDialog(); d != nil && d.d.Placement() == ext.PlaceAltScreen {
		return ext.AltView
	}
	return r.opts.Layout
}

func (r *Root) invalidateAll() {
	for _, c := range r.comps {
		c.valid = false
	}
	for _, d := range r.dialogs {
		d.c.valid = false
	}
}

func (r *Root) invalidate(id string) {
	if c := r.byID[id]; c != nil {
		c.valid = false
	}
	for _, d := range r.dialogs {
		if d.d.ID() == id || d.id == id {
			d.c.valid = false
		}
	}
}

// Init runs component Init and OnStart hooks.
func (r *Root) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range r.comps {
		var cmd tea.Cmd
		r.safe(c.feature, c.m.comp.ID()+".Init", func() { cmd = c.m.comp.Init(r.ctx) })
		cmds = append(cmds, wrapCmd(c.feature, c.m.comp.ID()+".Init", cmd))
	}
	for _, e := range r.host.live(KindStart) {
		fn := e.value.(startFn)
		var cmd tea.Cmd
		r.safe(e.feature, e.id+" OnStart", func() { cmd = fn(r.ctx) })
		cmds = append(cmds, wrapCmd(e.feature, e.id, cmd))
	}
	if f, ok := r.byID[r.focus]; ok {
		if fa, ok := f.m.comp.(ext.FocusAware); ok {
			var cmd tea.Cmd
			r.safe(f.feature, r.focus+".OnFocus", func() { cmd = fa.OnFocus(r.ctx) })
			cmds = append(cmds, wrapCmd(f.feature, r.focus, cmd))
		}
	}
	r.started = true
	cmds = append(cmds, tea.Tick(4*r.opts.FrameInterval, func(time.Time) tea.Msg { return firstFrameMsg{} }))
	if !r.opts.NoBackgroundQuery {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	cmds = append(cmds, r.startMain())
	return tea.Batch(cmds...)
}

// ExitReason is the reason given with the ExitMsg that ended the program, if any.
func (r *Root) ExitReason() string { return r.exitReason }

type firstFrameMsg struct{}

// Update routes a message. See docs/plans/01-contracts-shell.md (B2) for the order.
func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if c := r.shrinkCmd(); c != nil {
		cmds = append(cmds, c)
	}

	// Host-internal messages first: they are not part of the feature bus.
	switch m := msg.(type) {
	case shrinkReleaseMsg:
		if m.seq == r.shrink.seq {
			r.shrink.active = false
		}
		return r, tea.Batch(cmds...)
	case panicMsg:
		r.panicked(m.feature, m.where, m.value, m.stack)
		return r, tea.Batch(cmds...)
	case printStepMsg, printDoneMsg, clearedMsg:
		return r, tea.Batch(append(cmds, r.printerUpdate(msg))...)
	case noticeExpireMsg:
		r.expireNotice(m)
		return r, tea.Batch(cmds...)
	case spawnFailedMsg:
		return r, tea.Batch(append(cmds, r.spawnFailed(m))...)
	case firstFrameMsg:
		if r.firstFrame {
			return r, tea.Batch(cmds...)
		}
		r.firstFrame = true
		msg = ext.FirstFrameMsg{}
	}

	// Interceptors, by priority.
	for _, e := range r.interceptors {
		var out tea.Msg
		var cmd tea.Cmd
		in := msg
		if !r.safe(e.feature, e.id, func() { out, cmd = e.value.(ext.Interceptor)(r.ctx, in) }) {
			continue
		}
		cmds = append(cmds, wrapCmd(e.feature, e.id, cmd))
		if out == nil {
			return r, tea.Batch(cmds...)
		}
		msg = out
	}

	if cmd, ok := r.configUpdate(msg); ok {
		return r, tea.Batch(append(cmds, cmd)...)
	}
	if cmd, ok := r.themeUpdate(msg); ok {
		return r, tea.Batch(append(cmds, cmd)...)
	}

	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		r.w, r.h = m.Width, m.Height
		r.invalidateAll()
		cmds = append(cmds, r.broadcast(msg))
	case tea.KeyPressMsg:
		cmds = append(cmds, r.routeKey(m))
	case tea.PasteMsg:
		cmds = append(cmds, r.routePaste(m))
	case tea.MouseWheelMsg:
		cmds = append(cmds, r.routeWheel(m))
	case ext.AddressedMsg:
		cmds = append(cmds, r.deliver(m.To, m.Msg))
	case ext.ExitMsg:
		r.exitCode, r.exitReason = m.Code, m.Reason
		r.quitting = true
		cmds = append(cmds, r.broadcast(msg), tea.Quit)
	case ext.EngineAttachMsg:
		r.log().Debug("engine: attached", "id", m.EngineID)
		r.engines[m.EngineID] = m.Engine
		cmds = append(cmds, r.broadcast(msg))
	case ext.EngineStartMsg:
		if e := r.engines[m.EngineID]; e != nil {
			cmds = append(cmds, e.Restart(m.Opts))
		} else if r.opts.Spawn != nil {
			cmds = append(cmds, r.spawnCmd(m.EngineID, m.Opts))
		}
		cmds = append(cmds, r.broadcast(msg))
	case ext.EngineStopMsg:
		if stop := r.opts.Stop; stop != nil {
			id := m.EngineID
			cmds = append(cmds, func() tea.Msg {
				if err := stop(id); err != nil {
					return spawnFailedMsg{engineID: id, err: err}
				}
				return nil
			})
		}
		cmds = append(cmds, r.broadcast(msg))
	case ext.EngineDetachMsg:
		delete(r.engines, m.EngineID)
		cmds = append(cmds, r.broadcast(msg))
	case ext.TranscriptAttachMsg:
		r.transcript = m.Transcript
		cmds = append(cmds, r.broadcast(msg))
	case ext.SessionChangedMsg:
		r.sessions[m.EngineID] = m.Info
		r.invalidateAll()
		cmds = append(cmds, r.broadcast(msg))
	case ext.ThemeChangedMsg:
		// From a feature (e.g. a custom theme file changed): re-resolve.
		r.invalidateAll()
		cmds = append(cmds, r.applyTheme())
	case ext.CommandsMsg:
		r.runtimeCmds[m.Source+"|"+m.EngineID] = m.Commands
		cmds = append(cmds, r.broadcast(msg))
	default:
		cmds = append(cmds, r.broadcast(msg))
	}
	return r, tea.Batch(cmds...)
}

// broadcast delivers a message to typed subscribers, every mounted component and every
// open dialog.
func (r *Root) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range r.subs[reflect.TypeOf(msg)] {
		s := e.value.(subscription)
		var cmd tea.Cmd
		r.safe(e.feature, e.id, func() { cmd = s.fn(r.ctx, msg) })
		cmds = append(cmds, wrapCmd(e.feature, e.id, cmd))
	}
	for _, c := range slices.Clone(r.comps) {
		if r.host.Disabled(c.feature) {
			continue
		}
		var cmd tea.Cmd
		r.safe(c.feature, c.m.comp.ID()+".Update", func() { cmd = c.m.comp.Update(r.ctx, msg) })
		cmds = append(cmds, wrapCmd(c.feature, c.m.comp.ID(), cmd))
	}
	for _, d := range slices.Clone(r.dialogs) {
		var cmd tea.Cmd
		r.safe(d.feature, d.d.ID()+".Update", func() { cmd = d.d.Update(r.ctx, msg) })
		cmds = append(cmds, wrapCmd(d.feature, d.d.ID(), cmd))
	}
	return tea.Batch(cmds...)
}

// deliver sends a message to one component or dialog instance.
func (r *Root) deliver(to string, msg tea.Msg) tea.Cmd {
	for i := len(r.dialogs) - 1; i >= 0; i-- {
		d := r.dialogs[i]
		if d.d.ID() == to {
			var cmd tea.Cmd
			r.safe(d.feature, to+".Update", func() { cmd = d.d.Update(r.ctx, msg) })
			return wrapCmd(d.feature, to, cmd)
		}
	}
	if c := r.byID[to]; c != nil && !r.host.Disabled(c.feature) {
		var cmd tea.Cmd
		r.safe(c.feature, to+".Update", func() { cmd = c.m.comp.Update(r.ctx, msg) })
		return wrapCmd(c.feature, to, cmd)
	}
	return nil
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	return tea.Tick(d, fn)
}

type emptySettings struct{}

func (emptySettings) Claude(string) (any, bool)     { return nil, false }
func (emptySettings) Mantle(string) any             { return nil }
func (emptySettings) SetMantle(string, any) tea.Cmd { return nil }

// clip limits a line to width cells.
func clip(line string, w int) string {
	if w <= 0 {
		return ""
	}
	if strings.IndexByte(line, '\t') >= 0 {
		line = strings.ReplaceAll(line, "\t", "    ")
	}
	return truncate(line, w)
}
