package chrome

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// ReleaseNotes is one version's section of a changelog.
type ReleaseNotes struct {
	Version string
	Lines   []string // the section body, trimmed of surrounding blank lines
}

// ParseChangelog splits a markdown changelog into versions, newest first as written.
// A version section starts at a "## <version>" heading (an optional leading "v" and
// trailing text after the version, such as a date, are tolerated).
func ParseChangelog(md string) []ReleaseNotes {
	var out []ReleaseNotes
	var cur *ReleaseNotes
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if v, ok := versionHeading(line); ok {
			out = append(out, ReleaseNotes{Version: v})
			cur = &out[len(out)-1]
			continue
		}
		if strings.HasPrefix(line, "# ") {
			cur = nil // a top-level heading ends the current section
			continue
		}
		if cur != nil {
			cur.Lines = append(cur.Lines, strings.TrimRight(line, " \t"))
		}
	}
	for i := range out {
		out[i].Lines = trimBlank(out[i].Lines)
	}
	return out
}

// NotesSince returns the sections newer than lastSeen (all of them when lastSeen is
// empty), at most limit (0 = no limit).
func NotesSince(notes []ReleaseNotes, lastSeen string, limit int) []ReleaseNotes {
	var out []ReleaseNotes
	for _, n := range notes {
		if lastSeen != "" && !terminal.VersionAtLeast(n.Version, lastSeen) {
			continue
		}
		if lastSeen != "" && n.Version == lastSeen {
			continue
		}
		out = append(out, n)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

// LatestVersion is the highest version in notes ("" when empty).
func LatestVersion(notes []ReleaseNotes) string {
	best := ""
	for _, n := range notes {
		if best == "" || (terminal.VersionAtLeast(n.Version, best) && n.Version != best) {
			best = n.Version
		}
	}
	return best
}

// ClaudeChangelogPath is Claude Code's cached changelog, read at runtime only. It
// honours CLAUDE_CONFIG_DIR.
func ClaudeChangelogPath(env terminal.Env) string {
	dir := terminal.Get(env, "CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, "cache", "changelog.md")
}

func versionHeading(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "## ")
	if !ok {
		return "", false
	}
	rest = strings.TrimPrefix(strings.TrimSpace(rest), "[")
	rest = strings.TrimPrefix(rest, "v")
	end := 0
	for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9' || rest[end] == '.') {
		end++
	}
	v := strings.TrimRight(rest[:end], ".")
	if v == "" || !strings.Contains(v, ".") {
		return "", false
	}
	return v, true
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
