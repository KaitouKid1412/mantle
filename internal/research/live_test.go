package research

import "testing"

func TestLive(t *testing.T) {
	base := []ent{prompt("p1", "", "q1"), answer("a1", "p1", "one")}
	tree := Build(transcript(t, base...))

	n := tree.PromptSent("p2", "p1", "> quoted\n\nq2")
	if n.State != Running || n.Parent != tree.Node("p1") || tree.EngineLeaf != "p2" || n.Quote != "quoted" || n.Prompt != "q2" {
		t.Fatalf("sent: %+v leaf %s", n, tree.EngineLeaf)
	}
	if again := tree.PromptSent("p2", "", "other"); again != n || len(tree.Roots) != 1 {
		t.Fatal("duplicate PromptSent")
	}

	// The engine wrote the prompt but not the answer yet: the node stays Running.
	mid := Build(transcript(t, append(base, prompt("p2", "a1", "> quoted\n\nq2"))...))
	merged := tree.Merge(mid)
	if got := shape(merged); got != "p1(done,a1)[p2(running,)]" || merged.EngineLeaf != "p2" {
		t.Fatalf("mid merge: %s leaf %s", got, merged.EngineLeaf)
	}

	merged.TurnEnded("a2", Done)
	if got := shape(merged); got != "p1(done,a1)[p2(done,a2)]" {
		t.Fatalf("ended: %s", got)
	}
	merged.TurnEnded("zz", Failed) // not running: ignored
	if merged.Node("p2").State != Done {
		t.Fatal("TurnEnded on a finished node")
	}

	// After the result the JSONL is authoritative.
	full := Build(transcript(t, append(base, prompt("p2", "a1", "q2"), answer("a2", "p2", "two"), answer("a3", "a2", "more"))...))
	final := merged.Merge(full)
	if got := shape(final); got != "p1(done,a1)[p2(done,a3)]" {
		t.Fatalf("final: %s", got)
	}
}

func TestMergeKeepsLiveOnly(t *testing.T) {
	tree := Build(transcript(t, prompt("p1", "", "q1"), answer("a1", "p1", "one")))
	tree.PromptSent("p2", "p1", "q2")
	tree.PromptSent("p3", "p2", "q3") // a queued follow-up
	tree.PromptSent("r1", "", "new root")
	fresh := Build(transcript(t, prompt("p1", "", "q1"), answer("a1", "p1", "one")))
	got := tree.Merge(fresh)
	if s := shape(got); s != "p1(done,a1)[p2(running,)[p3(running,)]] r1(running,)" {
		t.Fatalf("merge: %s", s)
	}
	if got.EngineLeaf != "r1" || got.Node("p3").Parent != got.Node("p2") {
		t.Fatalf("leaf %s", got.EngineLeaf)
	}
	if (*Tree)(nil).Merge(nil).Len() != 0 {
		t.Fatal("nil merge")
	}
}

func TestMergeEngineLeaf(t *testing.T) {
	es := []ent{
		prompt("p1", "", "q1"), answer("a1", "p1", "one"),
		prompt("p2", "a1", "q2"), answer("a2", "p2", "two"),
	}
	live := Build(transcript(t, es...))
	live.SetEngineLeaf("p1") // after a rewind, before the next prompt is written
	live.SetEngineLeaf("zz") // unknown: ignored
	if got := live.Merge(Build(transcript(t, es...))); got.EngineLeaf != "p1" {
		t.Fatalf("leaf %s", got.EngineLeaf)
	}
}
