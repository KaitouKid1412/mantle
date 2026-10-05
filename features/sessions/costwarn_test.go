package sessions

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func costHarness(t *testing.T, global map[string]any, sub string) *harness {
	t.Helper()
	t.Setenv(costEnv, "")
	h := newHarness(t)
	h.f.global = func(_ ext.Ctx, k string) any { return global[k] }
	h.f.account = proto.Account{SubscriptionType: sub}
	return h
}

func admin() map[string]any {
	return map[string]any{"oauthAccount": map[string]any{"organizationRole": "admin", "workspaceRole": "workspace_developer"}}
}

func TestCostWarning(t *testing.T) {
	h := costHarness(t, admin(), "")
	h.run(ext.Msg(event(&proto.Result{TotalCostUSD: 4.99})))
	if len(h.ctx.Opened) != 0 {
		t.Fatalf("under the threshold: opened %v", h.ctx.Opened)
	}
	h.run(ext.Msg(event(&proto.Result{TotalCostUSD: 5.2})))
	if !slices.Equal(h.ctx.Opened, []string{DialogCostWarning}) {
		t.Fatalf("over the threshold: opened %v", h.ctx.Opened)
	}
	h.run(ext.Msg(event(&proto.Result{TotalCostUSD: 9})))
	if len(h.ctx.Opened) != 1 {
		t.Fatalf("checked once per run: opened %v", h.ctx.Opened)
	}

	d := h.openDialog(DialogCostWarning, 5.2).(*choiceDialog)
	if v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 100}).Text); !strings.Contains(v, "$5.20") || !strings.Contains(v, costEnv) {
		t.Fatalf("dialog:\n%s", v)
	}
	_, cmd := d.HandleAction(h.ctx, ext.ActSelectAccept)
	h.run(cmd)
	if !h.f.costAcknowledged(h.ctx) {
		t.Fatal("acknowledging is not remembered")
	}

	// Acknowledged in mantle or in Claude Code: no warning.
	h2 := costHarness(t, admin(), "")
	_ = h2.ctx.Store(FeatureID).Set(costAckKey, true)
	g := admin()
	g["hasAcknowledgedCostThreshold"] = true
	h3 := costHarness(t, g, "")
	for _, x := range []*harness{h2, h3} {
		x.run(ext.Msg(event(&proto.Result{TotalCostUSD: 6})))
		if len(x.ctx.Opened) != 0 {
			t.Fatalf("acknowledged: opened %v", x.ctx.Opened)
		}
	}
}

func TestCostWarningWho(t *testing.T) {
	role := func(org, ws string) map[string]any {
		return map[string]any{"oauthAccount": map[string]any{"organizationRole": org, "workspaceRole": ws}}
	}
	usage := map[string]any{"oauthAccount": map[string]any{"billingType": "usage_based"}}
	for _, c := range []struct {
		name   string
		global map[string]any
		sub    string
		env    string
		want   bool
	}{
		{"org admin", role("admin", "workspace_developer"), "", "", true},
		{"workspace billing", role("user", "workspace_billing"), "", "", true},
		{"developer", role("user", "workspace_developer"), "", "", false},
		{"plain API key", nil, "", "", false},
		{"subscription", role("admin", "workspace_admin"), "max", "", false},
		{"usage-billed subscription", usage, "team", "", true},
		{"turned off", role("admin", "workspace_admin"), "", "1", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := costHarness(t, c.global, c.sub)
			t.Setenv(costEnv, c.env)
			if got := h.f.costWarningsApply(h.ctx); got != c.want {
				t.Fatalf("applies = %v, want %v", got, c.want)
			}
		})
	}
}
