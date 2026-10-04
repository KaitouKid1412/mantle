package sessions

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// usage is what the feature knows about an engine's spend and context, from results,
// assistant usage, rate-limit events and get_context_usage replies.
type usage struct {
	costUSD  float64 // the session's total (continues from saved totals on resume)
	apiMS    int64   // API time of the turns seen in this process
	turns    int
	models   map[string]proto.ModelUsage // from the last result (session totals)
	model    string                      // model of the last main-thread response
	used     int64                       // context tokens of the last main-thread response
	window   int64                       // that model's context window, when known
	ctx      *proto.ContextUsage         // last get_context_usage reply
	ctxAt    time.Time
	rate     map[string]proto.RateLimitInfo // by rate-limit type
	rateSeen time.Time
}

func (f *feature) usage(engineID string) *usage {
	st := f.engine(engineID)
	if st.usage == nil {
		st.usage = &usage{models: map[string]proto.ModelUsage{}, rate: map[string]proto.RateLimitInfo{}}
	}
	return st.usage
}

// contextComponentID draws the auto-compact countdown under the prompt.
const contextComponentID = "sessions.context"

type contextUsageMsg struct {
	engineID string
	usage    *proto.ContextUsage
	err      error
}

// observeUsage folds an engine event into the usage state. It returns a Cmd when the
// event asks for a fresh context measurement.
func (f *feature) observeUsage(ctx ext.Ctx, engineID string, ev proto.Event) tea.Cmd {
	u := f.usage(engineID)
	switch e := ev.(type) {
	case *proto.Assistant:
		if e.ParentToolUseID != "" || e.Message.Usage == nil || e.Message.Model == proto.SyntheticModel {
			return nil
		}
		m := e.Message.Usage
		u.used = m.InputTokens + m.CacheReadInputTokens + m.CacheCreationInputTokens + m.OutputTokens
		u.model = e.Message.Model
		u.ctx = nil // stale until the next measurement
		ctx.Invalidate(contextComponentID)
	case *proto.Result:
		u.turns++
		u.apiMS += e.DurationAPIMS
		if e.TotalCostUSD > 0 {
			u.costUSD = e.TotalCostUSD
		}
		if len(e.ModelUsage) > 0 {
			u.models = e.ModelUsage
			if mu, ok := e.ModelUsage[u.model]; ok && mu.ContextWindow > 0 {
				u.window = mu.ContextWindow
			} else {
				for _, mu := range e.ModelUsage {
					u.window = max(u.window, mu.ContextWindow)
				}
			}
		}
		ctx.Invalidate(contextComponentID)
		return f.measureContext(ctx, engineID)
	case *proto.CompactBoundary:
		u.used, u.ctx = 0, nil
		ctx.Invalidate(contextComponentID)
	case *proto.ConversationReset:
		*u = usage{models: map[string]proto.ModelUsage{}, rate: u.rate}
		ctx.Invalidate(contextComponentID)
	case *proto.RateLimitEvent:
		u.rate[e.RateLimitInfo.RateLimitType] = e.RateLimitInfo
		u.rateSeen = f.now()
	}
	return nil
}

// measureContext asks the engine for its context breakdown (no model call).
func (f *feature) measureContext(ctx ext.Ctx, engineID string) tea.Cmd {
	eng := ctx.Engine(engineID)
	if eng == nil || !eng.Supports(proto.SubGetContextUsage) {
		return nil
	}
	return controlCmd(eng.Control(proto.SubGetContextUsage, proto.GetContextUsageRequest{}), func(r ext.ControlResultMsg) tea.Msg {
		var cu proto.ContextUsage
		err := decodeControl(r, &cu)
		return contextUsageMsg{engineID: engineID, usage: &cu, err: err}
	})
}

func (f *feature) onContextUsage(ctx ext.Ctx, m contextUsageMsg) tea.Cmd {
	if m.err != nil || m.usage == nil {
		return nil
	}
	u := f.usage(m.engineID)
	u.ctx, u.ctxAt = m.usage, f.now()
	ctx.Invalidate(contextComponentID)
	return nil
}

