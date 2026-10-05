package ecosystem_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	ecosystem "github.com/KaitouKid1412/mantle/features/ecosystem"
	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

type probeMsg struct{}

// TestVisibilityInHost checks the overlay end to end: the host's command list
// (what the / menu shows) drops claude.ai-only commands for an API-key account
// and brings them back for a subscription, while typed names still resolve.
func TestVisibilityInHost(t *testing.T) {
	old := ecosystem.Getenv
	ecosystem.Getenv = func(string) string { return "" }
	t.Cleanup(func() { ecosystem.Getenv = old })

	var mu sync.Mutex
	var listed []string
	var resolves bool
	var overlays []ext.CommandVisibilityMsg
	probe := ext.Feature{ID: "test.probe", Order: 900, Setup: func(r ext.Registrar) error {
		ext.Subscribe(r, "probe.vis", func(ctx ext.Ctx, m ext.CommandVisibilityMsg) tea.Cmd {
			mu.Lock()
			overlays = append(overlays, m)
			mu.Unlock()
			return nil
		})
		ext.Subscribe(r, "probe", func(ctx ext.Ctx, m probeMsg) tea.Cmd {
			mu.Lock()
			defer mu.Unlock()
			listed = listed[:0]
			for _, c := range ctx.Commands() {
				listed = append(listed, c.Name)
			}
			_, resolves = ctx.Command("teleport")
			return nil
		})
		return nil
	}}
	host := app.NewHost(append(ext.Pending(), probe), app.HostOptions{})
	root := app.New(app.Options{Host: host, Settings: exttest.NewSettings(nil), NoBackgroundQuery: true,
		Clock: &exttest.Clock{T: time.Unix(0, 0)}})
	hs := testkit.New(t, root, testkit.WithSize(80, 24))

	check := func(account string, wantTeleport bool) {
		t.Helper()
		hs.SendMsg(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize,
			Resp: []byte(`{"account":` + account + `}`)})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			hs.SendMsg(probeMsg{})
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			has, mcp, ok := slices.Contains(listed, "teleport"), slices.Contains(listed, "mcp"), resolves
			mu.Unlock()
			if has == wantTeleport && mcp && ok {
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("account %s: overlays=%+v teleport listed=%v (want %v), resolves=%v, commands=%v", account, overlays,
			slices.Contains(listed, "teleport"), wantTeleport, resolves, listed)
	}
	check(`{"apiKeySource":"ANTHROPIC_API_KEY","apiProvider":"firstParty"}`, false)
	check(`{"subscriptionType":"Claude Max","apiProvider":"firstParty"}`, true)
}
