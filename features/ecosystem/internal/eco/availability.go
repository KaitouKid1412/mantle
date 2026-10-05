package eco

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Auth is the kind of account the engine runs with, as far as command
// availability is concerned.
type Auth int

const (
	AuthUnknown    Auth = iota // no initialize reply yet: hide nothing
	AuthClaudeAI               // a claude.ai subscription (Pro, Max, Team, Enterprise)
	AuthConsole                // Anthropic API: Console login or an API key
	AuthThirdParty             // Bedrock, Vertex, Foundry, a gateway, …
)

func (a Auth) String() string {
	switch a {
	case AuthClaudeAI:
		return "claude-ai"
	case AuthConsole:
		return "console"
	case AuthThirdParty:
		return "third-party"
	}
	return "unknown"
}

// ClassifyAccount reads initialize's account info.
func ClassifyAccount(a proto.Account) Auth {
	switch p := strings.ToLower(a.APIProvider); {
	case p != "" && p != "firstparty":
		return AuthThirdParty
	case a.SubscriptionType != "" || strings.Contains(strings.ToLower(a.TokenSource), "claude.ai"):
		return AuthClaudeAI
	case a.APIKeySource != "" || a.TokenSource != "" || a.Email != "" || p == "firstparty":
		return AuthConsole
	}
	return AuthUnknown
}

// Command availability. claudeAIOnly holds the commands whose availability
// list in the 2.1.288/2.1.289 binary is claude.ai only, plus the cloud and
// claude.ai features that claude 2.1.289's / menu leaves out for an API-key
// account (checked with the side-by-side harness: remote-control, session,
// cloud-plugins, passes, privacy-settings, design-login, daemon,
// usage-credits). Commands not listed are available to every account.
var (
	claudeAIOnly = []string{"teleport", "desktop", "web-setup", "remote-env", "ultraplan", "autofix-pr",
		"chrome", "artifacts", "upgrade", "voice", "install-slack-app",
		"remote-control", "session", "cloud-plugins", "passes", "privacy-settings", "design-login", "daemon",
		"usage-credits"}
	claudeAIOrConsole = []string{"install-github-app", "logout"}
)

// envGates hide a command unless (or when) an environment variable is set, as
// Claude Code does: provider setup commands need their provider, and the
// DISABLE_*_COMMAND variables turn commands off.
var envGates = map[string]func(getenv func(string) string) bool{
	"setup-bedrock":      func(g func(string) string) bool { return !truthy(g("CLAUDE_CODE_USE_BEDROCK")) },
	"setup-vertex":       func(g func(string) string) bool { return !truthy(g("CLAUDE_CODE_USE_VERTEX")) },
	"bug":                disabledBy("DISABLE_BUG_COMMAND"),
	"feedback":           disabledBy("DISABLE_FEEDBACK_COMMAND"),
	"doctor":             disabledBy("DISABLE_DOCTOR_COMMAND"),
	"login":              disabledBy("DISABLE_LOGIN_COMMAND"),
	"logout":             disabledBy("DISABLE_LOGOUT_COMMAND"),
	"upgrade":            disabledBy("DISABLE_UPGRADE_COMMAND"),
	"usage-credits":      disabledBy("DISABLE_EXTRA_USAGE_COMMAND"),
	"install-github-app": disabledBy("DISABLE_INSTALL_GITHUB_APP_COMMAND"),
	// /import is listed only when another coding agent's config is there.
	"import": func(g func(string) string) bool { return !otherAgentConfig(g("HOME")) },
}

// otherAgentConfig reports whether a config directory of an agent claude
// import knows (Codex, Gemini CLI, Cursor) exists in home.
func otherAgentConfig(home string) bool {
	if home == "" {
		return true // unknown: keep /import visible
	}
	for _, d := range []string{".codex", ".gemini", ".cursor"} {
		if st, err := os.Stat(filepath.Join(home, d)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func disabledBy(name string) func(func(string) string) bool {
	return func(g func(string) string) bool { return truthy(g(name)) }
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// HiddenForAccount returns the plan 09 commands Claude Code hides for this kind
// of account (only hidden names are set). AuthUnknown hides nothing.
func HiddenForAccount(auth Auth) map[string]bool {
	hidden := map[string]bool{}
	if auth == AuthUnknown {
		return hidden
	}
	for _, n := range claudeAIOnly {
		if auth != AuthClaudeAI {
			hidden[n] = true
		}
	}
	for _, n := range claudeAIOrConsole {
		if auth == AuthThirdParty {
			hidden[n] = true
		}
	}
	return hidden
}

// HiddenForEnv returns the plan 09 commands the environment turns off.
func HiddenForEnv(getenv func(string) string) map[string]bool {
	hidden := map[string]bool{}
	for n, gate := range envGates {
		if gate(getenv) {
			hidden[n] = true
		}
	}
	return hidden
}

// HiddenCommands combines both gates.
func HiddenCommands(auth Auth, getenv func(string) string) map[string]bool {
	hidden := HiddenForAccount(auth)
	for n := range HiddenForEnv(getenv) {
		hidden[n] = true
	}
	return hidden
}
