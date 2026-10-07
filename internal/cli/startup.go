package cli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// SessionResolver finds the session -c and -r name. Plan 06's session index implements
// it (adapt it with ResolverFuncs).
type SessionResolver interface {
	// Continue returns the most recent session ID for cwd, headless (sdk-cli) sessions
	// included. An error means there is none.
	Continue(cwd string) (string, error)
	// Resolve interprets a -r value: the ID of the one session it names, or else a
	// search query for the picker. An error means it named a session that doesn't exist.
	Resolve(cwd, arg string) (id, query string, err error)
}

// ResolverFuncs adapts two functions to SessionResolver.
type ResolverFuncs struct {
	ContinueFunc func(cwd string) (string, error)
	ResolveFunc  func(cwd, arg string) (id, query string, err error)
}

func (r ResolverFuncs) Continue(cwd string) (string, error) { return r.ContinueFunc(cwd) }
func (r ResolverFuncs) Resolve(cwd, arg string) (string, string, error) {
	return r.ResolveFunc(cwd, arg)
}

// Startup is a UI command line turned into what mantle-ui starts with.
type Startup struct {
	// Spawn holds the main engine's options. When Picker is set the engine must not
	// start yet: the resume picker fills in Resume and starts it (ext.EngineStartMsg).
	Spawn ext.SpawnOpts
	// Picker opens the resume picker before any engine starts (-r without a session),
	// searching for PickerQuery.
	Picker      bool
	PickerQuery string
	// Session is the main engine's initial SessionInfo (app.Options.Session).
	Session ext.SessionInfo
	// Prompt is submitted as the first user message once the main engine is attached
	// (after the startup gates), through the prompt pipeline, so "/init" works.
	Prompt string
	// Prefill is text for the prompt box (--prefill, --prefill-b64).
	Prefill           string
	ScreenReader      bool  // --ax-screen-reader
	Verbose           bool  // --verbose
	Safe              bool  // mantle --safe reached mantle-ui: skip user mods
	Research          bool  // --research: start in research mode
	PromptSuggestions *bool // --prompt-suggestions, for initialize.promptSuggestions
	// AttachEngineFDs is the hand-off file of an in-place restart (request 10-02). When
	// set, the host adopts the engines it lists instead of spawning; Spawn (resuming the
	// session) is the fallback if adopting fails.
	AttachEngineFDs string
	// Worktree is the -w worktree name ("" without -w). The engine creates
	// .claude/worktrees/<name> on branch worktree-<name> and runs there (init.cwd).
	// Headless claude never offers to remove it on exit.
	Worktree string
	// Warnings are one-line notices to show at startup; Unknown lists the flags
	// forwarded without being recognised (log them).
	Warnings []string
	Unknown  []string
}

// MainSpawn returns the options to spawn the main engine with at startup
// (app.Options.MainSpawn), or nil when the resume picker opens first.
func (s Startup) MainSpawn() *ext.SpawnOpts {
	if s.Picker {
		return nil
	}
	o := s.Spawn
	return &o
}

// SetEnv exports the mantle-only flags the UI process reads from its environment
// (mantle-ui passes os.Setenv): ext.EnvSafe for --safe, ext.EnvResearch for --research.
func (s Startup) SetEnv(setenv func(key, value string) error) {
	if s.Safe {
		setenv(ext.EnvSafe, "1")
	}
	if s.Research {
		setenv(ext.EnvResearch, "1")
	}
}

// FlagSettings is the flag-scope settings for mantle's own config (plan 01's
// config.NewStore): the user's --settings plus the UI keys that command-line flags imply,
// as claude applies them (--verbose overrides the verbose setting). The engine still gets
// the user's --settings unchanged.
func (s Startup) FlagSettings() (string, error) {
	overlay := map[string]any{}
	if s.Verbose {
		overlay["verbose"] = true
	}
	return MergeSettings(s.Spawn.Settings, overlay)
}

// RestartOpts are the live values a relaunch should use instead of the command line's.
type RestartOpts struct {
	SessionID      string // the session to resume (required)
	Name           string // current session name; "" keeps the command line's
	Model          string // current model; "" keeps the command line's
	PermissionMode string // current mode; "" keeps the command line's
}

// RestartArgs is the mantle command line that relaunches this session (the launcher's
// exit-75 handoff): every forwarded flag, in the form the engine got it (an unnamed -w
// carries the name mantle gave it, so the restart reuses that worktree and its session
// directory), the UI flags that must survive (--ax-screen-reader, --prompt-suggestions),
// and --resume. The prompt, -c, -r, --fork-session, --session-id and --prefill are not
// repeated.
func (s Startup) RestartArgs(o RestartOpts) []string {
	var args []string
	pick := func(live, startup string) string {
		if live != "" {
			return live
		}
		return startup
	}
	if v := pick(o.Name, s.Spawn.Name); v != "" {
		args = append(args, "--name="+v)
	}
	if v := pick(o.Model, s.Spawn.Model); v != "" {
		args = append(args, "--model="+v)
	}
	if v := pick(o.PermissionMode, s.Spawn.PermissionMode); v != "" {
		args = append(args, "--permission-mode="+v)
	}
	for _, d := range s.Spawn.AddDirs {
		args = append(args, "--add-dir="+d)
	}
	if s.Spawn.Settings != "" {
		args = append(args, "--settings="+s.Spawn.Settings)
	}
	if s.ScreenReader {
		args = append(args, "--ax-screen-reader")
	}
	if s.PromptSuggestions != nil {
		args = append(args, fmt.Sprintf("--prompt-suggestions=%t", *s.PromptSuggestions))
	}
	args = append(args, s.Spawn.ExtraArgs...)
	// Last, in = form: it starts with "-", so a variadic flag before it ends there.
	return append(args, "--resume="+o.SessionID)
}

