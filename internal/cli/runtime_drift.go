package cli

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/config"
)

// KnownEngine lists the engine commands and tools of the claude version mantle's tables
// were last checked against (scripts/drift -accept writes known_engine.json).
type KnownEngine struct {
	ClaudeVersion string   `json:"claude_version"`
	Commands      []string `json:"commands"` // names and aliases
	Tools         []string `json:"tools"`
}

//go:embed known_engine.json
var knownEngineJSON []byte

// Known returns the embedded engine tables.
func Known() KnownEngine {
	var k KnownEngine
	_ = json.Unmarshal(knownEngineJSON, &k)
	return k
}

// DriftState is the last runtime drift check, kept in ~/.mantle/state/drift.json.
type DriftState struct {
	EngineVersion string     `json:"engine_version"`
	KnownVersion  string     `json:"known_version"` // the embedded tables' claude version
	CheckedAt     time.Time  `json:"checked_at"`
	NewCommands   []string   `json:"new_commands,omitempty"`
	NewTools      []string   `json:"new_tools,omitempty"`
	NewFlags      []HelpFlag `json:"new_flags,omitempty"`
	ChangedFlags  []string   `json:"changed_flags,omitempty"` // arity differs from the table
	Errors        []string   `json:"errors,omitempty"`
}

// EnvDriftCheck set to "off" disables the runtime drift check (tests, CI).
const EnvDriftCheck = "MANTLE_DRIFT_CHECK"

// DriftStatePath is ~/.mantle/state/drift.json ($MANTLE_HOME honoured).
func DriftStatePath() string {
	p, err := config.DefaultPaths("")
	if err != nil {
		return ""
	}
	return filepath.Join(p.StateDir(), "drift.json")
}

// LoadDriftState reads the last check; ok is false when there is none.
func LoadDriftState(path string) (DriftState, bool) {
	var s DriftState
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &s) != nil {
		return DriftState{}, false
	}
	return s, true
}

// SaveDriftState writes the state atomically.
func SaveDriftState(path string, s DriftState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Probes used by CheckDrift; tests replace them.
var (
	probeHelp   = func(ctx context.Context, bin string) (string, error) { return ClaudeHelp(ctx, bin, "", HelpTimeout) }
	probeEngine = func(ctx context.Context, bin string) (EngineReport, error) {
		return ProbeEngine(ctx, bin, 45*time.Second)
	}
)

// CheckDrift compares the installed engine (bin, at version) with the tables embedded
// in mantle: root flags from `claude --help`, and built-in commands and tools from a
// zero-token engine session. Failures are recorded in Errors; it never fails.
func CheckDrift(ctx context.Context, bin, version string) DriftState {
	known := Known()
	s := DriftState{EngineVersion: version, KnownVersion: known.ClaudeVersion, CheckedAt: time.Now().UTC()}

	if help, err := probeHelp(ctx, bin); err != nil {
		s.Errors = append(s.Errors, "claude --help: "+err.Error())
	} else {
		flags, _ := ParseHelp(help)
		for _, hf := range flags {
			f := lookupHelpFlag(hf)
			if f == nil {
				s.NewFlags = append(s.NewFlags, hf)
				continue
			}
			want := f.Arity
			if want == ArityRepeatable {
				want = ArityRequired
			}
			if hf.TableArity() != want {
				s.ChangedFlags = append(s.ChangedFlags, hf.Long)
			}
		}
	}

	if rep, err := probeEngine(ctx, bin); err != nil {
		s.Errors = append(s.Errors, "engine session: "+err.Error())
	} else {
		for _, c := range rep.Commands {
			if !slices.Contains(known.Commands, c.Name) && !slices.ContainsFunc(c.Aliases, func(a string) bool { return slices.Contains(known.Commands, a) }) {
				s.NewCommands = append(s.NewCommands, c.Name)
			}
		}
		for _, t := range rep.Tools {
			if !strings.HasPrefix(t, "mcp__") && !slices.Contains(known.Tools, t) {
				s.NewTools = append(s.NewTools, t)
			}
		}
	}
	return s
}

// lookupHelpFlag finds a help-listed flag in the table by its long name or aliases. A
// new long name is new even if its short letter is taken.
func lookupHelpFlag(hf HelpFlag) *Flag {
	for _, n := range append([]string{hf.Long}, hf.Aliases...) {
		if f := Lookup(n); f != nil {
			return f
		}
	}
	return nil
}

// RuntimeDrift is the startup check. When the installed engine's version differs from
// the last recorded check, it runs CheckDrift and records the result at statePath. ran
// reports whether a check ran (a notice is due only then). It never blocks the UI: call
// it from a Cmd.
func RuntimeDrift(ctx context.Context, statePath string) (s DriftState, ran bool, err error) {
	if os.Getenv(EnvDriftCheck) == "off" || statePath == "" {
		return DriftState{}, false, nil
	}
	bin, err := ResolveClaude()
	if err != nil {
		return DriftState{}, false, err
	}
	version, err := EngineVersion(ctx)
	if err != nil {
		return DriftState{}, false, err
	}
	if old, ok := LoadDriftState(statePath); ok && old.EngineVersion == version {
		return old, false, nil
	}
	s = CheckDrift(ctx, bin, version)
	return s, true, SaveDriftState(statePath, s)
}

// Notice is the one-line startup notice for a check, or "" when nothing is new.
func (s DriftState) Notice() string {
	var parts []string
	count := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	count(len(s.NewCommands), "command", "commands")
	count(len(s.NewTools), "tool", "tools")
	count(len(s.NewFlags), "flag", "flags")
	if len(parts) == 0 {
		return ""
	}
	list := parts[0]
	if len(parts) > 1 {
		list = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return fmt.Sprintf("Claude Code %s adds %s mantle doesn't know yet; they work as engine passthrough", s.EngineVersion, list)
}

// TableWithLearnt returns the flag table plus the flags the last drift check learnt
// from the installed engine (statePath), or Flags itself when there are none.
func TableWithLearnt(statePath string) []Flag {
	s, ok := LoadDriftState(statePath)
	if !ok {
		return Flags
	}
	learnt := s.LearntFlags()
	if len(learnt) == 0 {
		return Flags
	}
	return append(slices.Clone(Flags), learnt...)
}

// LearntFlags returns the new engine flags as forwarded table entries, so the parser
// knows their arity (and doesn't mistake a flag's value for the prompt). Flags the
// built-in table has learnt since are skipped.
func (s DriftState) LearntFlags() []Flag {
	var out []Flag
	for _, hf := range s.NewFlags {
		if lookupHelpFlag(hf) != nil || !strings.HasPrefix(hf.Long, "--") {
			continue
		}
		f := Flag{Long: hf.Long, Aliases: hf.Aliases, Arity: hf.TableArity(), Class: Forward, Parity: "CLI-02"}
		if hf.Short != "" && Lookup(hf.Short) == nil {
			f.Short = hf.Short
		}
		out = append(out, f)
	}
	return out
}
