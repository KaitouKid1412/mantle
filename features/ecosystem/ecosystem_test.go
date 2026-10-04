package ecosystem

import (
	"testing"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestStateFeature(t *testing.T) {
	ecotest.ResetState(t)
	r, err := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Dialogs[eco.RestartDialogID]; !ok {
		t.Error("restart dialog not registered")
	}
	ctx := exttest.NewCtx()
	init := func(statuses ...string) *proto.SystemInit {
		in := &proto.SystemInit{}
		for i, s := range statuses {
			in.MCPServers = append(in.MCPServers, proto.MCPServerInfo{Name: string(rune('a' + i)), Status: s})
		}
		return in
	}
	send := func(ev proto.Event) []ext.MCPStatusMsg {
		var out []ext.MCPStatusMsg
		for _, m := range exttest.Exec(r.Dispatch(ctx, ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})) {
			if s, ok := m.(ext.MCPStatusMsg); ok {
				out = append(out, s)
			}
		}
		return out
	}

	st := send(init("connected", "needs-auth", "failed"))
	if len(st) != 1 || st[0].Total != 3 || st[0].NeedsAuth != 1 || st[0].Failed != 1 || st[0].EngineID != ext.MainEngine {
		t.Fatalf("status = %+v", st)
	}
	if len(ctx.Notices) != 1 || ctx.Notices[0].Key != "mcp.needs-auth" {
		t.Fatalf("notices = %+v", ctx.Notices)
	}
	// Same count again: status, but no new notice.
	send(init("connected", "needs-auth", "failed"))
	if len(ctx.Notices) != 1 {
		t.Errorf("repeat notice: %+v", ctx.Notices)
	}
	send(init("needs-auth", "needs-auth"))
	if len(ctx.Notices) != 2 || ctx.Notices[1].Text != "2 MCP servers need authentication · /mcp" {
		t.Errorf("notices = %+v", ctx.Notices)
	}
	// Non-init events update the tracker without a status message.
	if st := send(&proto.CommandsChanged{Commands: []proto.SlashCommand{{Name: "x"}}}); len(st) != 0 {
		t.Errorf("status on commands_changed: %+v", st)
	}
	if !eco.State.Engine("").HasCommand("x") {
		t.Error("tracker not fed")
	}
	exttest.Exec(r.Dispatch(ctx, ext.EngineDetachMsg{EngineID: ext.MainEngine}))
	if eco.State.Engine("").Init != nil {
		t.Error("detach should forget the engine")
	}
}