// ErrNoSession is returned for -c when the directory has no session to continue.
var ErrNoSession = errors.New("no conversation found to continue in this directory")

// Startup builds a UI session from a parsed command line (ModeUI). cwd is the working
// directory; res resolves -c and -r (nil leaves both to the engine).
//
// Flags with a SpawnOpts field (--model, --permission-mode, --add-dir, --settings) move
// into that field instead of ExtraArgs. The engine appends ExtraArgs last and claude
// keeps the last --settings, so a user --settings left in ExtraArgs would silently
// replace what the startup gates add (disabledMcpjsonServers); in the field the gates
// can merge it (MergeSettings). Every other forwarded flag stays in ExtraArgs, verbatim.
func (p *Parsed) Startup(cwd string, res SessionResolver) (Startup, error) {
	if p.Mode != ModeUI {
		return Startup{}, fmt.Errorf("cli: Startup needs a UI command line, not %s", p.Mode)
	}
	m := p.Mantle
	s := Startup{
		Prompt:            p.Prompt,
		Prefill:           m.Prefill,
		ScreenReader:      m.ScreenReader,
		Verbose:           m.Verbose,
		Safe:              m.Safe,
		Research:          m.Research,
		AttachEngineFDs:   m.AttachEngineFDs,
		PromptSuggestions: m.PromptSuggestions,
		Warnings:          slices.Clone(p.Warnings),
		Unknown:           slices.Clone(p.Unknown),
	}
	o := &s.Spawn
	o.Cwd = cwd
	o.Name = m.Name
	o.SessionID = m.SessionID
	o.ForkSession = m.ForkSession
	o.SafeMode = m.SafeMode
	o.ExtraArgs = []string{}
	for _, occ := range p.Flags {
		f := occ.Flag
		if f != nil && f.Class != Forward && f.Class != Warn {
			continue
		}
		switch {
		case f != nil && f.Long == "--model":
			o.Model = occ.Value()
		case f != nil && f.Long == "--permission-mode":
			o.PermissionMode = occ.Value()
		case f != nil && f.Long == "--settings":
			o.Settings = occ.Value()
		case f != nil && f.Long == "--add-dir":
			o.AddDirs = append(o.AddDirs, occ.Values...)
		case f != nil && f.Long == "--worktree" && len(occ.Values) == 0:
			// claude names an unnamed worktree at random on every start, so an engine
			// restart (resume, session switch) would make another one. Name it once;
			// claude reuses a worktree that already has the name.
			s.Worktree = newWorktreeName()
			o.ExtraArgs = append(o.ExtraArgs, occ.Name, s.Worktree)
		case f != nil && f.Long == "--worktree":
			s.Worktree = occ.Value()
			o.ExtraArgs = append(o.ExtraArgs, occ.Tokens...)
		default:
			o.ExtraArgs = append(o.ExtraArgs, occ.Tokens...)
		}
	}

	switch {
	case m.Resume:
		if m.Continue {
			s.Warnings = append(s.Warnings, "both --continue and --resume given; resuming")
		}
		if m.AttachEngineFDs != "" {
			// An in-place restart: the engine is alive and Spawn is only the fallback.
			// Resume exactly the session the old process named; never look it up or open
			// the picker.
			o.Resume = m.ResumeQuery
			break
		}
		if m.ResumeQuery == "" {
			s.Picker = true
			break
		}
		if res == nil {
			o.Resume = m.ResumeQuery
			break
		}
		id, query, err := res.Resolve(cwd, m.ResumeQuery)
		if err != nil {
			return s, fmt.Errorf("no conversation found for %q: %w", m.ResumeQuery, err)
		}
		if id != "" {
			o.Resume = id
		} else {
			s.Picker, s.PickerQuery = true, query
		}
	case m.Continue:
		if res == nil {
			o.Continue = true
			break
		}
		id, err := res.Continue(cwd)
		if err != nil || id == "" {
			return s, ErrNoSession
		}
		o.Resume = id
	}
	if m.ForkSession && !m.Resume && !m.Continue {
		s.Warnings = append(s.Warnings, "--fork-session only applies with --resume or --continue")
	}

	sid := o.Resume
	if sid == "" || (o.ForkSession && o.SessionID != "") {
		sid = o.SessionID
	}
	s.Session = ext.SessionInfo{
		EngineID:       ext.MainEngine,
		SessionID:      sid,
		Cwd:            cwd,
		Title:          m.Name,
		Model:          o.Model,
		PermissionMode: o.PermissionMode,
	}
	return s, nil
}

