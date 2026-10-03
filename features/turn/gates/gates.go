package gates

import (
	"fmt"
	"slices"
)

// Kind names one startup gate. Gates run in this order: trust, bypass, mcp, apiKey
// (after plan 02's engine version check, before spawn).
type Kind string

const (
	KindTrust  Kind = "trust"
	KindBypass Kind = "bypass"
	KindMcp    Kind = "mcp"
	KindAPIKey Kind = "apiKey"
)

// Input is everything the gates need to evaluate a launch.
type Input struct {
	Env   Env
	Cwd   string
	Flags LaunchFlags
	// AutoTrust skips the trust gate: mantle's own builder worktrees are trusted by
	// construction.
	AutoTrust bool
	// SessionTrusted is set when the user already accepted trust for Cwd in this process
	// (the home directory's trust is never persisted).
	SessionTrusted bool
}

// Report is the evaluated state of every gate for one launch.
type Report struct {
	Input       Input
	Global      *GlobalConfig
	Settings    Layers
	Store       *GateStore
	Trust       TrustState
	TrustReport TrustReport
	// BypassWarning is true when the bypass-permissions warning must be shown.
	BypassWarning bool
	Mcp           McpApprovalResult
	APIKey        APIKeyState
}

// Evaluate reads every input once and decides which gates are pending. It writes
// nothing.
func Evaluate(in Input) *Report {
	r := &Report{Input: in}
	r.Global = LoadGlobalConfig(in.Env)
	r.Settings = LoadSettings(in.Env, in.Cwd, in.Flags.Settings)
	r.Store = LoadGateStore(in.Env)
	r.Trust = CheckTrust(in.Env, in.Cwd, r.Global)
	if !in.Flags.StrictMcpConfig {
		r.Mcp = McpApproval(in.Env, in.Cwd, r.Settings, r.Global, r.Store)
	}
	r.TrustReport = CollectTrustReport(in.Cwd, r.Settings, r.Mcp)
	r.BypassWarning = BypassWarningNeeded(in.Flags, r.Settings, r.Store)
	r.APIKey = APIKeyApproval(in.Env, r.Global, r.Store)
	return r
}

// Pending returns the gates still to show, in order.
func (r *Report) Pending() []Kind {
	var out []Kind
	if !r.Input.AutoTrust && !r.Input.SessionTrusted && !r.Trust.Trusted {
		out = append(out, KindTrust)
	}
	if r.BypassWarning {
		out = append(out, KindBypass)
	}
	if len(r.Mcp.Pending()) > 0 {
		out = append(out, KindMcp)
	}
	if r.APIKey.Status == APIKeyNew {
		out = append(out, KindAPIKey)
	}
	return out
}

// Notices returns non-blocking problems to report at startup: invalid settings files
// (headless claude silently ignores them) and an unreadable ~/.claude.json, which mantle
// reports but never repairs.
func (r *Report) Notices() []string {
	var out []string
	for _, p := range r.Settings.Problems() {
		where := p.Path
		if where == "" {
			where = "--settings"
		}
		out = append(out, fmt.Sprintf("Settings file ignored (%s): %s: %v", p.Scope, where, p.Err))
	}
	if r.Global != nil && r.Global.Err != nil {
		out = append(out, fmt.Sprintf("Could not read Claude Code's global config: %v", r.Global.Err))
	}
	for _, err := range r.Mcp.Errors {
		out = append(out, fmt.Sprintf("Could not read .mcp.json: %v", err))
	}
	return out
}

// Answers are the user's responses to the gate dialogs.
type Answers struct {
	TrustAccepted  bool
	BypassAccepted bool
	// McpApproved maps a pending server name to the user's choice. Missing names count
	// as not approved.
	McpApproved map[string]bool
	// McpEnableAll approves every current and future .mcp.json server of the project.
	McpEnableAll bool
	// APIKeyApproved is the answer to the API-key prompt (nil when not asked).
	APIKeyApproved *bool
}

// Outcome is what the launch does after the gates.
type Outcome struct {
	// Proceed is false when the user declined trust or the bypass warning; mantle exits.
	Proceed bool
	Reason  string
	// Settings is the engine's --settings value (inline JSON, "" for none). It folds the
	// user's own --settings together with disabledMcpjsonServers.
	Settings string
	// UnsetEnv lists environment variables to drop from the engine's environment.
	UnsetEnv []string
}

// Resolve computes the launch outcome from the answers. It writes nothing; call Record
// to persist the answers.
func (r *Report) Resolve(a Answers) (Outcome, error) {
	pending := r.Pending()
	if slices.Contains(pending, KindTrust) && !a.TrustAccepted {
		return Outcome{Reason: "workspace not trusted"}, nil
	}
	if slices.Contains(pending, KindBypass) && !a.BypassAccepted {
		return Outcome{Reason: "bypass permissions mode declined"}, nil
	}
	out := Outcome{Proceed: true}
	add := map[string]any{}
	approved := a.McpApproved
	if a.McpEnableAll {
		approved = map[string]bool{}
		for _, s := range r.Mcp.Pending() {
			approved[s.Name] = true
		}
	}
	if disabled := r.Mcp.Disabled(approved); len(disabled) > 0 {
		add["disabledMcpjsonServers"] = disabled
	}
	s, err := FlagSettings(r.Input.Flags.Settings, r.Input.Cwd, add)
	if err != nil {
		return Outcome{}, err
	}
	out.Settings = s
	key := r.APIKey
	if key.Status == APIKeyNew && a.APIKeyApproved != nil {
		key.Status = APIKeyRejected
		if *a.APIKeyApproved {
			key.Status = APIKeyApproved
		}
	}
	out.UnsetEnv = key.EngineEnvUnset()
	return out, nil
}

// Record persists the answers to mantle's own stores: trust for the folder, the bypass
// acceptance, the .mcp.json choices and the API-key answer.
func (r *Report) Record(a Answers) error {
	pending := r.Pending()
	if slices.Contains(pending, KindTrust) && a.TrustAccepted {
		if _, err := RecordTrust(r.Input.Env, r.Input.Cwd); err != nil {
			return err
		}
	}
	if slices.Contains(pending, KindBypass) && a.BypassAccepted {
		if err := RecordBypassAccepted(r.Input.Env); err != nil {
			return err
		}
	}
	if slices.Contains(pending, KindMcp) {
		answered := map[string]bool{}
		for _, s := range r.Mcp.Pending() {
			answered[s.Name] = a.McpEnableAll || a.McpApproved[s.Name]
		}
		if err := RecordMcp(r.Input.Env, r.Input.Cwd, answered, a.McpEnableAll); err != nil {
			return err
		}
	}
	if slices.Contains(pending, KindAPIKey) && a.APIKeyApproved != nil {
		if err := RecordAPIKey(r.Input.Env, r.APIKey.Key(), *a.APIKeyApproved); err != nil {
			return err
		}
	}
	return nil
}
