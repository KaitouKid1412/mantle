// Package statusline runs the user's statusLine and subagentStatusLine commands and
// builds the JSON they read on stdin.
//
// Field names match Claude Code's status line payload exactly, so existing scripts work
// unchanged. Fields mantle cannot compute are omitted, never faked.
package statusline

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// Payload is the JSON object a statusLine command reads on stdin. Optional groups are
// pointers with omitempty; fields Claude Code documents as nullable are pointers
// without omitempty, so they marshal as null.
type Payload struct {
	Cwd            string         `json:"cwd"`
	SessionID      string         `json:"session_id"`
	SessionName    string         `json:"session_name,omitempty"`
	PromptID       string         `json:"prompt_id,omitempty"`
	TranscriptPath string         `json:"transcript_path"`
	Model          *Model         `json:"model,omitempty"`
	Workspace      Workspace      `json:"workspace"`
	Version        string         `json:"version,omitempty"`
	OutputStyle    *OutputStyle   `json:"output_style,omitempty"`
	Cost           *Cost          `json:"cost,omitempty"`
	ContextWindow  *ContextWindow `json:"context_window,omitempty"`
	Exceeds200k    *bool          `json:"exceeds_200k_tokens,omitempty"`
	PromptCache    *PromptCache   `json:"prompt_cache,omitempty"`
	FastMode       *bool          `json:"fast_mode,omitempty"`
	Effort         *Effort        `json:"effort,omitempty"`
	Thinking       *Thinking      `json:"thinking,omitempty"`
	RateLimits     *RateLimits    `json:"rate_limits,omitempty"`
	Vim            *Vim           `json:"vim,omitempty"`
	Agent          *Agent         `json:"agent,omitempty"`
	PR             *PR            `json:"pr,omitempty"`
	Worktree       *Worktree      `json:"worktree,omitempty"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir  string   `json:"current_dir"`
	ProjectDir  string   `json:"project_dir"`
	AddedDirs   []string `json:"added_dirs"` // [] when none, never null
	GitWorktree string   `json:"git_worktree,omitempty"`
	Repo        *Repo    `json:"repo,omitempty"`
}

type Repo struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type OutputStyle struct {
	Name string `json:"name"`
}

type Cost struct {
	TotalCostUSD       float64 `json:"total_cost_usd"`
	TotalDurationMS    int64   `json:"total_duration_ms"`
	TotalAPIDurationMS int64   `json:"total_api_duration_ms"`
	TotalLinesAdded    int     `json:"total_lines_added"`
	TotalLinesRemoved  int     `json:"total_lines_removed"`
}

type ContextWindow struct {
	TotalInputTokens    int      `json:"total_input_tokens"`
	TotalOutputTokens   int      `json:"total_output_tokens"`
	ContextWindowSize   int      `json:"context_window_size"`
	UsedPercentage      *float64 `json:"used_percentage"`
	RemainingPercentage *float64 `json:"remaining_percentage"`
	CurrentUsage        *Usage   `json:"current_usage"`
}

// Usage is the token usage of one API response.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// PromptCache mirrors Claude Code's prompt_cache object. mantle does not compute it yet,
// so the builder never sets it; the type exists so a later source can.
type PromptCache struct {
	Warm                bool           `json:"warm"`
	CachingObserved     bool           `json:"caching_observed"`
	TTL                 string         `json:"ttl"`
	ExpiresAt           *int64         `json:"expires_at"`
	Requests            int            `json:"requests"`
	Misses              int            `json:"misses"`
	ExpectedRebuilds    int            `json:"expected_rebuilds"`
	HitRatio            *float64       `json:"hit_ratio"`
	CacheWriteTokens    int            `json:"cache_write_tokens"`
	MissRecacheTokens   int            `json:"miss_recache_tokens"`
	LastMissAt          *int64         `json:"last_miss_at"`
	LastMissCause       *MissCause     `json:"last_miss_cause"`
	MissCauses          map[string]int `json:"miss_causes"`
	RecacheTokensIfCold *int           `json:"recache_tokens_if_cold"`
}

type MissCause struct {
	Causes       []string `json:"causes"`
	ToolsAdded   *int     `json:"tools_added,omitempty"`
	ToolsRemoved *int     `json:"tools_removed,omitempty"`
}

type Effort struct {
	Level string `json:"level"`
}

type Thinking struct {
	Enabled bool `json:"enabled"`
}

type RateLimits struct {
	FiveHour   *RateWindow `json:"five_hour,omitempty"`
	SevenDay   *RateWindow `json:"seven_day,omitempty"`
	SpendLimit *SpendLimit `json:"spend_limit,omitempty"`
}

type RateWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

type SpendLimit struct {
	UsedPercentage float64  `json:"used_percentage"`
	ResetsAt       int64    `json:"resets_at"`
	UsedUSD        *float64 `json:"used_usd,omitempty"`
	LimitUSD       *float64 `json:"limit_usd,omitempty"`
	Period         string   `json:"period,omitempty"`
}

type Vim struct {
	Mode string `json:"mode"`
}

type Agent struct {
	Name string `json:"name"`
}

type PR struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	ReviewState string `json:"review_state,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type Worktree struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	Branch         string `json:"branch,omitempty"`
	OriginalCwd    string `json:"original_cwd"`
	OriginalBranch string `json:"original_branch,omitempty"`
}

