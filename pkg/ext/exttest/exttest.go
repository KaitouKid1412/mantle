// Package exttest provides test doubles for pkg/ext: a fake Ctx and a recording
// Registrar. Use these instead of implementing ext.Ctx or ext.Registrar yourself; they
// gain methods whenever ext does, so your tests keep compiling.
package exttest

import (
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Epoch is the fixed time a new Ctx's clock starts at.
var Epoch = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// Ctx is a fake ext.Ctx. Configure it through its fields; it records what features ask
// it to do. Cmds it returns are real Cmds whose messages are recorded types (PrintMsg,
// ext.DialogOpenedMsg, …) so tests can run them and inspect the result.
type Ctx struct {
	W, H         int
	LayoutMode   ext.LayoutMode
	ThemeValue   theme.Theme
	SessionValue ext.SessionInfo
	Engines      map[string]ext.Engine
	TranscriptV  ext.Transcript
	SettingsV    *Settings
	ClockV       *Clock
	CommandList  []ext.Command
	Bindings     []ext.Binding
	Logger       *slog.Logger

	// Recorded calls.
	Printed     []string
	Reprints    int
	Notices     []ext.Notice
	Invalidated []string
	Opened      []string // dialog IDs
	Closed      []string
	RanActions  []ext.ActionID
	Submitted   []ext.Draft
	FocusedID   string
	ActiveCtxs  map[string]bool
	stores      map[string]*KV
}

// NewCtx returns a Ctx with a 100×30 inline layout, the default theme, empty settings
// and a clock frozen at Epoch.
func NewCtx() *Ctx {
	return &Ctx{
		W: 100, H: 30,
		ThemeValue:   theme.Default(),
		SessionValue: ext.SessionInfo{EngineID: ext.MainEngine},
		Engines:      map[string]ext.Engine{},
		SettingsV:    NewSettings(nil),
		ClockV:       &Clock{T: Epoch},
		Bindings:     slices.Clone(ext.DefaultBindings),
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		ActiveCtxs:   map[string]bool{},
		stores:       map[string]*KV{},
	}
}

var _ ext.Ctx = (*Ctx)(nil)

// PrintMsg is what Ctx.Print's Cmd returns.
type PrintMsg struct{ Blocks []string }

// ReprintMsg is what Ctx.Reprint's Cmd returns.
type ReprintMsg struct{}

// RunActionMsg is what Ctx.Run's Cmd returns.
type RunActionMsg struct{ ID ext.ActionID }

// SubmitMsg is what Ctx.Submit's Cmd returns.
type SubmitMsg struct{ Draft ext.Draft }

func (c *Ctx) Engine(id string) ext.Engine {
	if id == "" {
		id = ext.MainEngine
	}
	return c.Engines[id]
}
func (c *Ctx) Session() ext.SessionInfo           { return c.SessionValue }
func (c *Ctx) Transcript() ext.Transcript         { return c.TranscriptV }
func (c *Ctx) Settings() ext.Settings             { return c.SettingsV }
func (c *Ctx) Theme() *theme.Theme                { return &c.ThemeValue }
func (c *Ctx) Clock() ext.Clock                   { return c.ClockV }
func (c *Ctx) Size() (w, h int)                   { return c.W, c.H }
func (c *Ctx) Layout() ext.LayoutMode             { return c.LayoutMode }
func (c *Ctx) Invalidate(id string)               { c.Invalidated = append(c.Invalidated, id) }
func (c *Ctx) Log() *slog.Logger                  { return c.Logger }
func (c *Ctx) Focused() string                    { return c.FocusedID }
func (c *Ctx) SetContextActive(k string, on bool) { c.ActiveCtxs[k] = on }

func (c *Ctx) Print(blocks ...string) tea.Cmd {
	c.Printed = append(c.Printed, blocks...)
	return ext.Msg(PrintMsg{Blocks: blocks})
}

func (c *Ctx) Reprint() tea.Cmd {
	c.Reprints++
	return ext.Msg(ReprintMsg{})
}

func (c *Ctx) Notify(n ext.Notice) tea.Cmd {
	c.Notices = append(c.Notices, n)
	return nil
}

func (c *Ctx) OpenDialog(id string, args any) tea.Cmd {
	c.Opened = append(c.Opened, id)
	return ext.Msg(ext.DialogOpenedMsg{ID: id, Instance: id})
}

func (c *Ctx) CloseDialog(id string) tea.Cmd {
	c.Closed = append(c.Closed, id)
	return ext.Msg(ext.DialogClosedMsg{ID: id, Instance: id})
}

func (c *Ctx) Run(a ext.ActionID) tea.Cmd {
	c.RanActions = append(c.RanActions, a)
	return ext.Msg(RunActionMsg{ID: a})
}

func (c *Ctx) Submit(d ext.Draft) tea.Cmd {
	c.Submitted = append(c.Submitted, d)
	return ext.Msg(SubmitMsg{Draft: d})
}

func (c *Ctx) Focus(id string) tea.Cmd {
	c.FocusedID = id
	return nil
}

func (c *Ctx) Store(featureID string) ext.KV {
	kv, ok := c.stores[featureID]
	if !ok {
		kv = &KV{M: map[string]json.RawMessage{}}
		c.stores[featureID] = kv
	}
	return kv
}

func (c *Ctx) KeysFor(context string, a ext.ActionID) []string {
	var out []string
	for _, b := range c.Bindings {
		if b.Action == a && (context == "" || b.Context == context) {
			out = append(out, b.Keys)
		}
	}
	return out
}

func (c *Ctx) Command(name string) (ext.Command, bool) {
	name = strings.TrimPrefix(name, "/")
	for _, cmd := range c.CommandList {
		if cmd.Name == name || slices.Contains(cmd.Aliases, name) {
			return cmd, true
		}
	}
	return ext.Command{}, false
}

func (c *Ctx) Commands() []ext.Command {
	out := slices.Clone(c.CommandList)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Area returns an Area for this Ctx's width.
func (c *Ctx) Area() ext.Area {
	return ext.Area{Width: c.W, Mode: c.LayoutMode}
}

// Settings is a fake ext.Settings backed by maps.
type Settings struct {
	ClaudeM map[string]any
	MantleM map[string]any
}

// NewSettings returns settings with the given merged Claude Code values.
func NewSettings(claude map[string]any) *Settings {
	if claude == nil {
		claude = map[string]any{}
	}
	return &Settings{ClaudeM: claude, MantleM: map[string]any{}}
}

func (s *Settings) Claude(key string) (any, bool) {
	v, ok := s.ClaudeM[key]
	return v, ok
}
func (s *Settings) Mantle(key string) any { return s.MantleM[key] }
func (s *Settings) SetMantle(key string, v any) tea.Cmd {
	s.MantleM[key] = v
	return ext.Msg(ext.SettingsMsg{Changed: []string{"mantle:" + key}})
}

// Clock is a manual clock. Tick Cmds fire immediately with Now()+d and advance nothing;
// tests advance T themselves.
type Clock struct{ T time.Time }

func (c *Clock) Now() time.Time { return c.T }
func (c *Clock) Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	at := c.T.Add(d)
	return func() tea.Msg { return fn(at) }
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) { c.T = c.T.Add(d) }

// KV is an in-memory ext.KV.
type KV struct{ M map[string]json.RawMessage }

func (k *KV) Get(key string, out any) (bool, error) {
	raw, ok := k.M[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (k *KV) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	k.M[key] = raw
	return nil
}

func (k *KV) Delete(key string) error { delete(k.M, key); return nil }

func (k *KV) Keys() []string {
	out := make([]string, 0, len(k.M))
	for key := range k.M {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Exec runs a Cmd and returns its message, flattening tea.Batch and tea.Sequence
// results into a slice (nil Cmds and nil messages are dropped).
func Exec(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, Exec(c)...)
		}
		return out
	}
	if seq, ok := asSequence(msg); ok {
		var out []tea.Msg
		for _, c := range seq {
			out = append(out, Exec(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// asSequence recognises tea.Sequence's unexported []Cmd message type.
func asSequence(msg tea.Msg) ([]tea.Cmd, bool) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return nil, false
	}
	out := make([]tea.Cmd, v.Len())
	for i := range out {
		out[i] = v.Index(i).Interface().(tea.Cmd)
	}
	return out, true
}
