package ui

import (
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// DefaultFrames are mantle's spinner frames (our own; not Claude Code's glyphs).
var DefaultFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// SpinnerTickMsg advances a Spinner; it is addressed to the spinner's ID.
type SpinnerTickMsg struct {
	ID  string
	Seq int
}

// Spinner is an animation primitive: a frame counter driven by the host clock. It is
// static when the user prefers reduced motion. Embed it in a component (the turn
// spinner, a task row) and render Frame().
type Spinner struct {
	IDValue  string
	Frames   []string
	Interval time.Duration
	Token    theme.Token

	frame   int
	seq     int
	running bool
}

// Start begins ticking.
func (s *Spinner) Start(c ext.Ctx) tea.Cmd {
	if s.running {
		return nil
	}
	s.running = true
	s.seq++
	return s.tick(c)
}

// Stop stops ticking.
func (s *Spinner) Stop() { s.running = false; s.seq++ }

// Running reports whether the spinner animates.
func (s *Spinner) Running() bool { return s.running }

func (s *Spinner) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return 100 * time.Millisecond
}

func (s *Spinner) tick(c ext.Ctx) tea.Cmd {
	if c.Accessibility().ReducedMotion || c.Accessibility().ScreenReader {
		return nil
	}
	id, seq := s.IDValue, s.seq
	return c.Clock().Tick(s.interval(), func(time.Time) tea.Msg {
		return ext.AddressedMsg{To: id, Msg: SpinnerTickMsg{ID: id, Seq: seq}}
	})
}

// Update advances on its own tick and returns the next one.
func (s *Spinner) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	m, ok := msg.(SpinnerTickMsg)
	if !ok || m.ID != s.IDValue || m.Seq != s.seq || !s.running {
		return nil
	}
	s.frame++
	c.Invalidate(s.IDValue)
	return s.tick(c)
}

// Frame returns the current frame, coloured.
func (s *Spinner) Frame(c ext.Ctx) string {
	frames := s.Frames
	if len(frames) == 0 {
		frames = DefaultFrames
	}
	tok := s.Token
	if tok == "" {
		tok = theme.Accent
	}
	f := frames[0]
	if !c.Accessibility().ReducedMotion {
		f = frames[s.frame%len(frames)]
	}
	return c.Theme().Paint(tok, f)
}

// Step returns the number of ticks so far (for shimmer positions).
func (s *Spinner) Step() int { return s.frame }

// Shimmer renders text in base colour with a highlight band (width cells) of hi
// colour at position pos, the sweep used for spinner verbs and mode borders.
func Shimmer(text string, pos, width int, base, hi color.Color) string {
	if text == "" {
		return ""
	}
	plain := ansi.Strip(text)
	runes := []rune(plain)
	n := len(runes)
	if n == 0 {
		return ""
	}
	pos = ((pos % (n + width)) + n + width) % (n + width)
	var b strings.Builder
	bs, hs := lipgloss.NewStyle().Foreground(base), lipgloss.NewStyle().Foreground(hi)
	start, end := pos-width, pos
	var run []rune
	inHi := false
	flush := func() {
		if len(run) == 0 {
			return
		}
		if inHi {
			b.WriteString(hs.Render(string(run)))
		} else {
			b.WriteString(bs.Render(string(run)))
		}
		run = run[:0]
	}
	for i, r := range runes {
		h := i >= start && i < end
		if h != inHi {
			flush()
			inHi = h
		}
		run = append(run, r)
	}
	flush()
	return b.String()
}
