package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"

	"github.com/KaitouKid1412/mantle/internal/engine"
)

var semverPrefix = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// engineVersionHint returns the installed claude's version without running it, so the
// startup layout can follow claude's default renderer before the first frame (running
// `claude --version` can take seconds). It tries, in order: the pinned version, the
// versioned file the claude symlink points to (native installer:
// ~/.local/share/claude/versions/<version>), and a package.json next to the resolved
// binary (npm installs). "" means unknown; callers then use the newest known default.
// The startup gate's version check (plan 05 / plan 02) still verifies the real version.
func engineVersionHint(statePath string) string {
	if s, err := engine.LoadEngineState(statePath); err == nil && s.Pinned != "" && s.PinnedVersion != "" {
		return s.PinnedVersion
	}
	bin, err := engine.ResolveBinary(statePath)
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return ""
	}
	if v := semverPrefix.FindString(filepath.Base(real)); v != "" {
		return v
	}
	for dir := filepath.Dir(real); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			continue
		}
		var pkg struct {
			Name, Version string
		}
		if json.Unmarshal(raw, &pkg) == nil && pkg.Name == "@anthropic-ai/claude-code" {
			return semverPrefix.FindString(pkg.Version)
		}
		break // the nearest package.json is not claude's
	}
	return ""
}
