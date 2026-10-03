package chrome

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// slData accumulates everything the statusLine payload needs from the main session.
type slData struct {
	s     sessionState
	start time.Time // when this session started (wall-clock duration)

	haveResult    bool
	costUSD       float64
	apiMS         int64
	linesAdded    int
	linesRemoved  int
	lastUsage     *statusline.Usage
	contextWindow int
	rate          *statusline.RateLimits
	repo          *statusline.Repo
	pr            *statusline.PR
	configDir     string // CLAUDE_CONFIG_DIR or ~/.claude
}

func newSLData() *slData { return &slData{s: newSessionState()} }

// observe folds msg in. changed: the payload differs; rerun: Claude Code would re-run
// the command for this event (session start, a new assistant message, compaction, a
// permission-mode or vim-mode change).
func (d *slData) observe(now time.Time, msg tea.Msg) (changed, rerun bool) {
	before := d.s
	if d.s.observe(msg) {
		changed = true
		rerun = before.Mode != d.s.Mode || before.Vim != d.s.Vim || before.SessionID != d.s.SessionID ||
			before.Model != d.s.Model || before.Name() != d.s.Name()
	}
	if before.SessionID != d.s.SessionID && d.s.SessionID != "" {
		d.resetSession(now)
	}
	if d.start.IsZero() && d.s.SessionID != "" {
		d.start = now
	}
	m, ok := msg.(ext.EngineEventMsg)
	if !ok || !isMain(m.EngineID) {
		return changed, rerun
	}
	switch e := m.Event.(type) {
	case *proto.Assistant:
		if e.ParentToolUseID == "" && e.Message.Usage != nil && e.Message.Model != proto.SyntheticModel {
			u := e.Message.Usage
			d.lastUsage = &statusline.Usage{
				InputTokens:              int(u.InputTokens),
				OutputTokens:             int(u.OutputTokens),
				CacheCreationInputTokens: int(u.CacheCreationInputTokens),
				CacheReadInputTokens:     int(u.CacheReadInputTokens),
			}
		}
		return true, true
	case *proto.User:
		for _, r := range e.ToolResults() {
			if !r.IsError && d.countLines(r.Structured) {
				changed = true
			}
		}
	case *proto.Result:
		d.haveResult = true
		if e.TotalCostUSD > 0 {
			d.costUSD = e.TotalCostUSD
		}
		d.apiMS += e.DurationAPIMS
		if mu, ok := e.ModelUsage[d.s.Model]; ok && mu.ContextWindow > 0 {
			d.contextWindow = int(mu.ContextWindow)
		} else {
			for _, mu := range e.ModelUsage {
				d.contextWindow = max(d.contextWindow, int(mu.ContextWindow))
			}
		}
		return true, true
	case *proto.CompactBoundary:
		d.lastUsage = nil // current_usage is null after /compact until the next response
		return true, true
	case *proto.RateLimitEvent:
		if d.applyRateLimit(e.RateLimitInfo, now) {
			return true, false
		}
	case *proto.ConversationReset:
		d.resetSession(now)
		return true, true
	}
	return changed, rerun
}

func (d *slData) resetSession(now time.Time) {
	d.start = now
	d.haveResult, d.costUSD, d.apiMS = false, 0, 0
	d.linesAdded, d.linesRemoved = 0, 0
	d.lastUsage = nil
}

// countLines adds Edit/Write/MultiEdit line counts from a structured tool result.
func (d *slData) countLines(raw json.RawMessage) bool {
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var r struct {
		Type            string `json:"type"`
		Content         string `json:"content"`
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return false
	}
	added, removed := 0, 0
	for _, h := range r.StructuredPatch {
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "+"):
				added++
			case strings.HasPrefix(l, "-"):
				removed++
			}
		}
	}
	if len(r.StructuredPatch) == 0 && r.Type == "create" && r.Content != "" {
		added = strings.Count(strings.TrimSuffix(r.Content, "\n"), "\n") + 1
	}
	d.linesAdded += added
	d.linesRemoved += removed
	return added+removed > 0
}

