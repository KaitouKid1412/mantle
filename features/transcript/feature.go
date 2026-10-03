package transcript

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// FeatureID is the transcript feature's ID.
const FeatureID = "transcript"

// Component IDs.
const (
	LiveID    = "transcript.live"
	SpinnerID = "transcript.spinner"
)

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 100,
		Parity: []string{
			"TR-01", "TR-02", "TR-03", "TR-04", "TR-05", "TR-06", "TR-07", "TR-08", "TR-09",
			"TR-10", "TR-11", "TR-12", "TR-13", "TR-14", "TR-15", "TR-16", "TR-17", "TR-18",
			"TR-19", "TR-20", "TR-21", "TR-22", "TR-23", "TR-24", "TR-25", "TR-26", "TR-27",
			"TR-28", "TR-29", "TR-30", "TR-31", "TR-32", "TR-33", "TR-34", "TR-35", "TR-36",
			"TR-37", "TR-38", "TR-39", "TR-40", "TR-41", "TR-42", "TR-44", "TR-45", "TR-46",
			"TR-47", "TR-48", "TR-49", "TR-50", "TR-51", "TR-52", "TR-53", "TR-54", "TR-55",
			"TR-56", "TR-57", "TR-58", "TR-59", "TR-60", "VW-08", "VW-13",
		},
		Setup: func(r ext.Registrar) error { return New(ext.MainEngine).Setup(r) },
	})
}

// Feature is the transcript: store, renderers, commit policy, live area and
// spinner for one engine.
type Feature struct {
	store *Store
	texts map[string]*textEntry
	cfg   config
	cwd   string

	renderers map[ext.ContentKey]ext.Renderer   // built-ins, by key
	resolve   func(ext.ContentKey) ext.Renderer // the host's resolver (mods applied)

	commit  commitState
	live    *liveView
	spinner *spinner

	brief      bool   // brief mode (app:toggleBrief), this session only
	engineView string // view_mode reported by the engine (/focus toggles it)
}

// New returns a transcript feature for an engine's events.
func New(engineID string) *Feature {
	f := &Feature{
		store: NewStore(engineID, time.Now),
		texts: map[string]*textEntry{},
		cfg:   defaultConfig(),
	}
	f.live = &liveView{f: f}
	f.spinner = newSpinner(f)
	return f
}

// Store returns the feature's transcript store.
func (f *Feature) Store() *Store { return f.store }

// Setup registers the feature.
func (f *Feature) Setup(r ext.Registrar) error {
	f.registerRenderers(r)
	r.AddComponent(ext.SlotLive, f.live, ext.SlotOpts{})
	r.AddComponent(ext.SlotStatus, f.spinner, ext.SlotOpts{MaxHeight: 2})
	r.OnStart("transcript.attach", func(c ext.Ctx) tea.Cmd {
		f.store.now = c.Clock().Now
		f.cfg = loadConfig(c.Settings())
		f.resolve = c.Renderer
		return ext.Msg(ext.TranscriptAttachMsg{Transcript: f.store})
	})
	ext.Subscribe(r, "transcript.events", f.onEvent)
	ext.Subscribe(r, "transcript.cleared", func(c ext.Ctx, _ ext.ScreenClearedMsg) tea.Cmd {
		return f.reprint(c)
	})
	ext.Subscribe(r, "transcript.settings", func(c ext.Ctx, m ext.SettingsMsg) tea.Cmd {
		old, oldMode := f.cfg, f.mode()
		f.cfg = loadConfig(c.Settings())
		c.Invalidate(LiveID)
		c.Invalidate(SpinnerID)
		if oldMode != f.mode() || old.maxProse != f.cfg.maxProse || old.noHighlight != f.cfg.noHighlight {
			return c.Reprint()
		}
		return nil
	})
	ext.Subscribe(r, "transcript.history", func(c ext.Ctx, m ext.TranscriptHistoryMsg) tea.Cmd {
		if !f.forEngine(m.EngineID) {
			return nil
		}
		if m.Reset {
			f.store.Reset()
			f.texts = map[string]*textEntry{}
			f.commit = commitState{}
		}
		f.store.AppendHistory(m.Items)
		c.Invalidate(LiveID)
		return f.commitReady(c)
	})
	r.AddAction(ext.Action{
		ID: ext.ActAppToggleBrief, Context: ext.ContextGlobal,
		Description: "Toggle brief mode (only messages addressed to you)",
		Run: func(c ext.Ctx) (bool, tea.Cmd) {
			f.brief = !f.brief
			label := "Brief mode off"
			if f.brief {
				label = "Brief mode on"
			}
			return true, tea.Batch(c.Notify(ext.Notice{Key: "transcript.brief", Text: label, Source: FeatureID}), c.Reprint())
		},
	})
	f.spinner.setup(r)
	f.registerStories(r)
	return nil
}

func (f *Feature) onEvent(c ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	if !f.forEngine(m.EngineID) {
		return nil
	}
	var reprint tea.Cmd
	if init, ok := m.Event.(*proto.SystemInit); ok {
		if init.CWD != "" {
			f.cwd = init.CWD
		}
		if init.ViewMode != f.engineView {
			old := f.mode()
			f.engineView = init.ViewMode
			if f.mode() != old {
				reprint = c.Reprint()
			}
		}
	}
	spin := f.spinner.onEvent(c, m.Event)
	if n, ok := m.Event.(*proto.Notification); ok && strings.TrimSpace(n.Text) != "" {
		spin = tea.Batch(spin, c.Notify(engineNotice(n)))
	}
	if _, ok := m.Event.(*proto.ConversationReset); ok {
		f.store.Apply(m.Event)
		f.texts = map[string]*textEntry{}
		f.commit = commitState{}
		c.Invalidate(LiveID)
		return tea.Batch(spin, c.Reprint())
	}
	if !f.store.Apply(m.Event) {
		return tea.Batch(spin, reprint)
	}
	c.Invalidate(LiveID)
	if reprint != nil {
		return tea.Batch(spin, reprint)
	}
	return tea.Batch(spin, f.commitReady(c))
}

// engineNotice turns an engine notification into a transient notice.
func engineNotice(n *proto.Notification) ext.Notice {
	level := ext.NoticeInfo
	switch n.Priority {
	case "high", "immediate":
		level = ext.NoticeWarning
	}
	key := n.Key
	if key == "" {
		key = "engine.notification"
	}
	return ext.Notice{Key: key, Text: oneLine(n.Text), Level: level, Timeout: msToDuration(n.TimeoutMS), Source: FeatureID}
}

// forEngine reports whether a message for an engine belongs to this feature
// ("" means the main engine).
func (f *Feature) forEngine(id string) bool {
	if id == "" {
		id = ext.MainEngine
	}
	return id == f.store.engineID
}

// renderCtx builds the RenderCtx for an item at a width.
func (f *Feature) renderCtx(c ext.Ctx, it *ext.Item, width int) ext.RenderCtx {
	return ext.RenderCtx{
		Width:    width,
		Mode:     f.mode(),
		Theme:    c.Theme(),
		Now:      c.Clock().Now(),
		Children: f.store.Children(it.ID),
	}
}
