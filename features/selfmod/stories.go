package selfmod

import (
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

var storyEpoch = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)

// storyBuilds are fixed BuildViews for the stories.
var storyBuilds = map[string]*BuildView{
	"building": {ID: "spinner-blue", Kind: "mod", Request: "make the spinner blue", Phase: PhaseBuilding,
		LastTool: "Edit mods/spinner-blue/spinner.go", CostUSD: 0.42, Budget: 5, Rounds: 3,
		Start: storyEpoch.Add(-72 * time.Second)},
	"vetting": {ID: "spinner-blue", Kind: "mod", Request: "make the spinner blue", Phase: PhaseVetting,
		Round: 2, Rounds: 3, CostUSD: 1.1, Budget: 5, Start: storyEpoch.Add(-3 * time.Minute)},
	"failed": {ID: "weather-command", Kind: "mod", Request: "add a /weather command", Phase: PhaseFailed,
		Round: 3, Rounds: 3, CostUSD: 2.75, Budget: 5, Start: storyEpoch.Add(-9 * time.Minute), End: storyEpoch,
		Err:    "the checks still fail after 3 rounds; the worktree is kept: /mantle retry weather-command, or /mantle edit",
		Detail: []string{"vet: go vet: exit status 1", "  mods/weather-command/weather.go:12:2: undefined: fetch"}},
	"done": {ID: "spinner-blue", Kind: "mod", Request: "make the spinner blue", Phase: PhaseDone,
		Round: 1, Rounds: 3, CostUSD: 0.61, Budget: 5, Start: storyEpoch.Add(-2 * time.Minute), End: storyEpoch,
		BuildID: "20260102-150405-abc1234"},
	"config": {ID: "verbs", Kind: "mod", Request: "use pirate spinner verbs", Phase: PhaseConfig,
		Start: storyEpoch.Add(-30 * time.Second), End: storyEpoch,
		Detail: []string{"Use pirate spinner verbs", `claude setting spinnerVerbs = {"mode":"replace","verbs":["Plundering"]}`, "apply it with /mantle apply verbs"}},
	"undo": {ID: "undo-spinner-blue", Kind: "undo", Request: "undo spinner-blue", Phase: PhaseVetting,
		Round: 1, Rounds: 3, Start: storyEpoch.Add(-20 * time.Second)},
}

func addStories(r ext.Registrar) {
	for _, name := range []string{"building", "vetting", "failed", "done", "config", "undo"} {
		v := storyBuilds[name]
		r.AddStory(ext.Story{
			ID: "selfmod.build/" + name,
			Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
				it := &ext.Item{ID: "mantle.build/" + v.ID, Key: KeyBuild, Data: v}
				blk := RenderBuild(ext.RenderCtx{Width: a.Width, Theme: ctx.Theme(), Now: storyEpoch}, it)
				return ext.Rendered{Text: strings.Join(blk.Lines, "\n")}
			},
		})
	}
	r.AddStory(ext.Story{
		ID: "selfmod.build/expanded",
		Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			it := &ext.Item{ID: "mantle.build/x", Key: KeyBuild, Data: storyBuilds["failed"]}
			blk := RenderBuild(ext.RenderCtx{Width: a.Width, Theme: ctx.Theme(), Now: storyEpoch, Expanded: true}, it)
			return ext.Rendered{Text: strings.Join(blk.Lines, "\n")}
		},
	})
	r.AddStory(ext.Story{
		ID: "selfmod.preview/diff",
		Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			d := &previewDialog{args: previewArgs{ID: "spinner-blue", Request: "make the spinner blue", Diffs: []StoryDiff{
				{ID: "chrome.spinner/thinking", Width: 100, Before: "✻ Thinking…", After: "✻ Thinking… (blue)"},
				{ID: "mod.spinner-blue/demo", Width: 100, New: true, After: "● blue spinner demo"},
			}}}
			return ext.Rendered{Text: strings.Join(d.lines(ctx.Theme(), a.Width, 0), "\n")}
		},
	})
}
