package parity

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Rule is one normalization: a named rewrite of volatile text.
type Rule struct {
	Name        string
	Description string
	re          *regexp.Regexp
	repl        string
}

func rule(name, desc, pattern, repl string) Rule {
	return Rule{Name: name, Description: desc, re: regexp.MustCompile(pattern), repl: repl}
}

// apply rewrites one line.
func (r Rule) apply(s string) string { return r.re.ReplaceAllString(s, r.repl) }

// Normalizer turns captured frames into comparable text: workspace paths, then the
// volatile-content rules in order, then optional colour stripping and trailing-space
// trimming.
type Normalizer struct {
	Rules        []Rule
	StripColor   bool
	TrimTrailing bool
	// Scrollback lines kept before the screen (the tail); 0 drops the scrollback, < 0
	// keeps all of it.
	Scrollback int
}

// spinnerGlyphs are the glyphs a busy indicator cycles through (Claude Code's flower
// set, dots and braille frames).
const spinnerGlyphs = `✻✽✶✳✢·*◐◓◑◒⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`

// DefaultRules are the volatile-content rules, applied in order.
var DefaultRules = []Rule{
	rule("spinner", "A busy line (spinner glyph, a verb ending in …, timers and hints) becomes <spinner>.",
		`^(\s*)[`+spinnerGlyphs+`]\s+\p{Lu}[\p{L}'-]*….*$`, "${1}<spinner>"),
	rule("tmp-path", "Temporary paths (/var/folders, /private/var, /tmp) become <tmp>.",
		`(?:/private)?/(?:var/folders|tmp|private/tmp)/[^\s│|)'"]*`, "<tmp>"),
	rule("uuid", "UUIDs (session, message and request ids) become <uuid>.",
		`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`, "<uuid>"),
	rule("tool-id", "Tool-use ids (toolu_…) become <toolu>.",
		`\btoolu_[A-Za-z0-9_]+\b`, "<toolu>"),
	rule("datetime", "ISO dates and times become <date>.",
		`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?\b`, "<date>"),
	rule("clock", "Clock times (14:05, 2:05 PM, 14:05:09) become <time>.",
		`\b\d{1,2}:\d{2}(?::\d{2})?(?:\s?[AaPp][Mm])?\b`, "<time>"),
	rule("cost", "Dollar amounts become $<cost>.",
		`\$\d+(?:,\d{3})*(?:\.\d+)?`, "$$<cost>"),
	rule("tokens", "Token counts (12.3k tokens, ↓ 120 tokens) become <n> tokens.",
		`(?:[↑↓]\s?)?\b\d+(?:[.,]\d+)?[kKM]?\s+tokens?\b`, "<n> tokens"),
	rule("duration", "Durations (850ms, 12s, 1m 5s, 2h 3m, 3.2s) become <dur>.",
		`\b(?:\d+h\s?)?(?:\d+m\s?)?\d+(?:\.\d+)?(?:ms|s)\b|\b\d+h(?:\s?\d+m)?\b|\b\d+m\b`, "<dur>"),
	rule("percent", "Percentages in meters (73% left, 12% used) become <pct>%.",
		`\b\d+(?:\.\d+)?%`, "<pct>%"),
}

// DefaultNormalizer strips colour, trims trailing spaces, keeps the last 200
// scrollback lines and applies DefaultRules.
func DefaultNormalizer() *Normalizer {
	return &Normalizer{Rules: DefaultRules, StripColor: true, TrimTrailing: true, Scrollback: 200}
}

// Line normalizes one line for a run's workspace.
func (n *Normalizer) Line(s string, ws Workspace) string {
	if n.StripColor {
		s = ansi.Strip(s)
	}
	s = workspacePaths(s, ws)
	for _, r := range n.Rules {
		s = r.apply(s)
	}
	if n.TrimTrailing {
		s = strings.TrimRight(s, " \t")
	}
	return s
}

// workspacePaths replaces the run's own directories, longest first, so every run
// prints the same placeholders.
func workspacePaths(s string, ws Workspace) string {
	for _, p := range []struct{ path, name string }{
		{ws.WorkDir, "<work>"}, {ws.ConfigDir, "<config>"}, {ws.HomeDir, "<home>"}, {ws.Root, "<root>"},
	} {
		if p.path != "" {
			s = strings.ReplaceAll(s, p.path, p.name)
		}
	}
	return s
}

// ScreenMarker separates the scrollback from the visible screen in a normalized frame.
const ScreenMarker = "──────── screen ────────"

// Frame normalizes a frame: the scrollback tail, a marker, then the screen, with
// trailing blank lines dropped from each part.
func (n *Normalizer) Frame(f Frame, ws Workspace) []string {
	var out []string
	sb := trimBlankTail(f.Scrollback)
	if n.Scrollback == 0 {
		sb = nil
	} else if n.Scrollback > 0 && len(sb) > n.Scrollback {
		sb = sb[len(sb)-n.Scrollback:]
	}
	for _, l := range sb {
		out = append(out, n.Line(l, ws))
	}
	if len(out) > 0 {
		out = append(out, ScreenMarker)
	}
	for _, l := range trimBlankTail(f.Screen) {
		out = append(out, n.Line(l, ws))
	}
	return out
}

// RuleDoc describes a rule for the report.
type RuleDoc struct{ Name, Description string }

// Describe lists every normalization the report applies.
func (n *Normalizer) Describe() []RuleDoc {
	docs := []RuleDoc{{"workspace", "The run's work, config, home and root directories become <work>, <config>, <home>, <root>."}}
	for _, r := range n.Rules {
		docs = append(docs, RuleDoc{r.Name, r.Description})
	}
	if n.StripColor {
		docs = append(docs, RuleDoc{"strip-color", "ANSI colour and style sequences are removed."})
	}
	if n.TrimTrailing {
		docs = append(docs, RuleDoc{"trim", "Trailing spaces and trailing blank lines are removed."})
	}
	switch {
	case n.Scrollback == 0:
		docs = append(docs, RuleDoc{"scrollback", "Scrollback is not compared."})
	case n.Scrollback > 0:
		docs = append(docs, RuleDoc{"scrollback", "Only the last lines of scrollback are compared."})
	}
	return docs
}

func trimBlankTail(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
