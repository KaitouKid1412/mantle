package sessions

// Node is an entry in the parentUuid tree.
type Node struct {
	Entry    *Entry
	Parent   *Node
	Children []*Node // in file order

	index  int // file order of the entry
	newest int // highest index in this subtree
}

// Tree is the parentUuid forest of a transcript's entries. Rewinds and edits leave
// several branches in one file; compact boundaries start new chains whose
// LogicalParentUUID points back into the old one.
type Tree struct {
	Roots []*Node
	// Orphans counts entries whose parent uuid is not in the file. They are treated as
	// roots.
	Orphans int

	nodes  []*Node
	byUUID map[string]*Node
}

// NewTree builds the tree from entries in file order. Entries without a uuid are
// ignored. When a uuid repeats, the last entry wins and keeps the first's position.
func NewTree(entries []*Entry) *Tree {
	t := &Tree{byUUID: make(map[string]*Node, len(entries))}
	for _, e := range entries {
		if e.UUID == "" {
			continue
		}
		if n, ok := t.byUUID[e.UUID]; ok {
			n.Entry = e
			continue
		}
		n := &Node{Entry: e, index: len(t.nodes)}
		t.nodes = append(t.nodes, n)
		t.byUUID[e.UUID] = n
	}
	for _, n := range t.nodes {
		p := n.Entry.ParentUUID
		if p == "" || p == n.Entry.UUID {
			t.Roots = append(t.Roots, n)
			continue
		}
		parent, ok := t.byUUID[p]
		if !ok || createsCycle(n, parent) {
			t.Orphans++
			t.Roots = append(t.Roots, n)
			continue
		}
		n.Parent = parent
		parent.Children = append(parent.Children, n)
	}
	for _, r := range t.Roots {
		computeNewest(r)
	}
	return t
}

// createsCycle reports whether making parent the parent of n would close a loop.
func createsCycle(n, parent *Node) bool {
	for p := parent; p != nil; p = p.Parent {
		if p == n {
			return true
		}
	}
	return false
}

func computeNewest(root *Node) {
	// Iterative post-order: transcripts can be deep enough to make recursion costly.
	type frame struct {
		n    *Node
		next int
	}
	stack := []frame{{n: root}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next == 0 {
			f.n.newest = f.n.index
		}
		if f.next < len(f.n.Children) {
			c := f.n.Children[f.next]
			f.next++
			stack = append(stack, frame{n: c})
			continue
		}
		done := f.n
		stack = stack[:len(stack)-1]
		if done.Parent != nil && done.newest > done.Parent.newest {
			done.Parent.newest = done.newest
		}
	}
}

// Len is the number of nodes.
func (t *Tree) Len() int { return len(t.nodes) }

// Node returns the node for uuid, or nil.
func (t *Tree) Node(uuid string) *Node { return t.byUUID[uuid] }

// Leaves returns nodes without children, in file order. Sidechain leaves are included
// only if sidechains is true.
func (t *Tree) Leaves(sidechains bool) []*Node {
	var out []*Node
	for _, n := range t.nodes {
		if len(n.Children) == 0 && (sidechains || !n.Entry.IsSidechain) {
			out = append(out, n)
		}
	}
	return out
}

// ActiveLeaf picks the leaf a resume continues from. hint is the leaf named by the last
// last-prompt record; if it is in the tree, the newest leaf below it is returned (the
// hint can lag behind the last messages). Otherwise the newest non-sidechain entry's
// newest descendant is used. It returns nil for an empty tree.
func (t *Tree) ActiveLeaf(hint string) *Node {
	start := t.byUUID[hint]
	if start == nil {
		for i := len(t.nodes) - 1; i >= 0; i-- {
			if !t.nodes[i].Entry.IsSidechain {
				start = t.nodes[i]
				break
			}
		}
	}
	if start == nil {
		return nil
	}
	return newestLeaf(start)
}

func newestLeaf(n *Node) *Node {
	for len(n.Children) > 0 {
		best := n.Children[0]
		for _, c := range n.Children[1:] {
			if c.newest > best.newest {
				best = c
			}
		}
		n = best
	}
	return n
}

// BranchOptions controls Branch.
type BranchOptions struct {
	// AcrossCompaction continues past compact boundaries into the history they
	// summarized (via LogicalParentUUID). Without it the branch starts at the most recent
	// boundary, which is what the engine itself loads.
	AcrossCompaction bool
	// Sidechains keeps sidechain entries (subagent messages stored inline by older
	// engines). By default they are dropped.
	Sidechains bool
}

// Branch returns the entries from the root to leaf, in order.
func (t *Tree) Branch(leaf *Node, opts BranchOptions) []*Entry {
	var rev []*Entry
	seen := map[*Node]bool{}
	for n := leaf; n != nil && !seen[n]; {
		seen[n] = true
		if opts.Sidechains || !n.Entry.IsSidechain {
			rev = append(rev, n.Entry)
		}
		if n.Parent != nil {
			n = n.Parent
			continue
		}
		if opts.AcrossCompaction && n.Entry.LogicalParentUUID != "" {
			n = t.byUUID[n.Entry.LogicalParentUUID]
			continue
		}
		break
	}
	out := make([]*Entry, len(rev))
	for i, e := range rev {
		out[len(rev)-1-i] = e
	}
	return out
}