// Input holds the plain values the builder turns into a Payload. Zero values mean
// "unknown" and leave the field out (or null, where Claude Code sends null).
type Input struct {
	SessionID      string
	SessionName    string // custom (/rename, -n) or AI title; not the default display name
	PromptID       string
	TranscriptPath string
	Cwd            string
	ProjectDir     string // defaults to Cwd
	AddedDirs      []string
	GitWorktree    string
	Repo           *Repo
	Version        string // engine version

	ModelID, ModelDisplayName string
	OutputStyle               string
	Effort                    string // "" when the model has no effort parameter
	Thinking                  *bool
	FastMode                  *bool

	// Cost totals; HaveCost says they are known (a result arrived, or restored totals).
	HaveCost          bool
	CostUSD           float64
	DurationMS        int64
	APIDurationMS     int64
	LinesAdded        int
	LinesRemoved      int
	HaveContextWindow bool   // the context window block is known (model known)
	ContextWindowSize int    // 0 = 200000
	LastUsage         *Usage // usage of the most recent main-conversation response; nil before one
	RateLimits        *RateLimits
	PromptCache       *PromptCache
	VimMode           string // "" when vim mode is off
	AgentName         string
	PR                *PR
	Worktree          *Worktree
}

// DefaultContextWindow is the context size when the model's is unknown.
const DefaultContextWindow = 200_000

// Build assembles a Payload from in.
func Build(in Input) Payload {
	p := Payload{
		Cwd:            in.Cwd,
		SessionID:      in.SessionID,
		SessionName:    in.SessionName,
		PromptID:       in.PromptID,
		TranscriptPath: in.TranscriptPath,
		Version:        in.Version,
		Workspace: Workspace{
			CurrentDir:  in.Cwd,
			ProjectDir:  in.ProjectDir,
			AddedDirs:   append([]string{}, in.AddedDirs...),
			GitWorktree: in.GitWorktree,
			Repo:        in.Repo,
		},
		FastMode:   in.FastMode,
		RateLimits: in.RateLimits,
		PR:         in.PR,
		Worktree:   in.Worktree,
	}
	if p.Workspace.ProjectDir == "" {
		p.Workspace.ProjectDir = in.Cwd
	}
	if in.ModelID != "" || in.ModelDisplayName != "" {
		name := in.ModelDisplayName
		if name == "" {
			name = in.ModelID
		}
		p.Model = &Model{ID: in.ModelID, DisplayName: name}
	}
	if in.OutputStyle != "" {
		p.OutputStyle = &OutputStyle{Name: in.OutputStyle}
	}
	if in.HaveCost {
		p.Cost = &Cost{
			TotalCostUSD:       in.CostUSD,
			TotalDurationMS:    in.DurationMS,
			TotalAPIDurationMS: in.APIDurationMS,
			TotalLinesAdded:    in.LinesAdded,
			TotalLinesRemoved:  in.LinesRemoved,
		}
	}
	if in.HaveContextWindow || in.LastUsage != nil {
		p.ContextWindow, p.Exceeds200k = contextWindow(in)
	}
	if in.Effort != "" {
		p.Effort = &Effort{Level: in.Effort}
	}
	if in.Thinking != nil {
		p.Thinking = &Thinking{Enabled: *in.Thinking}
	}
	p.PromptCache = in.PromptCache
	if in.VimMode != "" {
		p.Vim = &Vim{Mode: in.VimMode}
	}
	if in.AgentName != "" {
		p.Agent = &Agent{Name: in.AgentName}
	}
	return p
}

func contextWindow(in Input) (*ContextWindow, *bool) {
	size := in.ContextWindowSize
	if size <= 0 {
		size = DefaultContextWindow
	}
	cw := &ContextWindow{ContextWindowSize: size}
	exceeds := false
	if u := in.LastUsage; u != nil {
		usage := *u
		cw.CurrentUsage = &usage
		cw.TotalInputTokens = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
		cw.TotalOutputTokens = u.OutputTokens
		used := math.Round(float64(cw.TotalInputTokens) / float64(size) * 100)
		used = max(0, min(100, used))
		remaining := 100 - used
		cw.UsedPercentage, cw.RemainingPercentage = &used, &remaining
		// A fixed threshold, whatever the model's window.
		exceeds = cw.TotalInputTokens+cw.TotalOutputTokens > 200_000
	}
	return cw, &exceeds
}

// Marshal encodes the payload as the command's stdin.
func (p Payload) Marshal() ([]byte, error) { return json.Marshal(p) }

// KeySet returns the sorted, dot-joined key paths of a JSON object ("model.id",
// "workspace.repo.host", …). Arrays contribute their own path only. It is used to
// compare mantle's payload with a recorded Claude Code payload.
func KeySet(data []byte) ([]string, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	var keys []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		m, ok := v.(map[string]any)
		if !ok {
			if prefix != "" {
				keys = append(keys, prefix)
			}
			return
		}
		if len(m) == 0 && prefix != "" {
			keys = append(keys, prefix)
		}
		for k, child := range m {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			walk(path, child)
		}
	}
	walk("", v)
	sort.Strings(keys)
	return keys, nil
}

// effortLevels are the documented effort strings; others pass through unchanged.
var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// NormalizeEffort lower-cases a known effort level.
func NormalizeEffort(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	for _, e := range effortLevels {
		if l == e {
			return e
		}
	}
	return strings.TrimSpace(s)
}
