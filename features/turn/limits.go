package turn

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// continuePrompt is sent when a usage-limit wait ends. Headless claude has no
// auto-continue of its own, so mantle does the waiting.
const continuePrompt = "continue"

// limitWait is a pending auto-continue after a usage limit.
type limitWait struct {
	engineID string
	until    time.Time
	gen      int
}

type limitTickMsg struct{ gen int }

type limitArgs struct {
	engineID string
	until    time.Time
}

func (st *state) setupLimits(r ext.Registrar) {
	r.AddDialog(DialogUsageLimit, st.usageLimitDialog)
	ext.Subscribe(r, "turn.limitTick", st.onLimitTick)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// onRateLimit offers to wait out a usage limit (or waits right away when
// autoContinueAtUsageLimit is set).
func (st *state) onRateLimit(c ext.Ctx, engineID string, ev *proto.RateLimitEvent) tea.Cmd {
	if !strings.EqualFold(ev.RateLimitInfo.Status, "rejected") {
		return nil
	}
	now := c.Clock().Now()
	until, ok := parseResetsAt(ev.RateLimitInfo.ResetsAt)
	if !ok || !until.After(now) {
		return c.Notify(ext.Notice{Key: "turn.usageLimit", Text: "Usage limit reached.", Level: ext.NoticeWarning, Source: FeatureID})
	}
	key := engineID + "@" + strconv.FormatInt(until.Unix(), 10)
	if st.seenLim[key] {
		return nil
	}
	st.seenLim[key] = true
	if b, _ := settingValue(c, "autoContinueAtUsageLimit").(bool); b {
		return st.startLimitWait(c, engineID, until)
	}
	return c.OpenDialog(DialogUsageLimit, limitArgs{engineID: engineID, until: until})
}

func (st *state) usageLimitDialog(c ext.Ctx, args any) (ext.Dialog, error) {
	la, _ := args.(limitArgs)
	ch := dialogs.NewUsageLimit(formatReset(la.until, c.Clock().Now()))
	d := &vmDialog{id: DialogUsageLimit, vm: ch, contexts: []string{ext.ContextConfirmation}, place: ext.PlaceInline}
	d.result = func() any { return ch.Chosen() }
	d.onDone = func(c ext.Ctx) tea.Cmd {
		closeCmd := c.CloseDialog(DialogUsageLimit)
		switch ch.Chosen() {
		case dialogs.LimitWait:
			if la.until.IsZero() {
				return closeCmd
			}
			return tea.Batch(closeCmd, st.startLimitWait(c, engineKey(la.engineID), la.until))
		case dialogs.LimitModel:
			return tea.Batch(closeCmd, c.Run(ext.ActChatModelPicker))
		}
		return closeCmd
	}
	return d, nil
}

func (st *state) startLimitWait(c ext.Ctx, engineID string, until time.Time) tea.Cmd {
	st.limitGen++
	st.limit = &limitWait{engineID: engineID, until: until, gen: st.limitGen}
	gen := st.limitGen
	now := c.Clock().Now()
	notice := c.Notify(ext.Notice{
		Key:     "turn.usageLimit",
		Text:    "Usage limit reached. Continuing at " + formatReset(until, now) + " (esc to cancel)",
		Level:   ext.NoticeWarning,
		Timeout: -1,
		Source:  FeatureID,
	})
	tick := c.Clock().Tick(until.Sub(now)+time.Second, func(time.Time) tea.Msg { return limitTickMsg{gen: gen} })
	return tea.Batch(notice, tick)
}

func (st *state) onLimitTick(c ext.Ctx, m limitTickMsg) tea.Cmd {
	w := st.limit
	if w == nil || w.gen != m.gen {
		return nil
	}
	st.limit = nil
	notice := c.Notify(ext.Notice{Key: "turn.usageLimit", Text: "Usage limit reset; continuing.", Level: ext.NoticeInfo, Timeout: 3 * time.Second, Source: FeatureID})
	eng := c.Engine(w.engineID)
	if eng == nil {
		return notice
	}
	return tea.Batch(notice, eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(continuePrompt)}}))
}

func (st *state) cancelLimitWait(c ext.Ctx) tea.Cmd {
	st.limit = nil
	return c.Notify(ext.Notice{Key: "turn.usageLimit", Text: "Stopped waiting for the usage limit.", Level: ext.NoticeInfo, Timeout: 2 * time.Second, Source: FeatureID})
}

// parseResetsAt reads resetsAt as Unix seconds, Unix milliseconds or an RFC 3339 time.
func parseResetsAt(raw json.RawMessage) (time.Time, bool) {
	if len(raw) == 0 {
		return time.Time{}, false
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(int64(n)), true
		}
		return time.Unix(int64(n), 0), true
	}
	var s string
	if json.Unmarshal(raw, &s) != nil || s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return parseResetsAt(json.RawMessage(strconv.FormatFloat(f, 'f', 0, 64)))
	}
	return time.Time{}, false
}

// formatReset shows a reset time: "3:04 PM" today, "Jan 2 3:04 PM" otherwise.
func formatReset(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(now.Location())
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("3:04 PM")
	}
	return t.Format("Jan 2 3:04 PM")
}
