package sessions

import (
	"sort"
	"time"
)

// CostTotals are cost, time and token totals for one session or a group of sessions.
type CostTotals struct {
	CostUSD                   float64               `json:"costUSD"`
	APIDuration               time.Duration         `json:"apiDuration,omitempty"`
	APIDurationWithoutRetries time.Duration         `json:"apiDurationWithoutRetries,omitempty"`
	ToolDuration              time.Duration         `json:"toolDuration,omitempty"`
	Duration                  time.Duration         `json:"duration,omitempty"`
	LinesAdded                int64                 `json:"linesAdded,omitempty"`
	LinesRemoved              int64                 `json:"linesRemoved,omitempty"`
	Models                    map[string]ModelUsage `json:"models,omitempty"`
	// UnknownModelCost is set when some usage had no known price, so CostUSD is a floor.
	UnknownModelCost bool `json:"unknownModelCost,omitempty"`
}

// Totals converts a cost-state record.
func (c *CostState) Totals() CostTotals {
	t := CostTotals{
		CostUSD: c.TotalCostUSD, APIDuration: c.APIDuration,
		APIDurationWithoutRetries: c.APIDurationWithoutRetries, ToolDuration: c.ToolDuration,
		Duration: c.Duration, LinesAdded: c.LinesAdded, LinesRemoved: c.LinesRemoved,
		UnknownModelCost: c.HasUnknownModelCost,
	}
	if len(c.ModelUsage) > 0 {
		t.Models = make(map[string]ModelUsage, len(c.ModelUsage))
		for k, v := range c.ModelUsage {
			t.Models[k] = v
		}
	}
	return t
}

// Add accumulates o into t.
func (t *CostTotals) Add(o CostTotals) {
	t.CostUSD += o.CostUSD
	t.APIDuration += o.APIDuration
	t.APIDurationWithoutRetries += o.APIDurationWithoutRetries
	t.ToolDuration += o.ToolDuration
	t.Duration += o.Duration
	t.LinesAdded += o.LinesAdded
	t.LinesRemoved += o.LinesRemoved
	t.UnknownModelCost = t.UnknownModelCost || o.UnknownModelCost
	for k, v := range o.Models {
		if t.Models == nil {
			t.Models = map[string]ModelUsage{}
		}
		u := t.Models[k]
		u.Add(v)
		t.Models[k] = u
	}
}

// Add accumulates o into u.
func (u *ModelUsage) Add(o ModelUsage) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.ThinkingTokens += o.ThinkingTokens
	u.CacheReadInputTokens += o.CacheReadInputTokens
	u.CacheCreationInputTokens += o.CacheCreationInputTokens
	u.WebSearchRequests += o.WebSearchRequests
	u.CostUSD += o.CostUSD
}

// Tokens is every input and output token, cache reads and writes included.
func (u ModelUsage) Tokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// Tokens is the token total across models.
func (t CostTotals) Tokens() int64 {
	var n int64
	for _, u := range t.Models {
		n += u.Tokens()
	}
	return n
}

// UsageFromEntries sums token usage per model from assistant records. It is the
// fallback when a transcript has no cost-state record (costs are left at zero: prices
// are the engine's business). The engine writes one record per content block, all
// repeating the message's usage, so each message id counts once (its last record).
func UsageFromEntries(entries []*Entry) map[string]ModelUsage {
	last := map[string]*Entry{}
	var order []string
	for _, e := range entries {
		if e.Kind() != KindAssistant || e.Message == nil || e.Message.Usage == nil || e.IsAPIErrorMessage {
			continue
		}
		id := e.Message.ID
		if id == "" {
			id = e.UUID
		}
		if _, ok := last[id]; !ok {
			order = append(order, id)
		}
		last[id] = e
	}
	out := map[string]ModelUsage{}
	for _, id := range order {
		e := last[id]
		u := e.Message.Usage
		m := out[e.Message.Model]
		m.Add(ModelUsage{
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadInputTokens: u.CacheReadInputTokens, CacheCreationInputTokens: u.CacheCreationInputTokens,
		})
		if u.ServerToolUse != nil {
			m.WebSearchRequests += u.ServerToolUse.WebSearchRequests
		}
		out[e.Message.Model] = m
	}
	return out
}

// SessionTotals is a transcript's totals: its last cost-state record if any, else
// token usage summed from its assistant records.
func SessionTotals(t *Transcript) CostTotals {
	if t.Meta.Cost != nil {
		return t.Meta.Cost.Totals()
	}
	return CostTotals{Models: UsageFromEntries(t.Entries)}
}

// Group is the totals of a set of sessions.
type Group struct {
	Key        string
	Sessions   int
	Messages   int
	First      time.Time // earliest Created
	Last       time.Time // latest LastActive
	Totals     CostTotals
	WithCost   int // sessions that had a cost-state record
	SessionIDs []string
}

func (g *Group) add(m SessionMeta) {
	g.Sessions++
	g.Messages += m.MessageCount
	g.SessionIDs = append(g.SessionIDs, m.ID)
	if g.First.IsZero() || m.Created.Before(g.First) {
		g.First = m.Created
	}
	if m.LastActive.After(g.Last) {
		g.Last = m.LastActive
	}
	if m.Cost != nil {
		g.WithCost++
		g.Totals.Add(*m.Cost)
	}
}

// GroupBy totals sessions by key(m), sorted by most recent activity first. Sessions
// for which key returns "" are skipped.
func GroupBy(metas []SessionMeta, key func(SessionMeta) string) []*Group {
	byKey := map[string]*Group{}
	for _, m := range metas {
		k := key(m)
		if k == "" {
			continue
		}
		g := byKey[k]
		if g == nil {
			g = &Group{Key: k}
			byKey[k] = g
		}
		g.add(m)
	}
	out := make([]*Group, 0, len(byKey))
	for _, g := range byKey {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ByProject keys sessions by their cwd (falling back to the project dir).
func ByProject(m SessionMeta) string {
	if m.Cwd != "" {
		return m.Cwd
	}
	return m.ProjectDir
}

// ByDay returns a key function grouping sessions by the local date (YYYY-MM-DD) of
// their last activity in loc.
func ByDay(loc *time.Location) func(SessionMeta) string {
	return func(m SessionMeta) string {
		if m.LastActive.IsZero() {
			return ""
		}
		return m.LastActive.In(loc).Format(time.DateOnly)
	}
}

// Total sums every session into one group.
func Total(metas []SessionMeta) *Group {
	g := &Group{Key: "all"}
	for _, m := range metas {
		g.add(m)
	}
	return g
}
