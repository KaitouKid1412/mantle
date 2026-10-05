package sessions

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// DialogUsage is the /usage panel (aliases /cost, /stats).
const DialogUsage = "dialog.usage"

func (f *feature) registerUsage(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("usage"), Name: "usage", Aliases: []string{"cost", "stats"}, Source: ext.SourceBuiltin,
		Description: "Session cost, plan limits and activity",
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			return ctx.OpenDialog(DialogUsage, nil)
		},
	})
	r.AddDialog(DialogUsage, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		return &usagePanel{f: f, period: "day"}, nil
	})
}

// usageReport is the get_usage reply, decoded to the parts the panel draws.
type usageReport struct {
	Session struct {
		TotalCostUSD       float64                     `json:"total_cost_usd"`
		TotalAPIDurationMS int64                       `json:"total_api_duration_ms"`
		TotalDurationMS    int64                       `json:"total_duration_ms"`
		TotalLinesAdded    int64                       `json:"total_lines_added"`
		TotalLinesRemoved  int64                       `json:"total_lines_removed"`
		ModelUsage         map[string]proto.ModelUsage `json:"model_usage"`
	} `json:"session"`
	SubscriptionType    *string `json:"subscription_type"`
	RateLimitsAvailable bool    `json:"rate_limits_available"`
	RateLimits          *struct {
		FiveHour          *limitWindow `json:"five_hour"`
		SevenDay          *limitWindow `json:"seven_day"`
		SevenDayOAuthApps *limitWindow `json:"seven_day_oauth_apps"`
		SevenDayOpus      *limitWindow `json:"seven_day_opus"`
		SevenDaySonnet    *limitWindow `json:"seven_day_sonnet"`
		ModelScoped       []struct {
			DisplayName string   `json:"display_name"`
			Utilization *float64 `json:"utilization"`
			ResetsAt    *string  `json:"resets_at"`
		} `json:"model_scoped"`
	} `json:"rate_limits"`
	Behaviors *struct {
		Day  *behaviorWindow `json:"day"`
		Week *behaviorWindow `json:"week"`
	} `json:"behaviors"`
}

type limitWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type behaviorWindow struct {
	RequestCount int          `json:"request_count"`
	SessionCount int          `json:"session_count"`
	Behaviors    []namedShare `json:"behaviors"`
	Agents       []namedShare `json:"agents"`
	Skills       []namedShare `json:"skills"`
	Plugins      []namedShare `json:"plugins"`
	MCPServers   []namedShare `json:"mcp_servers"`
}

type namedShare struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Pct   float64 `json:"pct"`
	Count int     `json:"count"`
}

// localStats are totals from the session index for one period.
type localStats struct {
	Sessions, Messages int
	Totals             sessions.CostTotals
	WithCost           int
}

type usageLoadedMsg struct {
	report *usageReport
	err    error
}

type localStatsMsg struct {
	day, week localStats
}

// usagePanel draws the session, plan and activity sections (Settings context: d/w
// period, r refresh, t sort models by tokens, esc close).
type usagePanel struct {
	f         *feature
	period    string // "day" | "week"
	byTokens  bool
	report    *usageReport
	err       error
	loading   bool
	day, week *localStats
}

func (p *usagePanel) ID() string               { return DialogUsage }
func (p *usagePanel) KeyContext() string       { return ext.ContextSettings }
func (p *usagePanel) Placement() ext.Placement { return ext.PlaceAltScreen }

func (p *usagePanel) Init(ctx ext.Ctx) tea.Cmd { return tea.Batch(p.fetch(ctx), p.localStats(ctx)) }

func (p *usagePanel) fetch(ctx ext.Ctx) tea.Cmd {
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil || !eng.Supports(proto.SubGetUsage) {
		return nil
	}
	p.loading = true
	return controlCmd(eng.Control(proto.SubGetUsage, proto.GetUsageRequest{}), func(r ext.ControlResultMsg) tea.Msg {
		var rep usageReport
		err := decodeControl(r, &rep)
		return ext.AddressedMsg{To: DialogUsage, Msg: usageLoadedMsg{report: &rep, err: err}}
	})
}

func (p *usagePanel) localStats(ctx ext.Ctx) tea.Cmd {
	ix, now := p.f.index, ctx.Clock().Now()
	return func() tea.Msg {
		all, _ := ix.All()
		_ = ix.Save()
		return ext.AddressedMsg{To: DialogUsage, Msg: localStatsMsg{
			day:  statsSince(all, now.Add(-24*time.Hour)),
			week: statsSince(all, now.Add(-7*24*time.Hour)),
		}}
	}
}

