package fakeapi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FakeAPIKey is a well-formed but invalid API key for tests.
const FakeAPIKey = "sk-ant-fake-key-for-tests-0000000000"

// SeedConfig writes configDir/.claude.json so that claude, run with
// CLAUDE_CONFIG_DIR=configDir, starts without onboarding, without asking to approve
// apiKey, and without the trust dialog for each directory in trusted. An existing file
// is replaced. Never point configDir at the user's real ~/.claude.
func SeedConfig(configDir, apiKey string, trusted ...string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("fakeapi: seed config: %w", err)
	}
	projects := map[string]any{}
	for _, dir := range trusted {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("fakeapi: seed config: %w", err)
		}
		// claude keys projects by its real cwd (/private/var/... on macOS).
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		projects[abs] = map[string]any{
			"allowedTools":                  []string{},
			"hasTrustDialogAccepted":        true,
			"hasCompletedProjectOnboarding": true,
			"projectOnboardingSeenCount":    1,
		}
	}
	cfg := map[string]any{
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "2.1.288",
		"lastReleaseNotesSeen":   "2.1.288",
		"numStartups":            1,
		"theme":                  "dark",
		"autoUpdates":            false,
		"customApiKeyResponses": map[string]any{
			"approved": []string{apiKeySuffix(apiKey)},
			"rejected": []string{},
		},
		"projects": projects,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configDir, ".claude.json"), data, 0o600); err != nil {
		return fmt.Errorf("fakeapi: seed config: %w", err)
	}
	return nil
}

// apiKeySuffix is the part of a key claude stores when the user approves it: the last
// 20 characters.
func apiKeySuffix(key string) string {
	if len(key) <= 20 {
		return key
	}
	return key[len(key)-20:]
}

// Env returns base (usually os.Environ()) prepared for running claude against fakeapi:
// variables that could route claude elsewhere or leak the parent session (ANTHROPIC_*,
// CLAUDE_*, CLAUDECODE, NODE_OPTIONS, DEBUG, cloud-provider switches) are removed, then
// the base URL, API key, isolated config dir and traffic switches are set.
func Env(base []string, baseURL, configDir, apiKey string) []string {
	out := make([]string, 0, len(base)+8)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if dropEnv(name) {
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		"ANTHROPIC_BASE_URL="+baseURL,
		"ANTHROPIC_API_KEY="+apiKey,
		"CLAUDE_CONFIG_DIR="+configDir,
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"DISABLE_TELEMETRY=1",
		"DISABLE_AUTOUPDATER=1",
		"DISABLE_ERROR_REPORTING=1",
	)
}

func dropEnv(name string) bool {
	switch name {
	case "CLAUDECODE", "NODE_OPTIONS", "DEBUG", "AWS_BEARER_TOKEN_BEDROCK":
		return true
	}
	for _, p := range []string{"ANTHROPIC_", "CLAUDE_", "OTEL_"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
