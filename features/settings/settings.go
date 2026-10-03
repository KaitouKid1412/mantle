package settings

import "github.com/KaitouKid1412/mantle/pkg/ext"

// The area's features share one state value; each registers one panel.
func init() {
	a := newArea()
	for _, f := range a.features() {
		ext.Register(f)
	}
}

// features lists the area's registrations. Tests build their own area and call Setup
// on a recording Registrar.
func (a *area) features() []ext.Feature {
	return []ext.Feature{
		{ID: "settings.core", Order: 400, Parity: []string{"ST-33", "ST-34", "ST-35", "ST-36"},
			Setup: func(r ext.Registrar) error {
				a.subscribe(r)
				a.subscribeResults(r)
				return nil
			}},
		{ID: "settings.model", Order: 400, After: []string{"settings.core"},
			Parity: []string{"ST-01", "ST-02", "ST-03", "ST-04", "ST-05", "ST-06", "ST-13"},
			Setup:  a.setupModel},
		{ID: "settings.help", Order: 400, After: []string{"settings.core"},
			Parity: []string{"ST-28", "ST-29"}, Setup: a.setupHelp},
		{ID: "settings.effort", Order: 400, After: []string{"settings.model"},
			Parity: []string{"ST-07", "ST-08", "ST-09", "ST-10", "ST-11"}, Setup: a.setupEffort},
		{ID: "settings.status", Order: 400, After: []string{"settings.core"},
			Parity: []string{"ST-18", "ST-30"}, Setup: a.setupStatus},
	}
}
