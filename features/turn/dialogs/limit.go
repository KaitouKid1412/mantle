package dialogs

// Usage-limit dialog option IDs.
const (
	LimitWait  = "wait"
	LimitModel = "model"
	LimitStop  = "stop"
)

// NewUsageLimit offers what to do when a usage limit is reached: wait until resetAt
// (already formatted, e.g. "3:04 PM") and continue, switch model, or stop. Esc stops.
func NewUsageLimit(resetAt string) *Choice {
	body := func(w int, st Styles) []string {
		out := styleLines(st.Warning, wrap("You've reached your usage limit.", w))
		if resetAt != "" {
			out = append(out, wrap("It resets at "+SanitizeLine(resetAt)+".", w)...)
		}
		return out
	}
	wait := "Wait and continue automatically"
	if resetAt != "" {
		wait = "Wait until " + SanitizeLine(resetAt) + " and continue"
	}
	return &Choice{
		title: "Usage limit reached",
		kind:  "warning",
		body:  body,
		opts:  optionList{items: []option{{id: LimitWait, label: wait}, {id: LimitModel, label: "Switch model"}, {id: LimitStop, label: "Stop"}}},
		escID: LimitStop,
	}
}
