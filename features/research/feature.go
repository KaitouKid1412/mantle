// Package research is research mode's UI: one conversation node per screen in the
// fullscreen viewport, with clickable bars for the node's ancestors and children, key
// navigation and a tree picker (plan 13). The tree itself is internal/research.
package research

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// FeatureID is this feature's registration ID, and the owner of its transcript scope.
const FeatureID = "research"

// Component and dialog IDs.
const (
	AncestorsID = "research.ancestors"
	ChildrenID  = "research.children"
	TreeID      = "research.tree"
)

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 650,
		// MT-R1, R6 and R8 come with the mode switch, the send stage and the sidecar.
		Parity: []string{"MT-R2", "MT-R3", "MT-R4", "MT-R5", "MT-R7"},
		Setup: func(r ext.Registrar) error {
			newFeature().setup(r)
			return nil
		},
	})
}

func newFeature() *feature {
	return &feature{lastChild: map[string]string{}, scopes: newScopeCache(), hover: noHover}
}

func (f *feature) setup(r ext.Registrar) {
	only := []ext.LayoutMode{ext.Fullscreen}
	r.AddComponent(ext.SlotHeader, &bar{f: f, id: AncestorsID}, ext.SlotOpts{Weight: -10, Modes: only})
	r.AddComponent(ext.SlotAboveInput, &bar{f: f, id: ChildrenID}, ext.SlotOpts{Weight: -10, Modes: only})
	r.AddDialog(TreeID, f.openPicker)
	f.addActions(r)
	for _, s := range stories() {
		r.AddStory(s)
	}

	ext.Subscribe(r, FeatureID+".scopeLoaded", func(ctx ext.Ctx, m scopeLoadedMsg) tea.Cmd {
		return f.scopeLoaded(ctx, m)
	})
	ext.Subscribe(r, FeatureID+".selection", func(ctx ext.Ctx, m ext.SelectionMsg) tea.Cmd {
		f.selection = m.Text
		return nil
	})
	// The store was reset (session switch, rewind) or the screen redrawn: send the
	// scope again so the viewport points at the new store's items.
	ext.Subscribe(r, FeatureID+".history", func(ctx ext.Ctx, m ext.TranscriptHistoryMsg) tea.Cmd {
		if !m.Reset {
			return nil
		}
		return f.rescope(ctx)
	})
	ext.Subscribe(r, FeatureID+".cleared", func(ctx ext.Ctx, _ ext.ScreenClearedMsg) tea.Cmd {
		return f.rescope(ctx)
	})
}
