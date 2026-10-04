package fullscreen

import (
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// FeatureID is the fullscreen renderer feature.
const FeatureID = "fullscreen.renderer"

func init() {
	ext.Register(ext.Feature{
		ID: FeatureID, Order: 750,
		Parity: []string{"VW-15", "VW-16", "VW-17", "VW-18", "VW-19", "VW-20", "VW-21", "VW-23"},
		Setup: func(r ext.Registrar) error {
			env := terminal.OS()
			tv := newTranscriptView(env)
			fullscreenOnly := ext.SlotOpts{Modes: []ext.LayoutMode{ext.Fullscreen}}
			r.AddComponent(ext.SlotLive, tv, fullscreenOnly)
			r.AddComponent(ext.SlotHeader, &stickyHeader{tv: tv}, fullscreenOnly)
			for _, id := range scrollActions {
				r.AddAction(ext.Action{ID: id, Context: ext.ContextScroll,
					Description: "Fullscreen transcript: " + string(id), Run: tv.action(id)})
			}
			r.AddCommand(ext.Command{
				Name: "scroll-speed", ArgHint: "[1-20]", Source: ext.SourceBuiltin,
				Description: "Set how many lines the mouse wheel scrolls in fullscreen",
				Run:         scrollSpeedCommand(env),
			})
			r.AddSetting(ext.SettingSpec{Key: scrollSpeedKey, Type: "int", Default: defaultScrollSpeed,
				Description: "Lines the mouse wheel scrolls per notch in the fullscreen view"})
			addStories(r, tv)
			return nil
		},
	})
}
