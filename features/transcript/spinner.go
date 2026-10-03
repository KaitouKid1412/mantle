package transcript

import (
	"hash/fnv"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// builtinVerbs are mantle's own spinner verbs.
var builtinVerbs = []string{
	"Pondering", "Tinkering", "Noodling", "Mulling", "Puzzling", "Brewing", "Weaving",
	"Sketching", "Untangling", "Assembling", "Composing", "Sifting", "Tracing", "Mapping",
	"Polishing", "Wrangling", "Juggling", "Kneading", "Simmering", "Distilling", "Forging",
	"Charting", "Sorting", "Scheming", "Spelunking", "Calibrating", "Percolating", "Crafting",
	"Musing", "Tuning", "Whittling", "Conjuring", "Gathering", "Fiddling", "Plotting",
	"Orchestrating", "Rummaging", "Stitching", "Carving", "Hatching", "Shuffling", "Pruning",
	"Sprouting", "Fathoming", "Deciphering", "Ruminating", "Cogitating", "Synthesizing",
	"Marinating", "Churning",
}

// builtinTips are mantle's own spinner tips.
var builtinTips = []string{
	"Press ctrl+o to see the full transcript, with every tool output expanded",
	"Start a message with ! to run a shell command and share its output",
	"Use @ to mention files; they are attached to your message",
	"Press shift+tab to cycle permission modes, including plan mode",
	"Queue a follow-up while Claude works: just type and press enter",
	"Run /compact when the conversation gets long to free up context",
	"Use /mantle to change how mantle itself looks or behaves",
	"Press esc twice to rewind the conversation to an earlier message",
}

// spinner frames (a twinkle); the reduced-motion form uses only the last one.
var spinFrames = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

const (
	spinInterval  = 120 * time.Millisecond
	staticRefresh = time.Second
	tipAfter      = 3 * time.Second
	tipEvery      = 12 * time.Second
)

// spinTick drives the animation; gen discards ticks from an earlier turn.
type spinTick struct{ gen int }

// spinner is the SlotStatus component: verb, elapsed time, tokens and the
// interrupt hint while a turn runs, and a rotating tip under it.
type spinner struct {
	f *Feature

	busy       bool
	compacting bool
	waiting    bool // requires_action: a dialog is up
	start      time.Time
	verb       string
	frame      int
	gen        int
	ticking    bool

	tokens  int64 // output tokens this turn (estimated from streamed bytes)
	tipFile []string
}

func newSpinner(f *Feature) *spinner { return &spinner{f: f} }

func (s *spinner) ID() string             { return SpinnerID }
func (s *spinner) Init(c ext.Ctx) tea.Cmd { return nil }

func (s *spinner) setup(ext.Registrar) {}

func (s *spinner) Update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	t, ok := msg.(spinTick)
	if !ok || t.gen != s.gen || !s.busy {
		return nil
	}
	s.frame++
	c.Invalidate(SpinnerID)
	return s.tick(c)
}

func (s *spinner) tick(c ext.Ctx) tea.Cmd {
	d := spinInterval
	if s.f.cfg.noMotion {
		d = staticRefresh
	}
	gen := s.gen
	return c.Clock().Tick(d, func(time.Time) tea.Msg {
		return ext.AddressedMsg{To: SpinnerID, Msg: spinTick{gen}}
	})
}

// onEvent updates the spinner from an engine event and returns the Cmd that
// keeps it animating.
func (s *spinner) onEvent(c ext.Ctx, ev proto.Event) tea.Cmd {
	wasBusy := s.busy
	switch e := ev.(type) {
	case *proto.SessionStateChanged:
		s.waiting = e.State == proto.StateRequiresAction
		switch e.State {
		case proto.StateRunning, proto.StateRequiresAction:
			s.begin(c)
		case proto.StateIdle:
			s.stop()
		}
	case *proto.StreamEvent:
		s.begin(c)
		if e.Event.Type == proto.StreamContentBlockDelta && e.Event.Delta != nil {
			// Rough live estimate: ~4 bytes per token.
			n := int64(len(e.Event.Delta.Text) + len(e.Event.Delta.Thinking) + len(e.Event.Delta.PartialJSON))
			s.tokens += (n + 3) / 4
		}
	case *proto.Assistant:
		s.begin(c)
	case *proto.ThinkingTokens:
		s.tokens += e.EstimatedTokensDelta
	case *proto.Status:
		s.compacting = e.Status == "compacting"
		if s.compacting {
			s.begin(c)
		}
	case *proto.Result:
		s.stop()
	case *proto.ConversationReset:
		s.stop()
	}
	c.Invalidate(SpinnerID)
	if s.busy && (!wasBusy || !s.ticking) {
		s.ticking = true
		return s.tick(c)
	}
	return nil
}