func statsSince(ms []sessions.SessionMeta, since time.Time) localStats {
	var s localStats
	for _, m := range ms {
		if m.LastActive.Before(since) {
			continue
		}
		s.Sessions++
		s.Messages += m.MessageCount
		if m.Cost != nil {
			s.WithCost++
			s.Totals.Add(*m.Cost)
		}
	}
	return s
}

func (p *usagePanel) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case usageLoadedMsg:
		p.loading = false
		p.report, p.err = m.report, m.err
		ctx.Invalidate(DialogUsage)
	case localStatsMsg:
		p.day, p.week = &m.day, &m.week
		ctx.Invalidate(DialogUsage)
	}
	return nil
}

func (p *usagePanel) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (p *usagePanel) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	defer ctx.Invalidate(DialogUsage)
	switch a {
	case ext.ActSettingsPeriodDay:
		p.period = "day"
	case ext.ActSettingsPeriodWeek:
		p.period = "week"
	case ext.ActSettingsSortByTokens:
		p.byTokens = !p.byTokens
	case ext.ActSettingsRetry:
		return true, tea.Batch(p.fetch(ctx), p.localStats(ctx))
	case ext.ActConfirmNo, ext.ActAppInterrupt, ext.ActSelectCancel:
		return true, ctx.CloseDialog(DialogUsage)
	default:
		return false, nil
	}
	return true, nil
}

func (p *usagePanel) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "d":
		return p.HandleAction(ctx, ext.ActSettingsPeriodDay)
	case "w":
		return p.HandleAction(ctx, ext.ActSettingsPeriodWeek)
	case "t":
		return p.HandleAction(ctx, ext.ActSettingsSortByTokens)
	case "r":
		return p.HandleAction(ctx, ext.ActSettingsRetry)
	case "esc", "q":
		return p.HandleAction(ctx, ext.ActConfirmNo)
	}
	return true, nil
}

func (p *usagePanel) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 30)
	head := func(s string) string { return th.Fg(theme.Text).Bold(true).Render(s) }
	dim := func(s string) string { return th.Paint(theme.Inactive, s) }
	lines := []string{th.Fg(theme.Accent).Bold(true).Render("Usage"), ""}

	// Session.
	u := p.f.usage(ext.MainEngine)
	cost, api, wall := u.costUSD, time.Duration(u.apiMS)*time.Millisecond, time.Duration(0)
	var added, removed int64
	models := u.models
	if r := p.report; r != nil && p.err == nil {
		cost, api, wall = r.Session.TotalCostUSD, ms(r.Session.TotalAPIDurationMS), ms(r.Session.TotalDurationMS)
		added, removed = r.Session.TotalLinesAdded, r.Session.TotalLinesRemoved
		if len(r.Session.ModelUsage) > 0 {
			models = r.Session.ModelUsage
		}
	}
	lines = append(lines, head("This session"))
	summary := []string{fmt.Sprintf("Cost $%.2f", cost)}
	if api > 0 {
		summary = append(summary, "API time "+shortDuration(api))
	}
	if wall > 0 {
		summary = append(summary, "session "+shortDuration(wall))
	}
	if added+removed > 0 {
		summary = append(summary, fmt.Sprintf("+%d −%d lines", added, removed))
	}
	lines = append(lines, "  "+strings.Join(summary, dim(" · ")))
	lines = append(lines, p.modelLines(th, models, w)...)
	if p.report == nil && p.f.usage(ext.MainEngine).turns == 0 {
		lines = append(lines, dim("  No turns yet in this session."))
	}
	lines = append(lines, "")

	// Plan limits.
	if r := p.report; r != nil && r.RateLimitsAvailable && r.RateLimits != nil {
		lines = append(lines, head("Plan usage"))
		rl := r.RateLimits
		for _, row := range []struct {
			label string
			win   *limitWindow
		}{
			{"Current session (5h)", rl.FiveHour},
			{"This week, all models", rl.SevenDay},
			{"This week, Opus", rl.SevenDayOpus},
			{"This week, Sonnet", rl.SevenDaySonnet},
			{"This week, connected apps", rl.SevenDayOAuthApps},
		} {
			if row.win == nil || row.win.Utilization == nil {
				continue
			}
			lines = append(lines, limitLine(th, row.label, *row.win.Utilization, deref(row.win.ResetsAt), ctx.Clock().Now(), w))
		}
		for _, ms := range rl.ModelScoped {
			if ms.Utilization == nil {
				continue
			}
			lines = append(lines, limitLine(th, "This week, "+ms.DisplayName, *ms.Utilization, deref(ms.ResetsAt), ctx.Clock().Now(), w))
		}
		lines = append(lines, "")
	} else if p.loading {
		lines = append(lines, dim("Loading plan usage…"), "")
	} else if p.err != nil {
		lines = append(lines, th.Paint(theme.Error, "Could not load plan usage: "+p.err.Error()), "")
	}

	// Activity.
	label := map[string]string{"day": "Last 24 hours", "week": "Last 7 days"}[p.period]
	lines = append(lines, head("Activity · "+label))
	local := p.day
	if p.period == "week" {
		local = p.week
	}
	if local != nil {
		line := fmt.Sprintf("  %s on this machine", plural(local.Sessions, "session", "sessions"))
		if local.Messages > 0 {
			line += dim(" · ") + plural(local.Messages, "message", "messages")
		}
		if local.WithCost > 0 {
			line += dim(" · ") + fmt.Sprintf("$%.2f recorded", local.Totals.CostUSD)
		}
		lines = append(lines, line)
	}
	if r := p.report; r != nil && r.Behaviors != nil {
		bw := r.Behaviors.Day
		if p.period == "week" {
			bw = r.Behaviors.Week
		}
		if bw != nil {
			lines = append(lines, fmt.Sprintf("  %s across %s", plural(bw.RequestCount, "request", "requests"), plural(bw.SessionCount, "session", "sessions")))
			for _, g := range []struct {
				name  string
				items []namedShare
			}{{"Skills", bw.Skills}, {"Agents", bw.Agents}, {"Plugins", bw.Plugins}, {"MCP servers", bw.MCPServers}} {
				if s := topShares(g.items, 3); s != "" {
					lines = append(lines, fit("  "+g.name+": "+s, w))
				}
			}
		}
	}
	lines = append(lines, "", dim("d day · w week · t sort by tokens · r refresh · esc close"))
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

