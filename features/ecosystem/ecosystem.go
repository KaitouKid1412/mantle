// Package ecosystem registers the panels that manage Claude Code's ecosystem:
// /mcp, /plugin, /skills, /hooks, /agents, /memory, /doctor, /login, /logout,
// /import and the hand-off commands for cloud and product features. Each panel
// is its own feature in a subpackage; this package links them and runs the
// shared engine-state tracker.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package ecosystem

import (
	"encoding/json"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"

	_ "github.com/KaitouKid1412/mantle/features/ecosystem/agents"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/agentview"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/auth"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/doctor"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/handoff"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/hooks"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/importcfg"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/mcp"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/memory"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/plugins"
	_ "github.com/KaitouKid1412/mantle/features/ecosystem/skills"
)

// FeatureID is the shared state feature every panel builds on.
const FeatureID = "ecosystem.state"

func init() {
	ext.Register(ext.Feature{
		ID:    FeatureID,
		Order: 300,
		Setup: Setup,
	})
}

// Setup feeds the tracker from engine events, warns when MCP servers need
// authentication, and registers the restart confirmation.
func Setup(r ext.Registrar) error {
	ext.Subscribe(r, FeatureID+".events", func(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
		before, after, ok := eco.State.Observe(m.EngineID, m.Event)
		if !ok {
			return nil
		}
		status := ext.Msg(MCPStatus(m.EngineID, after))
		if after.NeedsAuth == 0 || after.NeedsAuth <= before.NeedsAuth {
			return status
		}
		return tea.Batch(status, ctx.Notify(MCPNeedsAuthNotice(after.NeedsAuth)))
	})
	ext.Subscribe(r, FeatureID+".detach", func(ctx ext.Ctx, m ext.EngineDetachMsg) tea.Cmd {
		eco.State.Forget(m.EngineID)
		return nil
	})
	r.AddDialog(eco.RestartDialogID, eco.Factory(eco.NewRestartDialog))

	// Hide the commands Claude Code hides for this account and environment:
	// environment gates at start, then again with the account from every
	// initialize reply (a new one follows the restart after /login).
	r.OnStart(FeatureID+".visibility", func(ctx ext.Ctx) tea.Cmd {
		return ext.Msg(EnvVisibility())
	})
	ext.Subscribe(r, FeatureID+".initialize", func(ctx ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
		if m.Subtype != proto.SubInitialize || m.Err != nil || (m.EngineID != "" && m.EngineID != ext.MainEngine) {
			return nil
		}
		var resp proto.InitializeResponse
		if json.Unmarshal(m.Resp, &resp) != nil {
			return nil
		}
		return ext.Msg(AccountVisibility(eco.ClassifyAccount(resp.Account)))
	})
	return nil
}

// CommandVisibilityMsg sources. The host ORs overlays, so the environment gates
// (sent at start) and the account gates (sent on each initialize) never undo
// each other, whatever order they arrive in.
const (
	EnvVisibilitySource     = "ecosystem.env"
	AccountVisibilitySource = "ecosystem.account"
)

// Getenv reads the environment for the visibility gates; tests replace it.
var Getenv = os.Getenv

// EnvVisibility hides the commands the environment turns off.
func EnvVisibility() ext.CommandVisibilityMsg {
	return ext.CommandVisibilityMsg{Source: EnvVisibilitySource, Hidden: eco.HiddenForEnv(Getenv)}
}

// AccountVisibility hides the commands Claude Code hides for an account kind.
func AccountVisibility(auth eco.Auth) ext.CommandVisibilityMsg {
	return ext.CommandVisibilityMsg{Source: AccountVisibilitySource, Hidden: eco.HiddenForAccount(auth)}
}

// MCPStatus converts tracker counts to the footer's message.
func MCPStatus(engineID string, c eco.MCPCounts) ext.MCPStatusMsg {
	if engineID == "" {
		engineID = ext.MainEngine
	}
	return ext.MCPStatusMsg{EngineID: engineID, Total: c.Total, Connected: c.Connected, NeedsAuth: c.NeedsAuth, Failed: c.Failed}
}

// MCPNeedsAuthNotice is the notice raised when more MCP servers need OAuth.
func MCPNeedsAuthNotice(n int) ext.Notice {
	s := "s need"
	if n == 1 {
		s = " needs"
	}
	return ext.Notice{
		Key:    "mcp.needs-auth",
		Level:  ext.NoticeWarning,
		Text:   fmt.Sprintf("%d MCP server%s authentication · /mcp", n, s),
		Source: FeatureID,
	}
}