func (s *spinner) begin(c ext.Ctx) {
	if s.busy {
		return
	}
	s.busy = true
	s.gen++
	s.ticking = false
	s.start = c.Clock().Now()
	s.frame = 0
	s.tokens = 0
	verbs := s.f.cfg.verbs
	if len(verbs) == 0 {
		verbs = builtinVerbs
	}
	h := fnv.New32a()
	h.Write([]byte(s.start.String()))
	s.verb = verbs[h.Sum32()%uint32(len(verbs))]
}

func (s *spinner) stop() {
	s.busy = false
	s.compacting = false
	s.waiting = false
	s.ticking = false
	s.gen++
}

func (s *spinner) View(c ext.Ctx, a ext.Area) ext.Rendered {
	if !s.busy || s.waiting {
		return ext.Rendered{}
	}
	th := c.Theme()
	st := stylesFor(ext.RenderCtx{Theme: th})
	now := c.Clock().Now()
	elapsed := now.Sub(s.start)

	tok, shim := theme.Accent, theme.AccentShimmer
	verb := s.verb
	if s.compacting {
		tok, shim = theme.SystemSpinner, theme.SystemSpinnerShimmer
		verb = "Compacting conversation"
	}
	p := paletteOf(th)
	glyph := spinFrames[s.frame%len(spinFrames)]
	motion := !s.f.cfg.noMotion && !c.Accessibility().ReducedMotion && !c.Accessibility().ScreenReader
	if !motion {
		glyph = "✻"
	}
	line := render.Fg(p, string(tok)).Render(glyph) + " "
	if motion {
		line += shimmer(verb+"…", s.frame, render.Fg(p, string(tok)), render.Fg(p, string(shim)))
	} else {
		line += render.Fg(p, string(tok)).Render(verb + "…")
	}
	var details []string
	details = append(details, formatDuration(elapsed.Truncate(time.Second)))
	if n := s.tokens; n > 0 {
		details = append(details, "↓ "+formatTokens(n)+" tokens")
	}
	key := "esc"
	if keys := c.KeysFor(ext.ContextChat, ext.ActChatCancel); len(keys) > 0 {
		key = keys[0]
	}
	details = append(details, key+" to interrupt")
	line += " " + st.dim.Render("("+strings.Join(details, " · ")+")")
	lines := []string{render.Truncate(line, a.Width, "…")}

	if tip := s.tip(elapsed); tip != "" && (a.MaxHeight == 0 || a.MaxHeight > 1) {
		lines = append(lines, render.Truncate(st.dim.Render(resultIndent+"Tip: "+tip), a.Width, "…"))
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// tip returns the tip to show after the turn has run a little while.
func (s *spinner) tip(elapsed time.Duration) string {
	if !s.f.cfg.tips || elapsed < tipAfter {
		return ""
	}
	tips := s.tips()
	if len(tips) == 0 {
		return ""
	}
	h := fnv.New32a()
	h.Write([]byte(s.start.String()))
	i := (int(h.Sum32()) + int((elapsed-tipAfter)/tipEvery)) % len(tips)
	if i < 0 {
		i = -i
	}
	return clean(tips[i])
}

func (s *spinner) tips() []string {
	cfg := s.f.cfg
	var tips []string
	if len(cfg.tipList) > 0 {
		tips = append(tips, cfg.tipList...)
	}
	if cfg.tipsFile != "" {
		if s.tipFile == nil {
			s.tipFile = []string{}
			if b, err := os.ReadFile(cfg.tipsFile); err == nil {
				for _, l := range strings.Split(string(b), "\n") {
					if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
						s.tipFile = append(s.tipFile, l)
					}
				}
			}
		}
		tips = append(tips, s.tipFile...)
	}
	if len(tips) == 0 {
		tips = builtinTips
	}
	return tips
}

// shimmer sweeps a highlight across text: three cells in the shimmer colour
// moving one cell per frame.
func shimmer(text string, frame int, base, hi render.Style) string {
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}
	pos := frame%(n+6) - 3
	var b strings.Builder
	for i := 0; i < n; {
		inHi := i >= pos && i < pos+3
		j := i
		for j < n && (j >= pos && j < pos+3) == inHi {
			j++
		}
		style := base
		if inHi {
			style = hi
		}
		b.WriteString(style.Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}
