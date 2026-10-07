// Package research models a session as a tree of question/answer nodes for research
// mode. It is pure: it reads a parsed transcript (internal/sessions) and keeps no UI
// state beyond the sidecar file.
package research

import (
	"regexp"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/sessions"
)

// NodeState is the state of a node's answer turn.
type NodeState int

const (
	Done NodeState = iota
	Running
	Interrupted
	Failed
)

func (s NodeState) String() string {
	switch s {
	case Done:
		return "done"
	case Running:
		return "running"
	case Interrupted:
		return "interrupted"
	case Failed:
		return "failed"
	}
	return "unknown"
}

// SyntheticID is the ID of the synthetic session root that holds chains whose parent is
// missing from the file.
const SyntheticID = "session"

// Node is one user question plus Claude's full answer turn.
type Node struct {
	ID       string // the prompt's uuid (transcript item "user:<ID>")
	Parent   *Node
	Children []*Node // file order; live ones appended
	Prompt   string  // the question, quote stripped (bar label)
	Quote    string  // the leading "> " block without its markers, if any
	// LeafUUID is the last transcript entry of this turn on its main path: the entry a
	// follow-up hangs off. It is "" while the turn runs.
	LeafUUID string
	State    NodeState
	// Compacted is set when a compact boundary sits between the parent and this node.
	Compacted bool
	// Synthetic marks the session root made for orphaned chains; it has no prompt.
	Synthetic bool
}

// Depth is the number of ancestors.
func (n *Node) Depth() int {
	d := 0
	for p := n.Parent; p != nil; p = p.Parent {
		d++
	}
	return d
}

// Path returns the nodes from the root down to n.
func (n *Node) Path() []*Node {
	var rev []*Node
	for p := n; p != nil; p = p.Parent {
		rev = append(rev, p)
	}
	out := make([]*Node, len(rev))
	for i, p := range rev {
		out[len(rev)-1-i] = p
	}
	return out
}

// IsAncestorOf reports whether n is a strict ancestor of m.
func (n *Node) IsAncestorOf(m *Node) bool {
	if m == nil {
		return false
	}
	for p := m.Parent; p != nil; p = p.Parent {
		if p == n {
			return true
		}
	}
	return false
}

// Tree is the research tree of one session. Hold node IDs, not pointers, across
// rebuilds: Build and Merge return new nodes.
type Tree struct {
	Roots []*Node
	// EngineLeaf is the ID of the node holding the engine's current leaf ("" when the
	// session has no prompt yet).
	EngineLeaf string

	byID map[string]*Node
}

// Node returns the node with the given ID, or nil.
func (t *Tree) Node(id string) *Node {
	if t == nil || id == "" {
		return nil
	}
	return t.byID[id]
}

// Leaf returns the node holding the engine's current leaf, or nil.
func (t *Tree) Leaf() *Node {
	if t == nil {
		return nil
	}
	return t.Node(t.EngineLeaf)
}

// Len is the number of nodes, the synthetic root included.
func (t *Tree) Len() int {
	if t == nil {
		return 0
	}
	return len(t.byID)
}

// Walk calls fn for every node in depth-first pre-order, children in order. It stops
// when fn returns false.
func (t *Tree) Walk(fn func(*Node) bool) {
	if t == nil {
		return
	}
	stack := make([]*Node, 0, len(t.Roots))
	for i := len(t.Roots) - 1; i >= 0; i-- {
		stack = append(stack, t.Roots[i])
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !fn(n) {
			return
		}
		for i := len(n.Children) - 1; i >= 0; i-- {
			stack = append(stack, n.Children[i])
		}
	}
}

func (t *Tree) add(n *Node) {
	if t.byID == nil {
		t.byID = map[string]*Node{}
	}
	t.byID[n.ID] = n
	if n.Parent == nil {
		t.Roots = append(t.Roots, n)
	} else {
		n.Parent.Children = append(n.Parent.Children, n)
	}
}

