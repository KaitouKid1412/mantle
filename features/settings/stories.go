package settings

import (
	"github.com/KaitouKid1412/mantle/features/settings/model"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// sampleModels is story data: a typical model list (mantle's own descriptions).
func sampleModels() []model.Info {
	all := []string{"low", "medium", "high", "xhigh", "max"}
	return []model.Info{
		{Value: "default", ResolvedModel: "claude-opus-5-5", DisplayName: "Default (recommended)",
			Description: "Account default model", SupportsEffort: true, SupportedEffortLevels: all,
			SupportsAdaptiveThinking: true, SupportsFastMode: true},
		{Value: "fable", ResolvedModel: "claude-fable-5-1", DisplayName: "Fable",
			Description: "Largest model for the hardest tasks", SupportsEffort: true, SupportedEffortLevels: all,
			SupportsAdaptiveThinking: true},
		{Value: "opus", ResolvedModel: "claude-opus-5-5", DisplayName: "Opus",
			Description: "Strong all-round model", SupportsEffort: true, SupportedEffortLevels: all,
			SupportsAdaptiveThinking: true, SupportsFastMode: true},
		{Value: "sonnet", ResolvedModel: "claude-sonnet-5-5", DisplayName: "Sonnet",
			Description: "Fast everyday model", SupportsEffort: true,
			SupportedEffortLevels: []string{"low", "medium", "high"}, SupportsAdaptiveThinking: true},
		{Value: "haiku", ResolvedModel: "claude-haiku-4-5-20251001", DisplayName: "Haiku",
			Description: "Smallest and quickest model"},
	}
}

// storyArea is an area primed with sample engine state and no side effects.
func storyArea() *area {
	a := newArea()
	a.engine.models = sampleModels()
	return a
}

func modelStories() []ext.Story {
	return []ext.Story{
		{ID: "settings.model/picker", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
			a := storyArea()
			a.engine.sys = nil
			d, _ := a.newModelPicker(c, nil)
			p := d.(*modelPicker)
			p.cursor = 2 // Opus, to show the effort row
			return p.View(c, ar)
		}},
		{ID: "settings.model/switch-warning", Render: func(c ext.Ctx, ar ext.Area) ext.Rendered {
			a := storyArea()
			d, _ := a.newModelPicker(c, nil)
			p := d.(*modelPicker)
			p.confirm = &model.Choice{Row: p.rows[3]}
			return p.View(c, ar)
		}},
	}
}
