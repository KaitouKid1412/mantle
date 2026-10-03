package transcript

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// config is the snapshot of the settings the transcript honours.
type config struct {
	verbose          bool   // verbose setting / --verbose
	viewMode         string // viewMode: default | verbose | focus
	maxProse         int    // maxProseWidth
	noHighlight      bool   // syntaxHighlightingDisabled
	showThinking     bool   // showThinkingSummaries
	showTurnDuration bool   // showTurnDuration (default on)
	showTimestamps   bool   // showMessageTimestamps
	timeFormat       string // timeFormat: "12h" | "24h" | ""
	timeZone         string // timeZone (IANA name)

	verbs     []string // spinner verbs after spinnerVerbs is applied
	tips      bool     // spinnerTipsEnabled (default on)
	tipList   []string // spinnerTipsOverride.tips
	tipsFile  string   // spinnerTipsOverride.tipsFile
	noMotion  bool     // prefersReducedMotion
	noLinks   bool     // hyperlinks off (screen reader, dumb terminals)
	flatShort bool     // screen-reader flat output
}

func defaultConfig() config {
	return config{showTurnDuration: true, tips: true, verbs: builtinVerbs}
}

// mode is the view mode in effect: brief mode, then the engine's view_mode
// (which /focus toggles), then the verbose and viewMode settings.
func (f *Feature) mode() ext.ViewMode {
	if f.brief {
		return ext.Brief
	}
	vm := f.cfg.viewMode
	if f.engineView != "" {
		vm = f.engineView
	}
	switch {
	case f.cfg.verbose || vm == "verbose":
		return ext.Verbose
	case vm == "focus":
		return ext.Focus
	}
	return ext.Normal
}

func loadConfig(s ext.Settings) config {
	c := defaultConfig()
	if s == nil {
		return c
	}
	c.verbose = ext.ClaudeBool(s, "verbose", false)
	c.viewMode = ext.ClaudeString(s, "viewMode", "")
	c.noHighlight = ext.ClaudeBool(s, "syntaxHighlightingDisabled", false)
	c.showThinking = ext.ClaudeBool(s, "showThinkingSummaries", false)
	c.showTurnDuration = ext.ClaudeBool(s, "showTurnDuration", true)
	c.showTimestamps = ext.ClaudeBool(s, "showMessageTimestamps", false)
	c.timeFormat = ext.ClaudeString(s, "timeFormat", "")
	c.timeZone = ext.ClaudeString(s, "timeZone", "")
	c.tips = ext.ClaudeBool(s, "spinnerTipsEnabled", true)
	c.noMotion = ext.ClaudeBool(s, "prefersReducedMotion", false)
	if v, ok := s.Claude("maxProseWidth"); ok {
		if n, ok := v.(float64); ok && n > 0 {
			c.maxProse = int(n)
		} else if n, ok := v.(int); ok && n > 0 {
			c.maxProse = n
		}
	}
	if v, ok := s.Claude("spinnerVerbs"); ok {
		c.verbs = spinnerVerbs(v)
	}
	if v, ok := s.Claude("spinnerTipsOverride"); ok {
		if m, ok := v.(map[string]any); ok {
			c.tipList = stringList(m["tips"])
			if f, ok := m["tipsFile"].(string); ok {
				c.tipsFile = f
			}
		}
	}
	return c
}

// spinnerVerbs applies the spinnerVerbs setting: {"mode": "append"|"replace",
// "verbs": [...]} (a bare list appends).
func spinnerVerbs(v any) []string {
	var mode string
	var verbs []string
	switch x := v.(type) {
	case map[string]any:
		mode, _ = x["mode"].(string)
		verbs = stringList(x["verbs"])
	case []any:
		verbs = stringList(x)
	}
	if len(verbs) == 0 {
		return builtinVerbs
	}
	if mode == "replace" {
		return verbs
	}
	return append(append([]string(nil), builtinVerbs...), verbs...)
}

func stringList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, x...)
	}
	return out
}
