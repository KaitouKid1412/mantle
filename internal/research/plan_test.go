package research

import "testing"

func TestPlan(t *testing.T) {
	// p1 ─ p2 ─ p3 (engine leaf)
	//    └ p4
	branched := []ent{
		prompt("p1", "", "q1"), answer("a1", "p1", "one"),
		prompt("p4", "a1", "q4"), answer("a4", "p4", "four"),
		prompt("p2", "a1", "q2"), answer("a2", "p2", "two"),
		prompt("p3", "a2", "q3"), answer("a3", "p3", "three"),
	}
	// p1, compaction, p2 ─ p3 (engine leaf); p1 ─ p5 before the compaction.
	compacted := []ent{
		prompt("p1", "", "q1"), answer("a1", "p1", "one"),
		prompt("p5", "a1", "q5"), answer("a5", "p5", "five"),
		{"type": "system", "subtype": "compact_boundary", "uuid": "cb", "parentUuid": nil, "logicalParentUuid": "a1"},
		prompt("cs", "cb", "summary").with("isCompactSummary", true),
		prompt("p2", "cs", "q2"), answer("a2", "p2", "two"),
		prompt("p3", "a2", "q3"), answer("a3", "p3", "three"),
	}
	tests := []struct {
		name    string
		entries []ent
		live    func(*Tree)
		viewing string
		busy    bool
		want    SendPlan
	}{
		{name: "empty session", viewing: "", want: SendPlan{Kind: Plain}},
		{name: "tip", entries: branched, viewing: "p3", want: SendPlan{Kind: Plain}},
		{name: "tip while busy queues", entries: branched, viewing: "p3", busy: true, want: SendPlan{Kind: Plain}},
		{name: "no viewing means tip", entries: branched, want: SendPlan{Kind: Plain}},
		{name: "parent of tip", entries: branched, viewing: "p2", want: SendPlan{Kind: Rewind, At: "a2", DropPrompt: "p3"}},
		{name: "root", entries: branched, viewing: "p1", want: SendPlan{Kind: Rewind, At: "a1", DropPrompt: "p2"}},
		{name: "off branch", entries: branched, viewing: "p4", want: SendPlan{Kind: Resume, At: "a4"}},
		{name: "busy off tip", entries: branched, viewing: "p1", busy: true, want: SendPlan{Kind: Blocked, Reason: ReasonBusy}},
		{name: "unknown", entries: branched, viewing: "zz", want: SendPlan{Kind: Blocked, Reason: ReasonUnknown}},
		{
			name: "running tip", entries: branched, viewing: "p6",
			live: func(t *Tree) { t.PromptSent("p6", "p3", "q6") },
			want: SendPlan{Kind: Blocked, Reason: ReasonRunning},
		},
		{
			name: "running elsewhere", entries: branched, viewing: "p4",
			live: func(t *Tree) { t.Node("p4").State = Running },
			want: SendPlan{Kind: Blocked, Reason: ReasonRunning},
		},
		{
			name: "no leaf", entries: branched, viewing: "p4",
			live: func(t *Tree) { t.Node("p4").LeafUUID = "" },
			want: SendPlan{Kind: Blocked, Reason: ReasonNoLeaf},
		},
		{name: "interrupted node can branch", entries: []ent{
			prompt("p1", "", "q1"), answer("a1", "p1", "par"), prompt("i1", "a1", "[Request interrupted by user]"),
			prompt("p2", "i1", "q2"), answer("a2", "p2", "two"),
		}, viewing: "p1", want: SendPlan{Kind: Rewind, At: "i1", DropPrompt: "p2"}},
		{name: "compacted: at the boundary's node keeps compaction", entries: compacted, viewing: "p1",
			want: SendPlan{Kind: Rewind, At: "cs", DropPrompt: "p2"}},
		{name: "compacted: below the boundary", entries: compacted, viewing: "p2",
			want: SendPlan{Kind: Rewind, At: "a2", DropPrompt: "p3"}},
		{name: "compacted: branch from before the boundary reloads", entries: compacted, viewing: "p5",
			want: SendPlan{Kind: Resume, At: "a5", NeedsConfirm: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := Build(transcript(t, tt.entries...))
			if tt.live != nil {
				tt.live(tree)
			}
			if got := Plan(tree, tt.viewing, tt.busy); got != tt.want {
				t.Errorf("Plan = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPlanCompactionAbove(t *testing.T) {
	// p0 ─ p1 ─(compaction in p1's span)─ p2 (leaf). Branching at p0 drops it.
	tree := Build(transcript(t,
		prompt("p0", "", "q0"), answer("a0", "p0", "zero"),
		prompt("p1", "a0", "q1"), answer("a1", "p1", "one"),
		ent{"type": "system", "subtype": "compact_boundary", "uuid": "cb", "parentUuid": nil, "logicalParentUuid": "a1"},
		prompt("cs", "cb", "summary").with("isCompactSummary", true),
		prompt("p2", "cs", "q2"), answer("a2", "p2", "two"),
	))
	if got := Plan(tree, "p0", false); got.Kind != Rewind || got.DropPrompt != "p1" || !got.NeedsConfirm {
		t.Fatalf("plan = %+v", got)
	}
}

func TestPlanNilTree(t *testing.T) {
	if got := Plan(nil, "", false); got.Kind != Plain {
		t.Fatalf("nil tree, tip: %+v", got)
	}
	if got := Plan(nil, "x", false); got.Kind != Blocked {
		t.Fatalf("nil tree, x: %+v", got)
	}
}

func TestKindStrings(t *testing.T) {
	for k, want := range map[SendKind]string{Plain: "plain", Rewind: "rewind", Resume: "resume", Blocked: "blocked", 9: "unknown"} {
		if k.String() != want {
			t.Errorf("%d = %s", k, k)
		}
	}
	if NodeState(9).String() != "unknown" || Failed.String() != "failed" {
		t.Error("state strings")
	}
}
