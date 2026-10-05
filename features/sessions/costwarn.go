package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/config"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Cost warning (CU-10). Once a session's cost passes costThreshold, people whose
// account pays for usage are told once, as in Claude Code. Who counts follows Claude
// Code: a usage-billed subscription, or an organization or workspace admin or billing
// role; DISABLE_COST_WARNINGS turns it off. Acknowledging it stops it for good; mantle
// keeps that in its own state and also honours Claude Code's acknowledgement.
const (
	DialogCostWarning = "dialog.costWarning"
	costThreshold     = 5.0 // USD
	costAckKey        = "costThresholdAcknowledged"
	costEnv           = "DISABLE_COST_WARNINGS"
)

func (f *feature) registerCostWarning(r ext.Registrar) {
	r.AddDialog(DialogCostWarning, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		cost, _ := args.(float64)
		return &choiceDialog{
			id:       DialogCostWarning,
			title:    fmt.Sprintf("This session has cost $%.2f so far", cost),
			subtitle: "It is billed to your account. /cost shows where it went; set " + costEnv + "=1 to stop these warnings.",
			choices: []choice{
				{label: "Got it, don't warn me again", run: func(ctx ext.Ctx) tea.Cmd {
					_ = ctx.Store(FeatureID).Set(costAckKey, true)
					return nil
				}},
				{label: "Remind me in a later session", run: func(ext.Ctx) tea.Cmd { return nil }},
			},
		}, nil
	})
	ext.Subscribe(r, "sessions.account", func(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
		if m.Subtype == proto.SubInitialize && m.Err == nil && (m.EngineID == "" || m.EngineID == ext.MainEngine) {
			var r proto.InitializeResponse
			if decodeControl(m, &r) == nil {
				f.account = r.Account
			}
		}
		return nil
	})
}

// maybeWarnCost runs when a main-engine turn ends. It checks once per run, like Claude
// Code: the first time the cost is over the threshold.
func (f *feature) maybeWarnCost(ctx ext.Ctx) tea.Cmd {
	cost := f.usage(ext.MainEngine).costUSD
	if f.costChecked || cost < costThreshold {
		return nil
	}
	f.costChecked = true
	if !f.costWarningsApply(ctx) || f.costAcknowledged(ctx) {
		return nil
	}
	return ctx.OpenDialog(DialogCostWarning, cost)
}

// costWarningsApply reports whether this account gets cost warnings.
func (f *feature) costWarningsApply(ctx ext.Ctx) bool {
	if truthy(os.Getenv(costEnv)) || truthy(settingsEnv(ctx, costEnv)) {
		return false
	}
	oa, _ := f.global(ctx, "oauthAccount").(map[string]any)
	str := func(k string) string { s, _ := oa[k].(string); return s }
	if f.account.SubscriptionType != "" {
		return str("billingType") == "usage_based"
	}
	org, ws := str("organizationRole"), str("workspaceRole")
	if org == "" || ws == "" {
		return false
	}
	return org == "admin" || org == "billing" || ws == "workspace_admin" || ws == "workspace_billing"
}

func (f *feature) costAcknowledged(ctx ext.Ctx) bool {
	var ack bool
	if ok, _ := ctx.Store(FeatureID).Get(costAckKey, &ack); ok && ack {
		return true
	}
	ack, _ = f.global(ctx, "hasAcknowledgedCostThreshold").(bool)
	return ack
}

// globalConfig reads a key of Claude Code's global config (~/.claude.json, or the one
// in $CLAUDE_CONFIG_DIR; read-only). It is read when needed: once per run at most.
func globalConfig(_ ext.Ctx, key string) any {
	p, err := config.DefaultPaths("")
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(p.ClaudeJSON)
	if err != nil {
		return nil
	}
	var g map[string]any
	if json.Unmarshal(raw, &g) != nil {
		return nil
	}
	return g[key]
}

// settingsEnv is a variable from the settings files' env map (the engine gets these;
// mantle's own environment does not).
func settingsEnv(ctx ext.Ctx, name string) string {
	env, _ := ctx.Settings().Claude("env")
	m, _ := env.(map[string]any)
	s, _ := m[name].(string)
	return s
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