func (d *slData) applyRateLimit(info proto.RateLimitInfo, now time.Time) bool {
	var resets int64
	_ = json.Unmarshal(info.ResetsAt, &resets)
	if info.RateLimitType == "" || resets == 0 {
		return false
	}
	pct := info.Utilization
	if pct <= 1 {
		pct *= 100 // the engine reports a fraction
	}
	w := &statusline.RateWindow{UsedPercentage: pct, ResetsAt: resets}
	if d.rate == nil {
		d.rate = &statusline.RateLimits{}
	}
	switch info.RateLimitType {
	case "five_hour":
		d.rate.FiveHour = w
	case "seven_day":
		d.rate.SevenDay = w
	default:
		return false
	}
	return true
}

// dropExpired removes rate-limit windows whose reset time passed, as Claude Code does.
func (d *slData) dropExpired(now time.Time) bool {
	if d.rate == nil {
		return false
	}
	changed := false
	if w := d.rate.FiveHour; w != nil && now.Unix() >= w.ResetsAt {
		d.rate.FiveHour, changed = nil, true
	}
	if w := d.rate.SevenDay; w != nil && now.Unix() >= w.ResetsAt {
		d.rate.SevenDay, changed = nil, true
	}
	if d.rate.FiveHour == nil && d.rate.SevenDay == nil && d.rate.SpendLimit == nil {
		d.rate = nil
	}
	return changed
}

// input builds the builder input.
func (d *slData) input(now time.Time) statusline.Input {
	in := statusline.Input{
		SessionID:         d.s.SessionID,
		SessionName:       d.s.Name(),
		TranscriptPath:    transcriptPath(d.configDir, d.s.ProjectDir, d.s.SessionID),
		Cwd:               d.s.Cwd,
		ProjectDir:        d.s.ProjectDir,
		Repo:              d.repo,
		Version:           d.s.Version,
		ModelID:           d.s.Model,
		ModelDisplayName:  modelDisplayName(d.s.Model),
		OutputStyle:       d.s.OutputStyle,
		Effort:            statusline.NormalizeEffort(d.s.Effort),
		HaveContextWindow: d.s.Model != "",
		ContextWindowSize: d.contextWindow,
		LastUsage:         d.lastUsage,
		RateLimits:        d.rate,
		VimMode:           d.s.Vim,
		PR:                d.pr,
	}
	switch d.s.FastMode {
	case "on":
		in.FastMode = ptrTo(true)
	case "off":
		in.FastMode = ptrTo(false)
	}
	if d.haveResult || !d.start.IsZero() {
		in.HaveCost = true
		in.CostUSD = d.costUSD
		if !d.start.IsZero() {
			in.DurationMS = now.Sub(d.start).Milliseconds()
		}
		in.APIDurationMS = d.apiMS
		in.LinesAdded, in.LinesRemoved = d.linesAdded, d.linesRemoved
	}
	return in
}

func ptrTo[T any](v T) *T { return &v }

// modelDisplayName turns a model ID into a short name ("claude-opus-5-5" → "Opus 5.5").
// Unknown shapes are returned unchanged.
func modelDisplayName(id string) string {
	base, suffix, _ := strings.Cut(id, "[")
	parts := strings.Split(strings.TrimPrefix(base, "claude-"), "-")
	if len(parts) < 2 || !strings.HasPrefix(base, "claude-") {
		return id
	}
	family := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	var ver []string
	for _, p := range parts[1:] {
		if len(p) > 2 { // a date stamp
			break
		}
		ver = append(ver, p)
	}
	name := family
	if len(ver) > 0 {
		name += " " + strings.Join(ver, ".")
	}
	if suffix != "" {
		name += " (" + strings.TrimSuffix(suffix, "]") + " context)"
	}
	return name
}

// transcriptPath is Claude Code's JSONL path for a session:
// <config>/projects/<cwd with every non-alphanumeric character replaced by '-'>/<id>.jsonl.
// TODO(07): use plan 06's internal/sessions path helper once it is merged.
func transcriptPath(configDir, projectDir, sessionID string) string {
	if sessionID == "" || projectDir == "" {
		return ""
	}
	if configDir == "" {
		configDir = claudeConfigDir(terminal.OS())
	}
	slug := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, projectDir)
	return filepath.Join(configDir, "projects", slug, sessionID+".jsonl")
}

func claudeConfigDir(env terminal.Env) string {
	if dir := terminal.Get(env, "CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}