// GateInputs are the command-line values the startup gates (plan 05) read.
type GateInputs struct {
	PermissionMode       string // --permission-mode
	Settings             string // --settings: inline JSON or a path
	SkipPermissions      bool   // --dangerously-skip-permissions
	AllowSkipPermissions bool   // --allow-dangerously-skip-permissions
	StrictMcpConfig      bool   // --strict-mcp-config
}

// GateInputsOf reads the gate inputs from spawn options, for example
// ext.SpawnGateMsg.Opts: the dedicated fields first, then ExtraArgs, which the engine
// passes last and which therefore win.
func GateInputsOf(o ext.SpawnOpts) GateInputs {
	g := GateInputs{PermissionMode: o.PermissionMode, Settings: o.Settings}
	var p Parsed
	_, _ = tokenize(&p, o.ExtraArgs, defaultIndex)
	for _, occ := range p.Flags {
		if occ.Flag == nil {
			continue
		}
		switch occ.Flag.Long {
		case "--permission-mode":
			g.PermissionMode = occ.Value()
		case "--settings":
			g.Settings = occ.Value()
		case "--dangerously-skip-permissions":
			g.SkipPermissions = true
		case "--allow-dangerously-skip-permissions":
			g.AllowSkipPermissions = true
		case "--strict-mcp-config":
			g.StrictMcpConfig = true
		}
	}
	return g
}

// MergeSettings combines a --settings value (inline JSON, or a path to a JSON file, as
// claude reads it) with keys mantle must add, such as the .mcp.json gate's
// disabledMcpjsonServers. Objects merge recursively, arrays merge as sets, and other
// overlay values win. The result is inline JSON. With an empty overlay the value is
// returned unchanged.
func MergeSettings(value string, overlay map[string]any) (string, error) {
	if len(overlay) == 0 {
		return value, nil
	}
	base := map[string]any{}
	if v := strings.TrimSpace(value); v != "" {
		data := []byte(v)
		if !strings.HasPrefix(v, "{") {
			b, err := os.ReadFile(v)
			if err != nil {
				return "", fmt.Errorf("--settings: %w", err)
			}
			data = b
		}
		if err := json.Unmarshal(data, &base); err != nil {
			return "", fmt.Errorf("--settings: not a JSON object: %w", err)
		}
	}
	merged := mergeJSON(base, overlay)
	b, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mergeJSON(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, ov := range overlay {
		bv, ok := out[k]
		if !ok {
			out[k] = ov
			continue
		}
		bm, bIsMap := bv.(map[string]any)
		om, oIsMap := ov.(map[string]any)
		if bIsMap && oIsMap {
			out[k] = mergeJSON(bm, om)
			continue
		}
		if ba, ok := asSlice(bv); ok {
			if oa, ok := asSlice(ov); ok {
				out[k] = unionJSON(ba, oa)
				continue
			}
		}
		out[k] = ov
	}
	return out
}

func asSlice(v any) ([]any, bool) {
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		out := make([]any, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, true
	}
	return nil, false
}

func unionJSON(a, b []any) []any {
	out := slices.Clone(a)
	seen := map[string]bool{}
	for _, x := range a {
		k, _ := json.Marshal(x)
		seen[string(k)] = true
	}
	for _, x := range b {
		k, _ := json.Marshal(x)
		if !seen[string(k)] {
			seen[string(k)] = true
			out = append(out, x)
		}
	}
	return out
}

// Words for worktree names: mantle's own short lists.
var (
	worktreeAdjectives = []string{"amber", "brisk", "calm", "deft", "eager", "fond", "gentle", "hardy",
		"keen", "lucid", "mellow", "nimble", "plucky", "quiet", "rapid", "steady", "tidy", "vivid", "witty", "zesty"}
	worktreeNouns = []string{"badger", "comet", "delta", "ember", "falcon", "garnet", "harbor", "island",
		"juniper", "kestrel", "lagoon", "meadow", "nebula", "orchard", "pebble", "quarry", "river", "summit", "tundra", "willow"}
)

// newWorktreeName returns a readable name such as "brisk-comet-3f9a".
var newWorktreeName = func() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s-%x", worktreeAdjectives[int(b[0])%len(worktreeAdjectives)],
		worktreeNouns[int(b[1])%len(worktreeNouns)], b[2:])
}

var (
	currentMu sync.Mutex
	current   *Startup
)

// SetCurrent records the startup options for features that read them at startup
// (features/cli, the gates). mantle-ui calls it once, before the host starts.
func SetCurrent(s Startup) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = &s
}

// ClearCurrent forgets the recorded startup options (tests).
func ClearCurrent() {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = nil
}

// Current returns the startup options recorded with SetCurrent.
func Current() (Startup, bool) {
	currentMu.Lock()
	defer currentMu.Unlock()
	if current == nil {
		return Startup{}, false
	}
	return *current, true
}
