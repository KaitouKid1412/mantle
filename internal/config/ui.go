package config

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Typed accessors for the Claude Code UI keys mantle honours. Defaults follow Claude
// Code's documented behaviour; a key set to the wrong type falls back to its default.

// StatusLine is the statusLine setting.
type StatusLine struct {
	Type                 string // "command"
	Command              string
	Padding              int
	RefreshInterval      int // seconds; 0 = only on events
	HideVimModeIndicator bool
}

// SpinnerVerbs is the spinnerVerbs setting.
type SpinnerVerbs struct {
	Mode  string // "append" | "replace"
	Verbs []string
}

// SpinnerTips is the spinnerTipsOverride setting.
type SpinnerTips struct {
	Tips     []string
	TipsFile string
}

// UI is a typed view of the UI settings.
type UI struct {
	Theme                      string
	EditorMode                 string // "normal" | "vim"
	VimInsertModeRemaps        map[string]string
	Verbose                    bool
	ViewMode                   string // "default" | "verbose" | "focus"
	TUI                        string // "default" | "fullscreen"
	StatusLine                 *StatusLine
	SpinnerVerbs               *SpinnerVerbs
	SpinnerTipsEnabled         bool
	SpinnerTipsOverride        *SpinnerTips
	PrefersReducedMotion       bool
	ShowTurnDuration           bool
	ShowMessageTimestamps      bool
	TimeFormat                 string
	TimeZone                   string
	MaxProseWidth              int // 0 = terminal width
	SyntaxHighlightingDisabled bool
	TerminalProgressBarEnabled bool
	TerminalTitleFromRename    bool
	PreferredNotifChannel      string
	RespectGitignore           bool
	FileSuggestion             map[string]any
	EmojiCompletionEnabled     bool
	PromptSuggestionEnabled    bool
	RespondToBashCommands      bool
	AutoScrollEnabled          bool
	AxScreenReader             bool
	FooterLinksRegexes         []string
	CompanyAnnouncements       []string
	TodoFeatureEnabled         bool
}

// UI returns the typed UI settings.
func (s *Store) UI() UI { return UIFrom(s) }

// UIFrom reads the typed UI settings from any ext.Settings.
func UIFrom(s ext.Settings) UI {
	str := func(k, def string) string { return ext.ClaudeString(s, k, def) }
	boolean := func(k string, def bool) bool { return ext.ClaudeBool(s, k, def) }
	u := UI{
		Theme:                      str("theme", "dark"),
		EditorMode:                 str("editorMode", "normal"),
		Verbose:                    boolean("verbose", false),
		ViewMode:                   str("viewMode", "default"),
		TUI:                        str("tui", "default"),
		SpinnerTipsEnabled:         boolean("spinnerTipsEnabled", true),
		PrefersReducedMotion:       boolean("prefersReducedMotion", false),
		ShowTurnDuration:           boolean("showTurnDuration", true),
		ShowMessageTimestamps:      boolean("showMessageTimestamps", false),
		TimeFormat:                 str("timeFormat", ""),
		TimeZone:                   str("timeZone", ""),
		MaxProseWidth:              intSetting(s, "maxProseWidth"),
		SyntaxHighlightingDisabled: boolean("syntaxHighlightingDisabled", false),
		TerminalProgressBarEnabled: boolean("terminalProgressBarEnabled", true),
		TerminalTitleFromRename:    boolean("terminalTitleFromRename", true),
		PreferredNotifChannel:      str("preferredNotifChannel", "auto"),
		RespectGitignore:           boolean("respectGitignore", true),
		EmojiCompletionEnabled:     boolean("emojiCompletionEnabled", true),
		PromptSuggestionEnabled:    boolean("promptSuggestionEnabled", true),
		RespondToBashCommands:      boolean("respondToBashCommands", false),
		AutoScrollEnabled:          boolean("autoScrollEnabled", true),
		AxScreenReader:             boolean("axScreenReader", false),
		TodoFeatureEnabled:         boolean("todoFeatureEnabled", true),
		FooterLinksRegexes:         stringList(s, "footerLinksRegexes"),
		CompanyAnnouncements:       stringList(s, "companyAnnouncements"),
	}
	if m, ok := object(s, "vimInsertModeRemaps"); ok {
		u.VimInsertModeRemaps = map[string]string{}
		for k, v := range m {
			if str, ok := v.(string); ok {
				u.VimInsertModeRemaps[k] = str
			}
		}
	}
	if m, ok := object(s, "statusLine"); ok {
		sl := &StatusLine{Type: "command"}
		if v, ok := m["type"].(string); ok {
			sl.Type = v
		}
		sl.Command, _ = m["command"].(string)
		sl.Padding = toInt(m["padding"])
		sl.RefreshInterval = toInt(m["refreshInterval"])
		sl.HideVimModeIndicator, _ = m["hideVimModeIndicator"].(bool)
		if sl.Command != "" {
			u.StatusLine = sl
		}
	}
	if m, ok := object(s, "spinnerVerbs"); ok {
		sv := &SpinnerVerbs{Mode: "append"}
		if v, ok := m["mode"].(string); ok {
			sv.Mode = v
		}
		sv.Verbs = anyStrings(m["verbs"])
		u.SpinnerVerbs = sv
	}
	if m, ok := object(s, "spinnerTipsOverride"); ok {
		st := &SpinnerTips{Tips: anyStrings(m["tips"])}
		st.TipsFile, _ = m["tipsFile"].(string)
		u.SpinnerTipsOverride = st
	}
	if m, ok := object(s, "fileSuggestion"); ok {
		u.FileSuggestion = m
	}
	return u
}

func object(s ext.Settings, key string) (map[string]any, bool) {
	v, ok := s.Claude(key)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok
}

func stringList(s ext.Settings, key string) []string {
	v, ok := s.Claude(key)
	if !ok {
		return nil
	}
	return anyStrings(v)
}

func anyStrings(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		if ss, ok := v.([]string); ok {
			return ss
		}
		return nil
	}
	var out []string
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func intSetting(s ext.Settings, key string) int {
	v, _ := s.Claude(key)
	return toInt(v)
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return 0
}
