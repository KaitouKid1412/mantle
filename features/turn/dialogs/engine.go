package dialogs

import "strings"

// Engine-version dialog option IDs.
const (
	EnginePin      = "pin"
	EngineContinue = "continue"
	EngineExit     = "exit"
)

// NewEngineCheck reports an engine version that failed mantle's conformance checks
// (for example a claude that runs in --bare mode headlessly). failed names the checks
// that failed. When lastGood is set, the first option pins that earlier version.
// Esc exits.
func NewEngineCheck(version string, failed []string, lastGood string) *Choice {
	v := SanitizeLine(version)
	if v == "" {
		v = "this version"
	}
	body := func(w int, st Styles) []string {
		out := styleLines(st.Warning, wrap("claude "+v+" did not pass mantle's startup checks.", w))
		if len(failed) > 0 {
			out = append(out, wrap("Failed: "+SanitizeLine(strings.Join(failed, ", ")), w)...)
		}
		out = append(out, "")
		out = append(out, wrap("Hooks, skills, CLAUDE.md or other features may not work with it.", w)...)
		return out
	}
	var items []option
	if lastGood != "" {
		items = append(items, option{id: EnginePin, label: "Use claude " + SanitizeLine(lastGood) + " instead (it passed before)"})
	}
	items = append(items,
		option{id: EngineContinue, label: "Continue with " + v + " anyway"},
		option{id: EngineExit, label: "Exit"},
	)
	return &Choice{
		title: "Engine check failed",
		kind:  "warning",
		body:  body,
		opts:  optionList{items: items},
		escID: EngineExit,
		hint:  "enter to confirm · esc to exit",
	}
}
