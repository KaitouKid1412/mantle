package settings

// commandAvailability says which of the area's commands Claude Code would hide for the
// current account, provider or models, from what the engine reported. A command is
// only hidden on evidence: before initialize nothing is hidden.
//
// From the 2.1.288 binary, two of plan 08's commands vary:
//   - /fast is hidden when fast mode is not available, i.e. no model offers it;
//   - /advisor is hidden when the advisor feature is off, which also removes its
//     headless twin from initialize.commands.
//
// The rest are always available in an interactive session.
func (a *area) commandAvailability() map[string]bool {
	hidden := map[string]bool{"fast": false, "advisor": false}
	if len(a.engine.models) > 0 {
		fast := false
		for _, m := range a.engine.models {
			if m.SupportsFastMode {
				fast = true
			}
		}
		hidden["fast"] = !fast
	}
	if a.engine.commands != nil {
		found := false
		for _, c := range a.engine.commands {
			if c.Name == "advisor" {
				found = true
			}
		}
		hidden["advisor"] = !found
	}
	return hidden
}
