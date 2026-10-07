package research

import (
	"testing"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name    string
		entries []ent
		want    string // shape
		leaf    string // EngineLeaf
	}{
		{
			name: "empty",
			want: "",
		},
		{
			name: "chain",
			entries: []ent{
				prompt("p1", "", "first"), answer("a1", "p1", "one"),
				prompt("p2", "a1", "second"), answer("a2", "p2", "two"),
			},
			want: "p1(done,a1)[p2(done,a2)]",
			leaf: "p2",
		},
		{
			name: "preamble before the first prompt",
			entries: []ent{
				{"type": "attachment", "uuid": "h1", "parentUuid": "", "attachment": map[string]any{"type": "hook"}},
				prompt("p1", "h1", "first"), answer("a1", "p1", "one"),
			},
			want: "p1(done,a1)",
			leaf: "p1",
		},
		{
			name: "siblings in one file",
			entries: []ent{
				prompt("p1", "", "q1"), answer("a1", "p1", "one"),
				prompt("p2", "a1", "q2"), answer("a2", "p2", "two"),
				prompt("p3", "a1", "q3"), answer("a3", "p3", "three"),
			},
			want: "p1(done,a1)[p2(done,a2) p3(done,a3)]",
			leaf: "p3",
		},
		{
			name: "parallel tool results stay inside the node",
			entries: []ent{
				prompt("p1", "", "read two files"),
				toolCall("t1", "p1", "c1"), toolCall("t2", "t1", "c2"),
				toolResult("r2", "t2", "c2"), answer("a1", "r2", "both read"),
				toolResult("r1", "t1", "c1"), // written last, on a side branch
			},
			want: "p1(done,a1)",
			leaf: "p1",
		},
		{
			name: "turn ending at parallel results",
			entries: []ent{
				prompt("p1", "", "read two files"),
				toolCall("t1", "p1", "c1"), toolCall("t2", "t1", "c2"),
				toolResult("r1", "t1", "c1"), toolResult("r2", "t2", "c2"),
			},
			want: "p1(done,r2)",
			leaf: "p1",
		},
		{
			name: "compaction",
			entries: []ent{
				prompt("p1", "", "q1"), answer("a1", "p1", "one"),
				{"type": "system", "subtype": "compact_boundary", "uuid": "cb", "parentUuid": nil, "logicalParentUuid": "a1"},
				prompt("cs", "cb", "summary of the conversation").with("isCompactSummary", true),
				prompt("p2", "cs", "q2"), answer("a2", "p2", "two"),
			},
			// The leaf is the summary: a follow-up to p1 keeps the compacted context.
			want: "p1(done,cs)[p2(done,a2,c)]",
			leaf: "p2",
		},
		{
			name: "auto-compaction mid-turn keeps the answer in its node",
			entries: []ent{
				prompt("p1", "", "q1"), answer("a1", "p1", "partial"),
				{"type": "system", "subtype": "compact_boundary", "uuid": "cb", "parentUuid": nil, "logicalParentUuid": "a1"},
				prompt("cs", "cb", "summary").with("isCompactSummary", true),
				answer("a2", "cs", "rest"),
			},
			want: "p1(done,a2)",
			leaf: "p1",
		},
		{
			name: "orphans get a synthetic root",
			entries: []ent{
				answer("x1", "missing", "left over"),
				prompt("p1", "x1", "q1"), answer("a1", "p1", "one"),
				prompt("p2", "", "q2"), answer("a2", "p2", "two"),
			},
			want: "session(done,x1,s)[p1(done,a1)] p2(done,a2)",
			leaf: "p2",
		},
		{
			name: "commands, meta, local output and sidechains are not nodes",
			entries: []ent{
				prompt("p1", "", "q1"), answer("a1", "p1", "one"),
				prompt("m1", "a1", "<command-name>/model</command-name>"),
				prompt("m2", "m1", "<local-command-stdout>Set model</local-command-stdout>"),
				prompt("m3", "m2", "expanded skill text").with("isMeta", true),
				prompt("m4", "m3", "<bash-input>ls</bash-input>"),
				prompt("sc", "m4", "subagent task").with("isSidechain", true),
				prompt("p2", "m4", "q2"), answer("a2", "p2", "two"),
			},
			want: "p1(done,m4)[p2(done,a2)]",
			leaf: "p2",
		},
		{
			name: "interrupted and failed turns",
			entries: []ent{
				prompt("p1", "", "q1"), answer("a1", "p1", "par"),
				prompt("i1", "a1", "[Request interrupted by user]"),
				prompt("p2", "i1", "q2"), answer("a2", "p2", "API Error").with("isApiErrorMessage", true),
			},
			want: "p1(interrupted,i1)[p2(failed,a2)]",
			leaf: "p2",
		},
		{
			name: "image-only prompt is a node",
			entries: []ent{
				{"type": "user", "uuid": "p1", "parentUuid": "", "message": map[string]any{"role": "user",
					"content": []any{map[string]any{"type": "image", "source": map[string]any{"type": "base64"}}}}},
				answer("a1", "p1", "a cat"),
			},
			want: "p1(done,a1)",
			leaf: "p1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := Build(transcript(t, tt.entries...))
			if got := shape(tree); got != tt.want {
				t.Errorf("shape\n got %s\nwant %s", got, tt.want)
			}
			if tree.EngineLeaf != tt.leaf {
				t.Errorf("engine leaf = %q, want %q", tree.EngineLeaf, tt.leaf)
			}
		})
	}
}

