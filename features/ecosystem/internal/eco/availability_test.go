package eco_test

import (
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestClassifyAccount(t *testing.T) {
	cases := []struct {
		acc  proto.Account
		want eco.Auth
	}{
		{proto.Account{}, eco.AuthUnknown},
		{proto.Account{Email: "u@example.test", SubscriptionType: "Claude Max", APIProvider: "firstParty"}, eco.AuthClaudeAI},
		{proto.Account{TokenSource: "claude.ai"}, eco.AuthClaudeAI},
		{proto.Account{APIKeySource: "ANTHROPIC_API_KEY", APIProvider: "firstParty"}, eco.AuthConsole},
		{proto.Account{Email: "u@example.test", TokenSource: "console"}, eco.AuthConsole},
		{proto.Account{APIProvider: "firstParty"}, eco.AuthConsole},
		{proto.Account{APIProvider: "bedrock"}, eco.AuthThirdParty},
		{proto.Account{APIProvider: "vertex", SubscriptionType: "Claude Max"}, eco.AuthThirdParty},
		{proto.Account{APIProvider: "gateway"}, eco.AuthThirdParty},
	}
	for _, c := range cases {
		if got := eco.ClassifyAccount(c.acc); got != c.want {
			t.Errorf("ClassifyAccount(%+v) = %v, want %v", c.acc, got, c.want)
		}
	}
}

func TestHiddenCommands(t *testing.T) {
	none := func(string) string { return "" }
	if h := eco.HiddenCommands(eco.AuthUnknown, none); h["teleport"] || h["logout"] || !h["setup-bedrock"] {
		t.Errorf("unknown account hides only environment-gated commands: %v", h)
	}
	if h := eco.HiddenCommands(eco.AuthClaudeAI, none); h["teleport"] || h["chrome"] || h["install-github-app"] {
		t.Errorf("claude.ai sees everything: %v", h)
	}
	h := eco.HiddenCommands(eco.AuthConsole, none)
	if !h["teleport"] || !h["chrome"] || !h["artifacts"] || !h["upgrade"] || h["install-github-app"] || h["logout"] || h["mcp"] {
		t.Errorf("console: %v", h)
	}
	h = eco.HiddenCommands(eco.AuthThirdParty, none)
	if !h["teleport"] || !h["install-github-app"] || !h["logout"] || h["login"] {
		t.Errorf("third party: %v", h)
	}
	env := map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "DISABLE_DOCTOR_COMMAND": "true", "DISABLE_LOGIN_COMMAND": "0"}
	h = eco.HiddenCommands(eco.AuthClaudeAI, func(k string) string { return env[k] })
	if h["setup-bedrock"] || !h["setup-vertex"] || !h["doctor"] || h["login"] {
		t.Errorf("environment gates: %v", h)
	}
}
