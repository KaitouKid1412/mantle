package sessions

import (
	"slices"
	"testing"
)

func TestTreePlainChain(t *testing.T) {
	tr := loadFixture(t, sidPlain)
	main := tr.Main(BranchOptions{})
	want := []string{u("1a", 0), u("1u", 1), u("1b", 1), u("1b", 2), u("1s", 1), u("1u", 2), u("1b", 3), u("1s", 2)}
	if got := entryUUIDs(main); !slices.Equal(got, want) {
		t.Fatalf("main = %v", got)
	}
	if tr.Tree.Orphans != 0 || len(tr.Tree.Roots) != 1 {
		t.Fatalf("roots=%d orphans=%d", len(tr.Tree.Roots), tr.Tree.Orphans)
	}
	if got := tr.Title(); got != "Explain the build system" {
		t.Fatalf("title = %q", got)
	}
	if got := tr.FirstPrompt(); got != "Explain the build" {
		t.Fatalf("first prompt = %q", got)
	}
}

func TestTreeBranchesAndSidechains(t *testing.T) {
	tr := loadFixture(t, sidBranch)
	// The last-prompt leaf (the green prompt) lags behind its answer; the active leaf is
	// the newest leaf below it.
	leaf := tr.ActiveLeaf()
	if leaf == nil || leaf.Entry.UUID != u("4b", 22) {
		t.Fatalf("active leaf = %v", leaf)
	}
	want := []string{u("4u", 1), u("4b", 1), u("4u", 22), u("4b", 22)}
	if got := entryUUIDs(tr.Main(BranchOptions{})); !slices.Equal(got, want) {
		t.Fatalf("main = %v", got)
	}
	// The abandoned branch is still reachable.
	old := tr.Tree.Branch(tr.Tree.Node(u("4b", 21)), BranchOptions{})
	if got := entryUUIDs(old); !slices.Equal(got, []string{u("4u", 1), u("4b", 1), u("4u", 21), u("4b", 21)}) {
		t.Fatalf("old branch = %v", got)
	}
	if n := tr.Tree.Node(u("4b", 1)); len(n.Children) != 2 {
		t.Fatalf("fork point children = %d", len(n.Children))
	}
	leaves := tr.Tree.Leaves(false)
	if len(leaves) != 2 || len(tr.Tree.Leaves(true)) != 3 {
		t.Fatalf("leaves = %d / %d", len(leaves), len(tr.Tree.Leaves(true)))
	}
	// Without a hint the newest non-sidechain entry decides, never a sidechain.
	if got := tr.Tree.ActiveLeaf(""); got.Entry.UUID != u("4b", 22) {
		t.Fatalf("hintless leaf = %s", got.Entry.UUID)
	}
	// A hint on the old branch is honoured.
	if got := tr.Tree.ActiveLeaf(u("4u", 21)); got.Entry.UUID != u("4b", 21) {
		t.Fatalf("old-branch leaf = %s", got.Entry.UUID)
	}
	sc := Sidechains(tr.Entries)
	if len(sc) != 1 || len(sc["legacy1"]) != 2 {
		t.Fatalf("sidechains = %v", sc)
	}
}

func TestTreeCompaction(t *testing.T) {
	tr := loadFixture(t, sidCompact)
	main := tr.Main(BranchOptions{})
	want := []string{u("3c", 1), u("3u", 3), u("3u", 4), u("3b", 3)}
	if got := entryUUIDs(main); !slices.Equal(got, want) {
		t.Fatalf("main = %v", got)
	}
	if !main[0].IsCompactBoundary() || main[0].CompactMetadata.PreTokens != 12345 ||
		main[0].CompactMetadata.Trigger != "manual" || main[0].LogicalParentUUID != u("3b", 2) {
		t.Fatalf("boundary = %+v", main[0])
	}
	if main[1].IsPrompt() || !main[1].IsCompactSummary {
		t.Fatal("compact summary counted as a prompt")
	}
	full := tr.Main(BranchOptions{AcrossCompaction: true})
	want = append([]string{u("3u", 1), u("3b", 1), u("3u", 2), u("3b", 2)}, want...)
	if got := entryUUIDs(full); !slices.Equal(got, want) {
		t.Fatalf("full = %v", got)
	}
	if got := tr.FirstPrompt(); got != "Start a long task" {
		t.Fatalf("first prompt = %q", got)
	}
}

func TestTreeDegenerate(t *testing.T) {
	mk := func(uuid, parent string) *Entry {
		return &Entry{rec: rec{kind: KindUser}, UUID: uuid, ParentUUID: parent}
	}
	// Orphan parents, a two-node cycle, a self-parent, a duplicate uuid, a missing uuid.
	es := []*Entry{
		mk("a", "missing"), mk("b", "a"),
		mk("c", "d"), mk("d", "c"),
		mk("e", "e"),
		mk("b", "a"),
		mk("", "a"),
	}
	tr := NewTree(es)
	if tr.Len() != 5 {
		t.Fatalf("len = %d", tr.Len())
	}
	if tr.Orphans != 2 { // a (missing parent) and d (cycle)
		t.Fatalf("orphans = %d", tr.Orphans)
	}
	if tr.Node("b").Entry != es[5] {
		t.Fatal("duplicate uuid: last entry should win")
	}
	if got := entryUUIDs(tr.Branch(tr.Node("c"), BranchOptions{})); !slices.Equal(got, []string{"d", "c"}) {
		t.Fatalf("cycle branch = %v", got)
	}
	if NewTree(nil).ActiveLeaf("x") != nil {
		t.Fatal("empty tree leaf")
	}
}
