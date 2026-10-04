package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// EngineState is ~/.mantle/state/engines.json: conformance results per claude
// version, and the pinned binary (if the user pinned one).
type EngineState struct {
	Passed map[string]VersionRecord `json:"passed,omitempty"`
	Failed map[string]VersionRecord `json:"failed,omitempty"`
	// Pinned is the engine binary mantle runs instead of `claude` on PATH.
	Pinned        string `json:"pinned,omitempty"`
	PinnedVersion string `json:"pinned_version,omitempty"`
}

// VersionRecord is one probed version.
type VersionRecord struct {
	Binary  string    `json:"binary"`
	Checked time.Time `json:"checked"`
	Failed  []string  `json:"failed,omitempty"` // failed check names
}

// DefaultEngineStatePath is ~/.mantle/state/engines.json.
func DefaultEngineStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mantle", "state", "engines.json")
}

// LoadEngineState reads path; a missing file is an empty state.
func LoadEngineState(path string) (EngineState, error) {
	var s EngineState
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// Save writes the state atomically.
func (s EngineState) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LastGood returns the newest passing version other than except, and its binary.
func (s EngineState) LastGood(except string) (version, binary string) {
	var vs []string
	for v := range s.Passed {
		if v != except {
			vs = append(vs, v)
		}
	}
	sort.Slice(vs, func(i, j int) bool { return versionLess(vs[j], vs[i]) })
	for _, v := range vs {
		bin := s.Passed[v].Binary
		if p := VersionBinary(v); p != "" {
			bin = p
		}
		if bin != "" {
			if _, err := os.Stat(bin); err == nil {
				return v, bin
			}
		}
	}
	return "", ""
}

func versionLess(a, b string) bool { return !versionAtLeast(a, b) }

// VersionBinary returns the installer's copy of a version
// (~/.local/share/claude/versions/<v>) if it exists.
func VersionBinary(version string) string {
	home, err := os.UserHomeDir()
	if err != nil || version == "" {
		return ""
	}
	p := filepath.Join(home, ".local", "share", "claude", "versions", version)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p
	}
	return ""
}

// ResolveBinary returns the engine binary to run: $MANTLE_CLAUDE_BIN, else the pinned
// binary from statePath (if it still exists), else `claude` on PATH.
func ResolveBinary(statePath string) (string, error) {
	if p := os.Getenv("MANTLE_CLAUDE_BIN"); p != "" {
		return p, nil
	}
	if statePath != "" {
		if s, err := LoadEngineState(statePath); err == nil && s.Pinned != "" {
			if _, err := os.Stat(s.Pinned); err == nil {
				return s.Pinned, nil
			}
		}
	}
	return exec.LookPath("claude")
}

// Pin makes mantle run binary (version) until Unpin.
func Pin(statePath, binary, version string) error {
	s, err := LoadEngineState(statePath)
	if err != nil {
		return err
	}
	s.Pinned, s.PinnedVersion = binary, version
	return s.Save(statePath)
}

// Unpin goes back to `claude` on PATH.
func Unpin(statePath string) error {
	s, err := LoadEngineState(statePath)
	if err != nil {
		return err
	}
	s.Pinned, s.PinnedVersion = "", ""
	return s.Save(statePath)
}

// EngineCheck is the outcome of CheckEngine.
type EngineCheck struct {
	Version string
	Binary  string
	// Probed is true when this call ran the probe (the version was new).
	Probed bool
	Result *ProbeResult
	OK     bool
	// LastGood is the newest version that passed before, with a runnable binary, to
	// offer for pinning when OK is false.
	LastGood, LastGoodBinary string
	// Override is true when $MANTLE_CLAUDE_BIN chose the binary: an explicit choice
	// (development, tests with fakeclaude) that is accepted without a probe.
	Override bool
}

// CheckOptions configure CheckEngine.
type CheckOptions struct {
	StatePath string // "" = DefaultEngineStatePath
	Binary    string // "" = ResolveBinary
	Probe     ProbeOptions
	// Force probes even if the version passed before, or $MANTLE_CLAUDE_BIN is set.
	Force bool
}

var cliVersionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// CheckEngine runs the conformance probe when the engine version is new (B9). A
// version that passed before is accepted without probing; results are recorded in
// the state file. On failure it reports the last passing version to offer pinning.
func CheckEngine(ctx context.Context, o CheckOptions) (EngineCheck, error) {
	if o.StatePath == "" {
		o.StatePath = DefaultEngineStatePath()
	}
	var c EngineCheck
	bin := o.Binary
	if bin == "" && os.Getenv("MANTLE_CLAUDE_BIN") != "" && !o.Force {
		c.Override = true
	}
	if bin == "" {
		var err error
		if bin, err = ResolveBinary(o.StatePath); err != nil {
			return c, err
		}
	}
	c.Binary = bin
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(vctx, bin, "--version")
	cmd.Env = append(os.Environ(), "CLAUDECODE=")
	out, err := cmd.Output()
	if err != nil {
		return c, fmt.Errorf("%s --version: %w", bin, err)
	}
	c.Version = cliVersionRe.FindString(string(out))
	if c.Version == "" {
		return c, fmt.Errorf("%s --version: no version in %q", bin, out)
	}
	if c.Override {
		c.OK = true // explicit override: version only, no probe, nothing recorded
		return c, nil
	}
	state, err := LoadEngineState(o.StatePath)
	if err != nil {
		return c, err
	}
	if _, ok := state.Passed[c.Version]; ok && !o.Force {
		c.OK = true
		return c, nil
	}
	po := o.Probe
	po.Binary = bin
	res, err := Probe(ctx, po)
	if err != nil {
		return c, err
	}
	c.Probed, c.Result, c.OK = true, &res, res.OK
	rec := VersionRecord{Binary: bin, Checked: time.Now().UTC(), Failed: res.Failed()}
	if res.OK {
		if state.Passed == nil {
			state.Passed = map[string]VersionRecord{}
		}
		state.Passed[c.Version] = rec
		delete(state.Failed, c.Version)
	} else {
		if state.Failed == nil {
			state.Failed = map[string]VersionRecord{}
		}
		state.Failed[c.Version] = rec
		c.LastGood, c.LastGoodBinary = state.LastGood(c.Version)
	}
	if err := state.Save(o.StatePath); err != nil {
		return c, err
	}
	return c, nil
}
