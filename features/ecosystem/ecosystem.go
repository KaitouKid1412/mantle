// Package ecosystem registers the panels that manage Claude Code's ecosystem:
// /mcp, /plugin, /skills, /hooks, /agents, /memory, /doctor, /login, /logout,
// /import and the hand-off commands for cloud and product features. Each panel
// is its own feature in a subpackage; this package links them and runs the
// shared engine-state tracker.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package ecosystem

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/pkg/ext"

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
		if !ok || after.NeedsAuth == 0 || after.NeedsAuth == before.NeedsAuth {
			return nil
		}
		return ctx.Notify(MCPNeedsAuthNotice(after.NeedsAuth))
	})
	ext.Subscribe(r, FeatureID+".detach", func(ctx ext.Ctx, m ext.EngineDetachMsg) tea.Cmd {
		eco.State.Forget(m.EngineID)
		return nil
	})
	r.AddDialog(eco.RestartDialogID, eco.Factory(eco.NewRestartDialog))
	return nil
}

// MCPNeedsAuthNotice is the notice raised when MCP servers need OAuth. The
// footer (plan 07) can match its Key until pkg/ext carries a typed message
// (docs/plans/requests/09-01-mcp-status-msg.md).
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
