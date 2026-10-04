package sessions

import (
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// storySessions is fixed picker data for stories and tests.
func storySessions(now time.Time) []sessions.SessionMeta {
	return []sessions.SessionMeta{
		{ID: "11111111-1111-4111-8111-111111111111", Cwd: "/work/demo", GitBranch: "main",
			AITitle: "Explain the build system", FirstPrompt: "Explain the build", MessageCount: 6,
			Modified: now.Add(-2 * time.Hour)},
		{ID: "22222222-2222-4222-8222-222222222222", Cwd: "/work/demo", GitBranch: "feature/x",
			FirstPrompt: "List files and summarize them", PRNumber: 42, Hidden: true,
			Modified: now.Add(-26 * time.Hour)},
		{ID: "55555555-5555-4555-8555-555555555555", Cwd: "/work/demo", GitBranch: "main",
			CustomTitle: "My renamed session", MessageCount: 12, Modified: now.Add(-9 * 24 * time.Hour)},
	}
}

func contextStory(ctx ext.Ctx, a ext.Area) ext.Rendered {
	cu := proto.ContextUsage{
		TotalTokens: 151_000, MaxTokens: 200_000, Percentage: 75.5,
		Categories: []proto.ContextCategory{
			{Name: "System prompt", Tokens: 3_100, Color: "promptBorder"},
			{Name: "System tools", Tokens: 12_000, Color: "inactive"},
			{Name: "MCP tools", Tokens: 6_400, Color: "ide"},
			{Name: "Memory files", Tokens: 1_500, Color: "remember"},
			{Name: "Messages", Tokens: 128_000, Color: "claude"},
			{Name: "Autocompact buffer", Tokens: 33_000, Kind: "buffer"},
			{Name: "Free space", Tokens: 16_000, Kind: "free"},
		},
	}
	it := &ext.Item{Key: KeyContextUsage, Data: &ContextReport{Usage: cu, Model: "claude-opus-5-5"}}
	b := renderContextReport(ext.RenderCtx{Width: a.Width, Theme: ctx.Theme()}, it)
	return ext.Rendered{Text: strings.Join(b.Lines, "\n")}
}

func handoffStory(ctx ext.Ctx, a ext.Area) ext.Rendered {
	d := &handoffDialog{extra: []string{"/privacy-settings"}, tasks: 1, sid: "11111111-1111-4111-8111-111111111111"}
	return d.View(ctx, a)
}

func pickerStory(ctx ext.Ctx, a ext.Area) ext.Rendered {
	p := &picker{
		f:    newFeature(sessions.Layout{ConfigDir: "/nonexistent"}, ""),
		live: "11111111-1111-4111-8111-111111111111",
		list: storySessions(ctx.Clock().Now()),
	}
	p.refilter()
	p.sel = 1
	if a.MaxHeight == 0 {
		a.MaxHeight = 14
	}
	return p.View(ctx, a)
}
