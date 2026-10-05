package ecosystem

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/ecotest"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func visibilityIn(msgs []tea.Msg) *ext.CommandVisibilityMsg {
	for _, m := range msgs {
		if v, ok := m.(ext.CommandVisibilityMsg); ok {
			return &v
		}
	}
	return nil
}

func TestCommandVisibility(t *testing.T) {
	ecotest.ResetState(t)
	old := Getenv
	Getenv = func(string) string { return "" }
	t.Cleanup(func() { Getenv = old })
	r, err := exttest.Setup(ext.Feature{ID: FeatureID, Setup: Setup})
	if err != nil {
		t.Fatal(err)
	}
	ctx := exttest.NewCtx()

	// At start: environment gates only.
	var start *ext.CommandVisibilityMsg
	for _, s := range r.Starts {
		if fn, ok := s.Value.(func(ext.Ctx) tea.Cmd); ok {
			if v := visibilityIn(exttest.Exec(fn(ctx))); v != nil {
				start = v
			}
		}
	}
	if start == nil || start.Source != EnvVisibilitySource || !start.Hidden["setup-vertex"] || start.Hidden["teleport"] {
		t.Fatalf("start overlay = %+v", start)
	}

	initialize := func(engineID, account string) *ext.CommandVisibilityMsg {
		return visibilityIn(exttest.Exec(r.Dispatch(ctx, ext.ControlResultMsg{EngineID: engineID,
			Subtype: proto.SubInitialize, Resp: []byte(`{"account":` + account + `}`)})))
	}
	// An API-key account hides the claude.ai-only commands.
	v := initialize(ext.MainEngine, `{"apiKeySource":"ANTHROPIC_API_KEY","apiProvider":"firstParty"}`)
	if v == nil || v.Source != AccountVisibilitySource || !v.Hidden["teleport"] || !v.Hidden["chrome"] || v.Hidden["install-github-app"] {
		t.Fatalf("console overlay = %+v", v)
	}
	// After /login with a subscription, the restarted engine's initialize shows them again.
	v = initialize(ext.MainEngine, `{"subscriptionType":"Claude Pro","apiProvider":"firstParty"}`)
	if v == nil || v.Hidden["teleport"] || v.Hidden["chrome"] {
		t.Fatalf("claude.ai overlay = %+v", v)
	}
	// Other engines are ignored.
	if v := initialize("builder", `{"apiProvider":"bedrock"}`); v != nil {
		t.Errorf("builder engine changed visibility: %+v", v)
	}
}
