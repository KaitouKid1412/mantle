package statusline

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

// fullInput sets every field, so the payload has every optional group.
func fullInput() Input {
	return Input{
		SessionID:      "sess-1",
		SessionName:    "my-session",
		PromptID:       "prompt-1",
		TranscriptPath: "/work/.claude/projects/p/sess-1.jsonl",
		Cwd:            "/work/proj",
		ProjectDir:     "/work/proj",
		AddedDirs:      []string{"/work/other"},
		GitWorktree:    "feature-x",
		Repo:           &Repo{Host: "github.com", Owner: "acme", Name: "widgets"},
		Version:        "2.1.288",

		ModelID:          "claude-opus-5-5",
		ModelDisplayName: "Opus",
		OutputStyle:      "default",
		Effort:           "high",
		Thinking:         ptr(true),
		FastMode:         ptr(false),

		HaveCost:      true,
		CostUSD:       0.25,
		DurationMS:    45_000,
		APIDurationMS: 2_300,
		LinesAdded:    10,
		LinesRemoved:  2,

		HaveContextWindow: true,
		ContextWindowSize: 200_000,
		LastUsage: &Usage{InputTokens: 8_500, OutputTokens: 1_200,
			CacheCreationInputTokens: 5_000, CacheReadInputTokens: 2_000},
		RateLimits: &RateLimits{
			FiveHour:   &RateWindow{UsedPercentage: 23.5, ResetsAt: 1_700_000_000},
			SevenDay:   &RateWindow{UsedPercentage: 41.2, ResetsAt: 1_700_500_000},
			SpendLimit: &SpendLimit{UsedPercentage: 60, ResetsAt: 1_701_000_000, UsedUSD: ptr(30.0), LimitUSD: ptr(50.0), Period: "monthly"},
		},
		PromptCache: &PromptCache{Warm: true, CachingObserved: true, TTL: "5m",
			ExpiresAt: ptr(int64(1_700_000_300)), Requests: 4, HitRatio: ptr(0.9),
			LastMissAt:    ptr(int64(1_700_000_000)),
			LastMissCause: &MissCause{Causes: []string{"tools_changed"}, ToolsAdded: ptr(1), ToolsRemoved: ptr(0)},
			MissCauses:    map[string]int{"tools_changed": 1}, RecacheTokensIfCold: ptr(4_000)},
		VimMode:   "NORMAL",
		AgentName: "reviewer",
		PR:        &PR{Number: 12, URL: "https://github.com/acme/widgets/pull/12", ReviewState: "pending", Kind: "mr"},
		Worktree: &Worktree{Name: "feature-x", Path: "/work/proj/.claude/worktrees/feature-x",
			Branch: "worktree-feature-x", OriginalCwd: "/work/proj", OriginalBranch: "main"},
	}
}

func readKeys(t *testing.T) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "..", "testdata", "fixtures", "07", "statusline-keys.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var keys []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			keys = append(keys, l)
		}
	}
	slices.Sort(keys)
	return keys
}

// TestPayloadKeyParity: with every group present, mantle sends exactly Claude Code's key
// set. Intentional omissions: none.
func TestPayloadKeyParity(t *testing.T) {
	data, err := Build(fullInput()).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := KeySet(data)
	if err != nil {
		t.Fatal(err)
	}
	want := readKeys(t)
	if !slices.Equal(got, want) {
		for _, k := range want {
			if !slices.Contains(got, k) {
				t.Errorf("missing key %s", k)
			}
		}
		for _, k := range got {
			if !slices.Contains(want, k) {
				t.Errorf("extra key %s", k)
			}
		}
	}
}

