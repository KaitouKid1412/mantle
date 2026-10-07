package research

// SendKind says how a follow-up to the viewed node reaches the engine.
type SendKind int

const (
	// Plain: the viewed node is the engine's leaf; send as usual.
	Plain SendKind = iota
	// Rewind: the viewed node is an ancestor of the engine's leaf; rewind the
	// conversation to drop DropPrompt (and all after it), then send.
	Rewind
	// Resume: restart the engine with --resume-session-at=At, then send.
	Resume
	// Blocked: the follow-up cannot be sent now; Reason says why.
	Blocked
)

func (k SendKind) String() string {
	switch k {
	case Plain:
		return "plain"
	case Rewind:
		return "rewind"
	case Resume:
		return "resume"
	case Blocked:
		return "blocked"
	}
	return "unknown"
}

// SendPlan is how to send a follow-up to the viewed node.
type SendPlan struct {
	Kind SendKind
	// At is the entry uuid the new prompt hangs off (the viewed node's LeafUUID). It is
	// "" for Plain and Blocked.
	At string
	// DropPrompt is the ID of the viewed node's child on the engine's current path
	// (Rewind only).
	DropPrompt string
	// Reason explains a Blocked plan.
	Reason string
	// NeedsConfirm is set when branching undoes a compaction: the engine reloads the
	// full context from before the compact boundary.
	NeedsConfirm bool
}

// Reasons for Blocked plans.
const (
	ReasonUnknown = "the viewed question is no longer in the session"
	ReasonRunning = "the viewed answer is still running"
	ReasonBusy    = "Claude is answering another question"
	ReasonNoLeaf  = "the viewed question has no answer to continue from"
)

// Plan decides how a follow-up typed while viewing the node with ID viewing is sent.
// busy reports whether the engine is running a turn. An empty viewing means the tip.
func Plan(t *Tree, viewing string, busy bool) SendPlan {
	if viewing == "" || (t != nil && viewing == t.EngineLeaf) {
		if v := t.Node(viewing); v != nil && v.State == Running {
			return SendPlan{Kind: Blocked, Reason: ReasonRunning}
		}
		return SendPlan{Kind: Plain}
	}
	v := t.Node(viewing)
	switch {
	case v == nil:
		return SendPlan{Kind: Blocked, Reason: ReasonUnknown}
	case v.State == Running:
		return SendPlan{Kind: Blocked, Reason: ReasonRunning}
	case busy:
		return SendPlan{Kind: Blocked, Reason: ReasonBusy}
	case v.LeafUUID == "":
		return SendPlan{Kind: Blocked, Reason: ReasonNoLeaf}
	}
	leaf := t.Leaf()
	p := SendPlan{Kind: Resume, At: v.LeafUUID, NeedsConfirm: undoesCompaction(v, leaf)}
	if leaf != nil && v.IsAncestorOf(leaf) {
		p.Kind = Rewind
		for c := leaf; c != nil; c = c.Parent {
			if c.Parent == v {
				p.DropPrompt = c.ID
				break
			}
		}
	}
	return p
}

// undoesCompaction reports whether branching at v drops a compaction the engine's
// current path relies on. A Compacted node's boundary sits in its parent's span, before
// the parent's LeafUUID, so branching at that parent (or below it, past the same
// boundary) keeps the compacted context; branching above it does not.
func undoesCompaction(v, leaf *Node) bool {
	if leaf == nil {
		return false
	}
	// onV maps each node on v's path to its child on that path (nil for v).
	onV := map[*Node]*Node{v: nil}
	for c := v; c.Parent != nil; c = c.Parent {
		onV[c.Parent] = c
	}
	for c := leaf; c != nil; c = c.Parent {
		if _, ok := onV[c]; ok {
			break
		}
		if !c.Compacted || c.Parent == v {
			continue
		}
		if d, ok := onV[c.Parent]; ok && d != nil && d.Compacted {
			continue // v's own path crosses the same boundary
		}
		return true
	}
	return false
}