var (
	// Engine-generated user text: a leading XML-ish tag (slash commands, local command
	// output, bash mode, caveats) or an interruption marker.
	syntheticRE = regexp.MustCompile(`^(?:<[a-z][\w-]*[\s>/]|\[Request interrupted by user)`)
	interruptRE = regexp.MustCompile(`^\[Request interrupted by user`)
)

// promptText returns the typed text of a real prompt and ok=false for anything else:
// tool results, meta, compact summaries, sidechains, slash and bash commands, local
// command output and interruption markers.
func promptText(e *sessions.Entry) (string, bool) {
	if e.IsSidechain || !e.IsPrompt() || e.IsVisibleInTranscriptOnly {
		return "", false
	}
	var parts []string
	image := false
	for _, b := range e.Message.Content.Blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, b.Text)
			}
		case "image", "document":
			image = true
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", image
	}
	if syntheticRE.MatchString(text) {
		return "", false
	}
	return text, true
}

// isInterrupt reports whether e is the engine's "[Request interrupted by user]" marker.
func isInterrupt(e *sessions.Entry) bool {
	if e.Kind() != sessions.KindUser || e.Message == nil {
		return false
	}
	for _, b := range e.Message.Content.Blocks {
		if b.Type == "text" && interruptRE.MatchString(strings.TrimSpace(b.Text)) {
			return true
		}
	}
	return false
}

// Build makes the research tree of a transcript. A node starts at every real prompt and
// owns the entries up to the next one; compact boundaries are followed back through
// their logical parent. Chains whose parent is missing from the file hang under a
// synthetic root. Build never returns nil.
func Build(tr *sessions.Transcript) *Tree {
	t := &Tree{byID: map[string]*Node{}}
	if tr == nil || tr.Tree == nil {
		return t
	}
	b := builder{
		t:     t,
		tr:    tr,
		index: make(map[*sessions.Node]int, len(tr.Entries)),
		owner: make(map[*sessions.Node]*Node, len(tr.Entries)),
		first: map[*Node]*sessions.Node{},
		logic: map[*sessions.Node][]*sessions.Node{},
	}
	b.run()
	return t
}

type builder struct {
	t     *Tree
	tr    *sessions.Transcript
	index map[*sessions.Node]int              // file order
	owner map[*sessions.Node]*Node            // research node owning each entry
	first map[*Node]*sessions.Node            // a node's prompt entry
	logic map[*sessions.Node][]*sessions.Node // compact boundaries by logical parent
	order []*sessions.Node                    // entry nodes in file order
	synth *Node
}

func (b *builder) synthetic() *Node {
	if b.synth == nil {
		b.synth = &Node{ID: SyntheticID, Synthetic: true}
		b.t.add(b.synth)
	}
	return b.synth
}