func TestBuildQuote(t *testing.T) {
	tree := Build(transcript(t, prompt("p1", "", "> a tree is\n> a graph\n\nwhy acyclic?"), answer("a1", "p1", "because")))
	n := tree.Node("p1")
	if n.Quote != "a tree is\na graph" || n.Prompt != "why acyclic?" {
		t.Fatalf("quote %q prompt %q", n.Quote, n.Prompt)
	}
}

func TestBuildNil(t *testing.T) {
	tree := Build(nil)
	if tree == nil || tree.Len() != 0 || tree.Leaf() != nil || tree.Node("x") != nil {
		t.Fatal("nil transcript")
	}
	var nilTree *Tree
	if nilTree.Len() != 0 || nilTree.Leaf() != nil || nilTree.Node("x") != nil {
		t.Fatal("nil tree")
	}
	nilTree.Walk(func(*Node) bool { t.Fatal("walked a nil tree"); return false })
}

// The fixture is the spike's session: Q1, Q2; resume-at Q1 then Q3; rewind Q3 then Q4.
func TestBuildFixture(t *testing.T) {
	tr := fixture(t)
	tree := Build(tr)
	const q1, q2, q3, q4 = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"
	n1 := tree.Node(q1)
	if n1 == nil || len(tree.Roots) != 1 || tree.Roots[0] != n1 {
		t.Fatalf("roots %v", tree.Roots)
	}
	var kids []string
	for _, c := range n1.Children {
		kids = append(kids, c.ID)
		if c.Parent != n1 || len(c.Children) != 0 || c.LeafUUID == "" {
			t.Errorf("child %+v", c)
		}
		// Every follow-up hangs off Q1's leaf.
		if p := tr.Tree.Node(c.ID).Entry.ParentUUID; p != n1.LeafUUID {
			t.Errorf("%s hangs off %s, Q1 leaf %s", c.ID, p, n1.LeafUUID)
		}
	}
	if len(kids) != 3 || kids[0] != q2 || kids[1] != q3 || kids[2] != q4 {
		t.Fatalf("children %v", kids)
	}
	if tree.EngineLeaf != q4 || tree.Len() != 4 {
		t.Fatalf("engine leaf %s, len %d", tree.EngineLeaf, tree.Len())
	}
	if n1.Prompt != "Q1 what is a tree?" {
		t.Errorf("prompt %q", n1.Prompt)
	}
	var order []string
	tree.Walk(func(n *Node) bool { order = append(order, n.ID); return true })
	if len(order) != 4 || order[0] != q1 || order[3] != q4 {
		t.Errorf("walk %v", order)
	}
	if !n1.IsAncestorOf(tree.Node(q4)) || tree.Node(q4).IsAncestorOf(n1) || tree.Node(q4).Depth() != 1 {
		t.Error("ancestry")
	}
	if p := tree.Node(q3).Path(); len(p) != 2 || p[0] != n1 {
		t.Errorf("path %v", p)
	}
}
