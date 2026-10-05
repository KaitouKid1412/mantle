package cli

import (
	"strconv"
	"strings"
)

// EngineDefaults are claude's UI defaults that mantle mirrors when a setting is unset.
type EngineDefaults struct {
	TUI string // "default" (inline) | "fullscreen"
}

// DefaultsEntry is the defaults from one claude version on.
type DefaultsEntry struct {
	Since    string // first claude version with these defaults
	Defaults EngineDefaults
}

// DefaultsTable lists claude's UI defaults by version, oldest first. scripts/drift
// checks the newest entry against what a fresh-config claude actually does.
var DefaultsTable = []DefaultsEntry{
	{Since: "0.0.0", Defaults: EngineDefaults{TUI: "default"}},
	// 2.1.289 starts in the fullscreen renderer with a fresh config (seen by plans 07
	// and 12; scripts/drift probes it).
	{Since: "2.1.289", Defaults: EngineDefaults{TUI: "fullscreen"}},
}

// DefaultsFor returns the defaults of a claude version: the newest entry whose version
// is <= v. "" or an unparseable version gets the newest known entry.
func DefaultsFor(version string) EngineDefaults {
	v, ok := parseVersion(version)
	if !ok {
		return DefaultsTable[len(DefaultsTable)-1].Defaults
	}
	d := DefaultsTable[0].Defaults
	for _, e := range DefaultsTable {
		if since, _ := parseVersion(e.Since); compareVersions(since, v) <= 0 {
			d = e.Defaults
		}
	}
	return d
}

// parseVersion reads "2.1.289" (a suffix such as "-beta" or " (Claude Code)" is
// ignored) into its numbers.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = engineVersionRE.FindString(strings.TrimSpace(s))
	if s == "" {
		return v, false
	}
	parts := strings.SplitN(s, ".", 3)
	for i, p := range parts {
		if j := strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
			p = p[:j]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