func (b *builder) run() {
	st := b.tr.Tree
	for _, e := range b.tr.Entries {
		n := st.Node(e.UUID)
		if n == nil || n.Entry != e {
			continue
		}
		if _, seen := b.index[n]; seen {
			continue
		}
		b.index[n] = len(b.order)
		b.order = append(b.order, n)
	}

	type start struct {
		n      *sessions.Node
		orphan bool
	}
	var starts []start
	for _, r := range st.Roots {
		if r.Entry.IsSidechain {
			continue
		}
		if lp := r.Entry.LogicalParentUUID; lp != "" && r.Entry.IsCompactBoundary() {
			if p := st.Node(lp); p != nil && p != r {
				b.logic[p] = append(b.logic[p], r)
				continue
			}
			starts = append(starts, start{r, true})
			continue
		}
		p := r.Entry.ParentUUID
		starts = append(starts, start{r, p != "" && p != r.Entry.UUID})
	}

	// Depth-first over parentUuid children plus logical (compaction) children.
	type frame struct {
		n         *sessions.Node
		owner     *Node
		compacted bool
	}
	var stack []frame
	for i := len(starts) - 1; i >= 0; i-- {
		f := frame{n: starts[i].n}
		if starts[i].orphan {
			f.owner = b.synthetic()
			f.compacted = starts[i].n.Entry.IsCompactBoundary()
		}
		stack = append(stack, f)
	}
	visited := map[*sessions.Node]bool{}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[f.n] || f.n.Entry.IsSidechain {
			continue
		}
		visited[f.n] = true
		e := f.n.Entry
		owner, compacted := f.owner, f.compacted
		if text, ok := promptText(e); ok {
			quote, question := SplitQuote(text)
			nd := &Node{ID: e.UUID, Parent: owner, Prompt: question, Quote: quote, Compacted: compacted}
			if b.t.byID[nd.ID] == nil {
				b.t.add(nd)
				b.first[nd] = f.n
				owner, compacted = nd, false
			}
		}
		if owner != nil {
			b.owner[f.n] = owner
		}
		kids := f.n.Children
		for i := len(b.logic[f.n]) - 1; i >= 0; i-- {
			stack = append(stack, frame{n: b.logic[f.n][i], owner: owner, compacted: true})
		}
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, frame{n: kids[i], owner: owner, compacted: compacted})
		}
	}

	b.leaves()
	if leaf := b.tr.ActiveLeaf(); leaf != nil {
		if o := b.owner[leaf]; o != nil {
			b.t.EngineLeaf = o.ID
		}
	}
}

// leaves sets each node's LeafUUID and State from the entries it owns.
func (b *builder) leaves() {
	type span struct {
		leaf        *sessions.Node
		interrupted bool
	}
	spans := map[*Node]*span{}
	for _, n := range b.order {
		o := b.owner[n]
		if o == nil {
			continue
		}
		s := spans[o]
		if s == nil {
			s = &span{}
			spans[o] = s
		}
		if isInterrupt(n.Entry) {
			s.interrupted = true
		}
		if b.sideResult(n, o) {
			continue
		}
		if !b.spanLeaf(n, o) {
			continue
		}
		if s.leaf == nil || b.index[n] > b.index[s.leaf] {
			s.leaf = n
		}
	}
	for o, s := range spans {
		if s.leaf == nil {
			continue
		}
		o.LeafUUID = s.leaf.Entry.UUID
		switch {
		case s.interrupted:
			o.State = Interrupted
		case s.leaf.Entry.IsAPIErrorMessage:
			o.State = Failed
		}
	}
}

// inSpan lists n's parentUuid and logical children owned by o.
func (b *builder) inSpan(n *sessions.Node, o *Node) []*sessions.Node {
	var out []*sessions.Node
	for _, c := range n.Children {
		if b.owner[c] == o && b.first[o] != c {
			out = append(out, c)
		}
	}
	for _, c := range b.logic[n] {
		if b.owner[c] == o {
			out = append(out, c)
		}
	}
	return out
}

// spanLeaf reports whether n ends its node's span: none of its children continue it.
func (b *builder) spanLeaf(n *sessions.Node, o *Node) bool {
	for _, c := range b.inSpan(n, o) {
		if !b.sideResult(c, o) {
			return false
		}
	}
	return true
}

// sideResult reports whether n is a tool result on a parallel-call side branch: a
// childless tool-result entry whose parent continues the span another way.
func (b *builder) sideResult(n *sessions.Node, o *Node) bool {
	if !n.Entry.IsToolResult() || len(b.inSpan(n, o)) > 0 || n.Parent == nil {
		return false
	}
	for _, c := range n.Children {
		if _, ok := promptText(c.Entry); ok {
			return false // a follow-up hangs off it
		}
	}
	for _, sib := range b.inSpan(n.Parent, o) {
		if sib != n && (!sib.Entry.IsToolResult() || len(b.inSpan(sib, o)) > 0) {
			return true
		}
	}
	return false
}
