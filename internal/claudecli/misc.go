package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ---- version ----

var reVersion = regexp.MustCompile(`\b(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)

// ParseVersion extracts the semantic version from "claude --version" output,
// e.g. "2.1.288 (Claude Code)" → "2.1.288".
func ParseVersion(out []byte) (string, error) {
	m := reVersion.FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("claude --version: unrecognised output %q", firstLine(string(out)))
	}
	return string(m[1]), nil
}

// Version runs "claude --version".
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, _, err := r.Run(ctx, Version)
	if err != nil {
		return "", err
	}
	return ParseVersion(out)
}

// CompareVersions compares dotted numeric versions ("2.1.288" < "2.1.290").
// Pre-release suffixes are ignored.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var parts []int
	for _, s := range strings.Split(v, ".") {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		parts = append(parts, n)
	}
	return parts
}

// ---- auth ----

// AuthInfo is "claude auth status --json". Unknown fields are ignored.
type AuthInfo struct {
	LoggedIn          bool   `json:"loggedIn"`
	AuthMethod        string `json:"authMethod"`
	APIProvider       string `json:"apiProvider"`
	AnalyticsDisabled bool   `json:"analyticsDisabled"`
	ProjectsDirectory string `json:"projectsDirectory"`
	ConfigDirectory   string `json:"configDirectory"`
	Email             string `json:"email"`
	OrgID             string `json:"orgId"`
	OrgName           string `json:"orgName"`
	SubscriptionType  string `json:"subscriptionType"`
}

// ParseAuthStatus parses "claude auth status --json".
func ParseAuthStatus(b []byte) (AuthInfo, error) {
	var s AuthInfo
	if err := json.Unmarshal(bytes.TrimSpace(b), &s); err != nil {
		return s, fmt.Errorf("claude auth status: %w", err)
	}
	return s, nil
}

// AuthStatus runs "claude auth status --json". A logged-out CLI may exit
// non-zero while still printing the JSON; that is reported as LoggedIn=false.
func (r *Runner) AuthStatus(ctx context.Context) (AuthInfo, error) {
	out, _, err := r.Run(ctx, AuthStatus, "--json")
	s, perr := ParseAuthStatus(out)
	var exitErr *ExitError
	switch {
	case perr == nil && (err == nil || errors.As(err, &exitErr)):
		return s, nil
	case err != nil:
		return s, err
	}
	return s, perr
}

// AuthMethod picks the login flow for "claude auth login".
type AuthMethod string

const (
	AuthClaudeAI AuthMethod = "claudeai" // Claude subscription (the CLI default)
	AuthConsole  AuthMethod = "console"  // Anthropic Console, API billing
	AuthSSO      AuthMethod = "sso"      // force SSO
)

// AuthLoginCmd returns "claude auth login" for tea.ExecProcess. email
// pre-fills the login page and may be empty.
func (r *Runner) AuthLoginCmd(method AuthMethod, email string) (*exec.Cmd, error) {
	var args []string
	switch method {
	case "":
	case AuthClaudeAI, AuthConsole, AuthSSO:
		args = append(args, "--"+string(method))
	default:
		return nil, &ArgError{Sub: AuthLogin, Msg: fmt.Sprintf("unknown login method %q", method)}
	}
	args = appendValue(args, "--email", email)
	return r.Interactive(AuthLogin, args...)
}

// AuthLogoutCmd returns "claude auth logout" for tea.ExecProcess.
func (r *Runner) AuthLogoutCmd() (*exec.Cmd, error) { return r.Interactive(AuthLogout) }

// ---- background sessions (agent view) ----

// BackgroundSession is one entry of "claude agents --json". Interactive
// sessions have no ID; background ones carry a short ID and a State.
type BackgroundSession struct {
	PID         int    `json:"pid"`
	ID          string `json:"id"`
	CWD         string `json:"cwd"`
	Kind        string `json:"kind"` // interactive, background
	StartedAtMs int64  `json:"startedAt"`
	SessionID   string `json:"sessionId"`
	Name        string `json:"name"`
	Status      string `json:"status"` // idle, busy, …
	State       string `json:"state"`  // working, …
}

// StartedAt converts StartedAtMs.
func (s BackgroundSession) StartedAt() time.Time {
	if s.StartedAtMs == 0 {
		return time.Time{}
	}
	return time.UnixMilli(s.StartedAtMs)
}

// ParseAgents parses "claude agents --json".
func ParseAgents(b []byte) ([]BackgroundSession, error) {
	var out []BackgroundSession
	if err := json.Unmarshal(bytes.TrimSpace(b), &out); err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	return out, nil
}

// Agents runs "claude agents --json [--all] [--cwd <path>]".
func (r *Runner) Agents(ctx context.Context, all bool, cwd string) ([]BackgroundSession, error) {
	var args []string
	if all {
		args = append(args, "--all")
	}
	args = appendValue(args, "--cwd", cwd)
	out, _, err := r.Run(ctx, AgentsList, args...)
	if err != nil {
		return nil, err
	}
	return ParseAgents(out)
}

// AttachCmd returns "claude attach <id>" for tea.ExecProcess.
func (r *Runner) AttachCmd(id string) (*exec.Cmd, error) { return r.Interactive(Attach, id) }

// AgentViewCmd returns "claude agents" (the interactive agent view).
func (r *Runner) AgentViewCmd() (*exec.Cmd, error) { return r.Interactive(AgentsView) }

// Logs runs "claude logs <id>".
func (r *Runner) Logs(ctx context.Context, id string) (string, error) {
	out, _, err := r.Run(ctx, Logs, id)
	return string(out), err
}

// StopSession runs "claude stop <id>". The conversation is kept.
func (r *Runner) StopSession(ctx context.Context, id string) error {
	_, _, err := r.Run(ctx, Stop, id)
	return err
}

// ---- import ----

// ImportPreview is the result of "claude import --dry-run".
type ImportPreview struct {
	Text   string // the CLI's preview, ANSI-free
	Digest string // scan digest to confirm with --yes=<digest>; empty when nothing to import
}

var reDigestHint = regexp.MustCompile(`(?:scan digest:\s*|--yes=)([A-Za-z0-9_-]{1,128})`)

// ParseImportPreview extracts the scan digest from a dry-run preview.
func ParseImportPreview(out []byte) ImportPreview {
	p := ImportPreview{Text: strings.TrimSpace(string(out))}
	if m := reDigestHint.FindSubmatch(out); m != nil {
		p.Digest = string(m[1])
	}
	return p
}

// ImportDryRun runs "claude import --dry-run [source]". source is codex,
// gemini, cursor or empty for every detected agent.
func (r *Runner) ImportDryRun(ctx context.Context, source string) (ImportPreview, error) {
	args := []string{"--dry-run"}
	if source != "" {
		args = append(args, source)
	}
	out, _, err := r.Run(ctx, Import, args...)
	if err != nil {
		return ImportPreview{}, err
	}
	return ParseImportPreview(out), nil
}

// ImportApply runs "claude import --yes=<digest> [source]" with the digest from
// the preview the person confirmed. The CLI refuses if the scan changed.
func (r *Runner) ImportApply(ctx context.Context, source, digest string) (string, error) {
	if !reDigest.MatchString(digest) {
		return "", &ArgError{Sub: Import, Msg: "missing or invalid digest"}
	}
	args := []string{"--yes=" + digest}
	if source != "" {
		args = append(args, source)
	}
	out, _, err := r.Run(ctx, Import, args...)
	return strings.TrimSpace(string(out)), err
}

// ---- auto mode (used by plan 08's /permissions) ----

// AutoModeRules is the JSON printed by "claude auto-mode defaults|config".
type AutoModeRules struct {
	Allow       []string `json:"allow"`
	SoftDeny    []string `json:"soft_deny"`
	HardDeny    []string `json:"hard_deny"`
	Environment []string `json:"environment"`
}

// ParseAutoModeRules parses auto-mode JSON.
func ParseAutoModeRules(b []byte) (AutoModeRules, error) {
	var r AutoModeRules
	if err := json.Unmarshal(bytes.TrimSpace(b), &r); err != nil {
		return r, fmt.Errorf("claude auto-mode: %w", err)
	}
	return r, nil
}

// AutoModeDefaults runs "claude auto-mode defaults [--label <prefix>]".
func (r *Runner) AutoModeDefaults(ctx context.Context, label string) (AutoModeRules, error) {
	out, _, err := r.Run(ctx, AutoModeDefaults, appendValue(nil, "--label", label)...)
	if err != nil {
		return AutoModeRules{}, err
	}
	return ParseAutoModeRules(out)
}

// AutoModeConfig runs "claude auto-mode config" (effective rules).
func (r *Runner) AutoModeConfig(ctx context.Context) (AutoModeRules, error) {
	out, _, err := r.Run(ctx, AutoModeConfig)
	if err != nil {
		return AutoModeRules{}, err
	}
	return ParseAutoModeRules(out)
}

// ---- other interactive flows ----

// DoctorCmd returns "claude doctor" (installation health check).
func (r *Runner) DoctorCmd() (*exec.Cmd, error) { return r.Interactive(Doctor) }

// UpdateCmd returns "claude update". Run the engine conformance probe afterwards.
func (r *Runner) UpdateCmd() (*exec.Cmd, error) { return r.Interactive(Update) }

// MCPAddFromDesktopCmd returns "claude mcp add-from-claude-desktop" (a picker).
func (r *Runner) MCPAddFromDesktopCmd(scope string) (*exec.Cmd, error) {
	return r.Interactive(MCPAddFromDesktop, appendValue(nil, "--scope", scope)...)
}
