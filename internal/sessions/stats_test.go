package sessions

import "testing"

func TestSessionTotals(t *testing.T) {
	plain := SessionTotals(loadFixture(t, sidPlain))
	if plain.CostUSD != 0.0123 || plain.LinesAdded != 3 || plain.Models["claude-opus-5-5"].CacheReadInputTokens != 50 {
		t.Fatalf("plain totals = %+v", plain)
	}

	// No cost-state: usage summed from assistant records, one count per message id.
	tools := SessionTotals(loadFixture(t, sidTools))
	u := tools.Models["claude-opus-5-5"]
	if tools.CostUSD != 0 || u.InputTokens != 30 || u.OutputTokens != 15 {
		t.Fatalf("tools totals = %+v", tools)
	}

	// msg_01 is split across two records repeating the same usage.
	usage := UsageFromEntries(loadFixture(t, sidPlain).Entries)
	if got := usage["claude-opus-5-5"]; got.InputTokens != 300 || got.OutputTokens != 50 || got.CacheCreationInputTokens != 10 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestCostTotalsAdd(t *testing.T) {
	var a CostTotals
	a.Add(CostTotals{CostUSD: 1, Models: map[string]ModelUsage{"m": {InputTokens: 1, CostUSD: 1}}})
	a.Add(CostTotals{CostUSD: 2, UnknownModelCost: true, Models: map[string]ModelUsage{"m": {OutputTokens: 2, CostUSD: 2}, "n": {InputTokens: 5}}})
	if a.CostUSD != 3 || !a.UnknownModelCost || a.Models["m"].Tokens() != 3 || a.Tokens() != 8 {
		t.Fatalf("totals = %+v", a)
	}
}