// TestPayloadFreshSession: before any response, only known fields appear; nullable
// fields are null, not invented.
func TestPayloadFreshSession(t *testing.T) {
	in := Input{
		SessionID: "s", TranscriptPath: "/t.jsonl", Cwd: "/w",
		ModelID: "claude-sonnet-5-5", ModelDisplayName: "Sonnet",
		HaveContextWindow: true,
	}
	data, err := Build(in).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	keys, _ := KeySet(data)
	want := []string{
		"context_window.context_window_size", "context_window.current_usage",
		"context_window.remaining_percentage", "context_window.total_input_tokens",
		"context_window.total_output_tokens", "context_window.used_percentage",
		"cwd", "exceeds_200k_tokens", "model.display_name", "model.id", "session_id",
		"transcript_path", "workspace.added_dirs", "workspace.current_dir", "workspace.project_dir",
	}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %v\nwant %v", keys, want)
	}
	cw := m["context_window"].(map[string]any)
	for _, k := range []string{"current_usage", "used_percentage", "remaining_percentage"} {
		if v, ok := cw[k]; !ok || v != nil {
			t.Errorf("context_window.%s = %v, want null", k, v)
		}
	}
	if dirs := m["workspace"].(map[string]any)["added_dirs"]; dirs == nil {
		t.Error("added_dirs must be [], not null")
	}
	if m["workspace"].(map[string]any)["project_dir"] != "/w" {
		t.Error("project_dir defaults to cwd")
	}
}

func TestContextWindowMath(t *testing.T) {
	in := Input{HaveContextWindow: true, LastUsage: &Usage{InputTokens: 8_500, OutputTokens: 1_200,
		CacheCreationInputTokens: 5_000, CacheReadInputTokens: 2_000}}
	p := Build(in)
	cw := p.ContextWindow
	if cw.TotalInputTokens != 15_500 || cw.TotalOutputTokens != 1_200 || cw.ContextWindowSize != 200_000 {
		t.Errorf("totals = %+v", cw)
	}
	if *cw.UsedPercentage != 8 || *cw.RemainingPercentage != 92 {
		t.Errorf("percentages = %v/%v", *cw.UsedPercentage, *cw.RemainingPercentage)
	}
	if *p.Exceeds200k {
		t.Error("not over 200k")
	}

	in.ContextWindowSize = 1_000_000
	in.LastUsage = &Usage{InputTokens: 150_000, CacheReadInputTokens: 50_000, OutputTokens: 1}
	p = Build(in)
	if *p.ContextWindow.UsedPercentage != 20 || !*p.Exceeds200k {
		t.Errorf("1M window: used=%v exceeds=%v", *p.ContextWindow.UsedPercentage, *p.Exceeds200k)
	}

	in.ContextWindowSize = 100
	in.LastUsage = &Usage{InputTokens: 500}
	if got := *Build(in).ContextWindow.UsedPercentage; got != 100 {
		t.Errorf("clamped used = %v", got)
	}
}

func TestBuildOptionalGroups(t *testing.T) {
	p := Build(Input{ModelID: "m"})
	if p.Model.DisplayName != "m" {
		t.Error("display name falls back to the id")
	}
	if p.Cost != nil || p.ContextWindow != nil || p.Effort != nil || p.Thinking != nil ||
		p.Vim != nil || p.Agent != nil || p.OutputStyle != nil || p.FastMode != nil || p.Exceeds200k != nil {
		t.Errorf("unknown groups must be omitted: %+v", p)
	}
	p = Build(Input{Thinking: ptr(false), FastMode: ptr(false)})
	data, _ := p.Marshal()
	if !strings.Contains(string(data), `"thinking":{"enabled":false}`) || !strings.Contains(string(data), `"fast_mode":false`) {
		t.Errorf("known false values must be sent: %s", data)
	}
}

func TestKeySet(t *testing.T) {
	keys, err := KeySet([]byte(`{"a":{"b":1,"c":{}},"d":[1],"e":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.b", "a.c", "d", "e"}; !slices.Equal(keys, want) {
		t.Errorf("KeySet = %v", keys)
	}
	if _, err := KeySet([]byte("nope")); err == nil {
		t.Error("invalid JSON")
	}
}

func TestNormalizeEffort(t *testing.T) {
	for in, want := range map[string]string{"HIGH": "high", " xhigh ": "xhigh", "custom": "custom", "": ""} {
		if got := NormalizeEffort(in); got != want {
			t.Errorf("NormalizeEffort(%q) = %q", in, got)
		}
	}
}
