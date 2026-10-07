package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

// Environment variables shared by the launcher, mantle-ui and the installer.
const (
	// EnvHome overrides the mantle home directory (default ~/.mantle).
	EnvHome = "MANTLE_HOME"
	// EnvSafe is "1" when mantle-ui must skip mod registrations (Order ≥ 1000).
	EnvSafe = "MANTLE_SAFE"
	// EnvBuildID is the build id of the running mantle-ui ("dev" for MANTLE_UI_BIN).
	EnvBuildID = "MANTLE_BUILD_ID"
	// EnvLauncherPID is the pid of the supervising launcher.
	EnvLauncherPID = "MANTLE_LAUNCHER_PID"
	// EnvProbation is "1" while the running build is on probation.
	EnvProbation = "MANTLE_PROBATION"
	// EnvUIBin makes the launcher supervise this binary instead of current/mantle-ui
	// (development; no probation, no version store).
	EnvUIBin = "MANTLE_UI_BIN"
	// EnvClaudeBin overrides the claude binary used for passthrough.
	EnvClaudeBin = "MANTLE_CLAUDE_BIN"
)

// Exit codes of mantle-ui as understood by the launcher.
const (
	ExitOK      = 0
	ExitUsage   = 64 // EX_USAGE: bad arguments; not counted as a crash
	ExitRestart = 75 // EX_TEMPFAIL: relaunch current with the run file's handoff args
)

// UIBinaryName is the file name of the UI binary inside a version folder.
const UIBinaryName = "mantle-ui"

// ManifestName is the file name of a version's manifest.
const ManifestName = "manifest.json"

// Layout is the ~/.mantle directory tree.
//
//	bin/mantle            launcher
//	src/                  private clone that installs build from (branch user)
//	versions/<build-id>/  immutable: mantle-ui, manifest.json
//	current, last-good    symlinks into versions/
//	builds/<id>/          build scratch space
//	run/<pid>.json        live run files
//	state/                healthy markers, probation counters, feature state
//	logs/<pid>.log        mantle-ui stderr
//	promote.lock          flock for promotion
//	mods.json             cache of the mod list (never the source of truth)
type Layout struct {
	Root string
}

// DefaultLayout returns $MANTLE_HOME or ~/.mantle.
func DefaultLayout() (Layout, error) {
	if h := os.Getenv(EnvHome); h != "" {
		abs, err := filepath.Abs(h)
		if err != nil {
			return Layout{}, err
		}
		return Layout{Root: abs}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	if home == "" {
		return Layout{}, errors.New("cannot find the home directory; set " + EnvHome)
	}
	return Layout{Root: filepath.Join(home, ".mantle")}, nil
}

func (l Layout) path(elem ...string) string {
	return filepath.Join(append([]string{l.Root}, elem...)...)
}

func (l Layout) Bin() string                  { return l.path("bin") }
func (l Layout) Launcher() string             { return l.path("bin", "mantle") }
func (l Layout) Src() string                  { return l.path("src") }
func (l Layout) Work() string                 { return l.path("work") }
func (l Layout) Worktree(modID string) string { return l.path("work", modID) }
func (l Layout) Versions() string             { return l.path("versions") }
func (l Layout) Version(id string) string     { return l.path("versions", id) }
func (l Layout) Current() string              { return l.path("current") }
func (l Layout) LastGood() string             { return l.path("last-good") }
func (l Layout) Builds() string               { return l.path("builds") }
func (l Layout) Build(id string) string       { return l.path("builds", id) }
func (l Layout) Run() string                  { return l.path("run") }
func (l Layout) RunFile(pid int) string       { return l.path("run", strconv.Itoa(pid)+".json") }
func (l Layout) State() string                { return l.path("state") }
func (l Layout) Logs() string                 { return l.path("logs") }
func (l Layout) Log(pid int) string           { return l.path("logs", strconv.Itoa(pid)+".log") }
func (l Layout) PromoteLock() string          { return l.path("promote.lock") }
func (l Layout) ModsCache() string            { return l.path("mods.json") }
func (l Layout) Settings() string             { return l.path("settings.json") }
func (l Layout) DisabledFeatures() string     { return l.path("state", "disabled.json") }

// EnsureDirs creates the directories of the layout that the launcher and the
// pipeline write into.
func (l Layout) EnsureDirs() error {
	for _, d := range []string{l.Root, l.Bin(), l.Work(), l.Versions(), l.Builds(), l.Run(), l.State(), l.Logs()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