// Thresholds for the countdown, in tokens. The engine keeps a summary buffer below the
// window where auto-compact fires; the countdown appears once usage is within
// warnBuffer of that point.
const (
	defaultWindow     = 200_000
	autocompactBuffer = 13_000
	warnBuffer        = 20_000
)

// contextLevel is the countdown state for the main session.
type contextLevel struct {
	show     bool
	pctLeft  int  // percent left until auto-compact (or of the window when it is off)
	autoOn   bool // auto-compact enabled
	used     int64
	limit    int64
	measured bool // from get_context_usage rather than an estimate
}

func (u *usage) level(autoOn bool) contextLevel {
	lv := contextLevel{autoOn: autoOn}
	var warnAt int64
	if c := u.ctx; c != nil && c.MaxTokens > 0 {
		var buffer, usedSum int64
		for _, cat := range c.Categories {
			switch {
			case cat.Kind == "buffer":
				buffer += cat.Tokens
			case cat.Kind == "free" || cat.IsDeferred || cat.Kind == "deferred":
			default:
				usedSum += cat.Tokens
			}
		}
		lv.used = c.TotalTokens
		if usedSum > 0 && (c.TotalTokens == 0 || c.TotalTokens >= c.MaxTokens) {
			lv.used = usedSum
		}
		lv.limit, warnAt = c.MaxTokens, c.MaxTokens-buffer-warnBuffer
		if autoOn {
			lv.limit -= buffer
		}
		lv.measured = true
	} else {
		if u.used == 0 {
			return lv
		}
		window := u.window
		if window == 0 {
			window = defaultWindow
		}
		lv.used, lv.limit, warnAt = u.used, window, window-autocompactBuffer-warnBuffer
		if autoOn {
			lv.limit -= autocompactBuffer
		}
	}
	if lv.limit <= 0 {
		return lv
	}
	left := float64(lv.limit-lv.used) / float64(lv.limit) * 100
	lv.pctLeft = int(math.Max(0, math.Round(left)))
	// The countdown starts at the same usage whether auto-compact is on or not.
	lv.show = lv.used >= warnAt
	return lv
}

// autoCompactOn reads autoCompactEnabled and the variables that turn compaction off.
func autoCompactOn(ctx ext.Ctx) bool {
	for _, k := range []string{"DISABLE_AUTO_COMPACT", "DISABLE_COMPACT"} {
		if v, err := strconv.ParseBool(os.Getenv(k)); err == nil && v {
			return false
		}
	}
	return ext.ClaudeBool(ctx.Settings(), "autoCompactEnabled", true)
}

// contextWarning shows how much context is left before auto-compact, once it is low.
type contextWarning struct{ f *feature }

func (c *contextWarning) ID() string                      { return contextComponentID }
func (c *contextWarning) Init(ext.Ctx) tea.Cmd            { return nil }
func (c *contextWarning) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }

// TerminalState asks for focus reports, which drive the away recap.
func (c *contextWarning) TerminalState(ctx ext.Ctx) ext.TerminalState {
	return ext.TerminalState{ReportFocus: awayEnabled(ctx)}
}

func (c *contextWarning) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	lv := c.f.usage(ext.MainEngine).level(autoCompactOn(ctx))
	if !lv.show {
		return ext.Rendered{}
	}
	th := ctx.Theme()
	var text string
	if lv.autoOn {
		text = th.Paint(theme.Inactive, fmt.Sprintf("%d%% left until auto-compact", lv.pctLeft))
	} else {
		text = th.Paint(theme.Error, fmt.Sprintf("Context almost full (%d%% left) · /compact frees space", lv.pctLeft))
	}
	text = fit(text, a.Width)
	return ext.Rendered{Text: pad("", max(a.Width-visibleWidth(text), 0)) + text}
}
