package research

// PromptSent adds a Running node for a prompt just sent to the engine, as a child of
// the node with ID parent ("" or unknown: a new root), and makes it the engine leaf. It
// returns the node; a known uuid returns the existing node unchanged.
func (t *Tree) PromptSent(uuid, parent, text string) *Node {
	if n := t.Node(uuid); n != nil {
		return n
	}
	quote, question := SplitQuote(text)
	n := &Node{ID: uuid, Parent: t.Node(parent), Prompt: question, Quote: quote, State: Running}
	t.add(n)
	t.EngineLeaf = uuid
	return n
}

// TurnEnded finishes the engine leaf's turn: lastUUID is the turn's last entry and state
// how it ended. It does nothing unless the leaf is Running.
func (t *Tree) TurnEnded(lastUUID string, state NodeState) {
	n := t.Leaf()
	if n == nil || n.State != Running {
		return
	}
	n.State = state
	if lastUUID != "" {
		n.LeafUUID = lastUUID
	}
}

// SetEngineLeaf records that the engine now continues from the node with ID id (after
// a rewind or a resume-at). Unknown IDs are ignored.
func (t *Tree) SetEngineLeaf(id string) {
	if t.Node(id) != nil {
		t.EngineLeaf = id
	}
}

// Merge combines live state with fresh, a tree just rebuilt from the session JSONL, and
// returns the result (built on fresh's nodes). The JSONL is authoritative, except that:
//   - nodes known only live (the engine has not written them yet) are kept, under the
//     same parent;
//   - nodes still Running live stay Running with no leaf;
//   - the live engine leaf is kept while it exists (the JSONL's last-prompt record
//     lags).
func (t *Tree) Merge(fresh *Tree) *Tree {
	if fresh == nil {
		fresh = &Tree{}
	}
	if fresh.byID == nil {
		fresh.byID = map[string]*Node{}
	}
	t.Walk(func(n *Node) bool {
		f := fresh.byID[n.ID]
		switch {
		case f == nil:
			pid := ""
			if n.Parent != nil {
				pid = n.Parent.ID
			}
			c := &Node{ID: n.ID, Parent: fresh.byID[pid], Prompt: n.Prompt, Quote: n.Quote,
				LeafUUID: n.LeafUUID, State: n.State, Compacted: n.Compacted, Synthetic: n.Synthetic}
			fresh.add(c)
		case n.State == Running:
			f.State, f.LeafUUID = Running, ""
		}
		return true
	})
	if t != nil && fresh.byID[t.EngineLeaf] != nil {
		fresh.EngineLeaf = t.EngineLeaf
	}
	return fresh
}
