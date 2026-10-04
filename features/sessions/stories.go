package sessions

import (
	"time"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
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