func ms(v int64) time.Duration { return time.Duration(v) * time.Millisecond }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// modelLines is the per-model table, by cost (or tokens).
func (p *usagePanel) modelLines(th *theme.Theme, models map[string]proto.ModelUsage, w int) []string {
	if len(models) == 0 {
		return nil
	}
	names := make([]string, 0, len(models))
	for n := range models {
		names = append(names, n)
	}
	tokens := func(m proto.ModelUsage) int64 {
		return m.InputTokens + m.OutputTokens + m.CacheReadInputTokens + m.CacheCreationInputTokens
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := models[names[i]], models[names[j]]
		if p.byTokens {
			if tokens(a) != tokens(b) {
				return tokens(a) > tokens(b)
			}
		} else if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return names[i] < names[j]
	})
	out := []string{}
	for _, n := range names {
		m := models[n]
		line := fmt.Sprintf("  %s  %s in · %s out · %s cache read · %s cache write · $%.2f",
			n, compactTokens(m.InputTokens), compactTokens(m.OutputTokens),
			compactTokens(m.CacheReadInputTokens), compactTokens(m.CacheCreationInputTokens), m.CostUSD)
		out = append(out, fit(th.Paint(theme.Inactive, line), w))
	}
	return out
}

// limitLine draws one plan window as a bar.
func limitLine(th *theme.Theme, label string, util float64, resets string, now time.Time, w int) string {
	if util <= 1 && util > 0 {
		util *= 100
	}
	util = min(max(util, 0), 100)
	const width = 20
	filled := int(util/100*width + 0.5)
	bar := th.Paint(theme.RateLimitFill, strings.Repeat("█", filled)) + th.Paint(theme.RateLimitEmpty, strings.Repeat("░", width-filled))
	text := fmt.Sprintf("  %-26s %s %3.0f%% used", label, bar, util)
	if t, err := time.Parse(time.RFC3339, resets); err == nil {
		text += th.Paint(theme.Inactive, " · resets "+resetText(now, t))
	}
	return fit(text, w)
}

func resetText(now, t time.Time) string {
	t = t.Local()
	if t.Sub(now) < 24*time.Hour && t.YearDay() == now.Local().YearDay() {
		return t.Format("3:04pm")
	}
	return t.Format("Mon 3:04pm")
}

func topShares(items []namedShare, n int) string {
	var parts []string
	for i, it := range items {
		if i == n {
			break
		}
		name := it.Name
		if name == "" {
			name = it.Key
		}
		parts = append(parts, fmt.Sprintf("%s %.0f%%", name, it.Pct))
	}
	return strings.Join(parts, ", ")
}

// shortDuration prints 45s, 3m 12s, 1h 4m.
func shortDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}
