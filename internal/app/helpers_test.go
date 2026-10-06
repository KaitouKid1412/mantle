package app

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

// manualClock never fires ticks by itself; tests advance it.
type manualClock struct {
	t     time.Time
	ticks []func() tea.Msg
}

func (c *manualClock) Now() time.Time { return c.t }
func (c *manualClock) Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	at := c.t.Add(d)
	c.ticks = append(c.ticks, func() tea.Msg { return fn(at) })
	return nil
}

// box is a configurable test component.
type box struct {
	id        string
	text      string
	ctx       string // key context; "" = not focusable behaviour
	keys      []string
	pastes    []string
	actions   []ext.ActionID
	handle    map[ext.ActionID]bool // actions HandleAction claims
	gotMu     sync.Mutex            // got is written by the program goroutine
	got       []tea.Msg
	panicView bool
	views     int
}

func (b *box) ID() string           { return b.id }
func (b *box) Init(ext.Ctx) tea.Cmd { return nil }
func (b *box) Update(c ext.Ctx, m tea.Msg) tea.Cmd {
	b.gotMu.Lock()
	b.got = append(b.got, m)
	b.gotMu.Unlock()
	return nil
}

// msgs returns the messages Update got so far (safe while a program runs).
func (b *box) msgs() []tea.Msg {
	b.gotMu.Lock()
	defer b.gotMu.Unlock()
	return append([]tea.Msg(nil), b.got...)
}

// resetMsgs forgets the messages Update got so far.
func (b *box) resetMsgs() {
	b.gotMu.Lock()
	b.got = nil
	b.gotMu.Unlock()
}
func (b *box) View(c ext.Ctx, a ext.Area) ext.Rendered {
	b.views++
	if b.panicView {
		panic("boom in " + b.id)
	}
	return ext.Rendered{Text: b.text}
}

type focusBox struct{ *box }

func (f focusBox) KeyContext() string { return f.ctx }
func (f focusBox) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	f.keys = append(f.keys, k.String())
	return true, nil
}
func (f focusBox) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	f.pastes = append(f.pastes, p.Content)
	return true, nil
}
func (f focusBox) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	f.actions = append(f.actions, a)
	return f.handle[a], nil
}

// testDialog is an inline dialog that closes on confirm:yes / confirm:no.
type testDialog struct {
	focusBox
	result string
}

func (d *testDialog) Placement() ext.Placement { return ext.PlaceInline }
func (d *testDialog) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	switch a {
	case ext.ActConfirmYes:
		d.result = "yes"
		return true, c.CloseDialog(d.id)
	case ext.ActConfirmNo:
		d.result = "no"
		return true, c.CloseDialog(d.id)
	}
	return false, nil
}
func (d *testDialog) Result() any { return d.result }

func newRoot(t *testing.T, fs ...ext.Feature) (*Root, *manualClock) {
	t.Helper()
	clk := &manualClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	h := NewHost(fs, HostOptions{Core: CoreFeatures()})
	for _, rep := range h.Reports() {
		if rep.Fatal {
			t.Fatalf("fatal setup report: %v", rep)
		}
	}
	r := New(Options{Host: h, Clock: clk, NoBackgroundQuery: true, FrameInterval: time.Millisecond})
	r.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return r, clk
}

// drive feeds msgs to the root, executing returned Cmds and feeding their messages
// back, until quiet. Messages the root can't handle (Bubble Tea internals such as
// print or quit) are returned.
func drive(t *testing.T, r *Root, msgs ...tea.Msg) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	queue := append([]tea.Msg(nil), msgs...)
	for i := 0; len(queue) > 0; i++ {
		if i > 10000 {
			t.Fatal("drive: message loop did not settle")
		}
		m := queue[0]
		queue = queue[1:]
		if isTeaInternal(m) {
			out = append(out, m)
			continue
		}
		_, cmd := r.Update(m)
		queue = append(queue, exttest.Exec(cmd)...)
	}
	return out
}

func isTeaInternal(m tea.Msg) bool {
	switch m.(type) {
	case tea.QuitMsg, tea.RawMsg, tea.SuspendMsg:
		return true
	}
	// Bubble Tea's own command messages (print, sequence, clear screen, …) are
	// unexported types; the program handles them, not the model.
	t := reflect.TypeOf(m)
	name := t.Name()
	return t.PkgPath() == "charm.land/bubbletea/v2" && name != "" && name[0] >= 'a' && name[0] <= 'z'
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	if rest, ok := strings.CutPrefix(s, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func lines(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s %03d", prefix, i)
	}
	return out
}
