package research

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// A scope is the ext.Transcript the fullscreen viewport shows while research mode is
// on: the items of one node, from its prompt up to the next top-level prompt.
//
// On the engine's current branch the node's items are in the live store, so the scope
// slices the store on every call and streaming shows. Off the branch they are not: the
// node's branch is normalized from the session JSONL (ending at its LeafUUID), off the
// UI goroutine, and cached per node and leaf.

// Loader normalizes the session's branch ending at JSONL entry leaf into items, the
// way session history is loaded (Normalize with Leaf). It runs in a Cmd.
type Loader func(leaf string) ([]*ext.Item, error)

// promptItemID is the transcript item ID of a node's prompt.
func promptItemID(nodeID string) string { return "user:" + nodeID }

// sliceNode returns the items of node nodeID: its top-level prompt item and everything
// up to (not including) the next top-level prompt. nil when the prompt is not there.
func sliceNode(items []*ext.Item, nodeID string) []*ext.Item {
	id := promptItemID(nodeID)
	start := -1
	for i, it := range items {
		if it != nil && it.ID == id && it.ParentID == "" {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := len(items)
	for i := start + 1; i < len(items); i++ {
		if isTopPrompt(items[i]) {
			end = i
			break
		}
	}
	return items[start:end:end]
}

// isTopPrompt reports whether it starts a new node: a top-level user prompt.
func isTopPrompt(it *ext.Item) bool {
	return it != nil && it.ParentID == "" && it.Key == ext.KeyUserPrompt && strings.HasPrefix(it.ID, "user:")
}

// liveScope is a node on the engine's current branch, sliced from the live store.
type liveScope struct {
	src    ext.Transcript
	nodeID string
}

func (s liveScope) Items() []*ext.Item {
	if s.src == nil {
		return nil
	}
	return sliceNode(s.src.Items(), s.nodeID)
}

func (s liveScope) Get(id string) *ext.Item {
	if s.src == nil {
		return nil
	}
	return s.src.Get(id)
}

func (s liveScope) Committed() int { return 0 }

// staticScope is a node off the current branch: fixed items from the JSONL.
type staticScope struct {
	items []*ext.Item
	byID  map[string]*ext.Item
}

func newStaticScope(items []*ext.Item) *staticScope {
	s := &staticScope{items: items, byID: make(map[string]*ext.Item, len(items))}
	for _, it := range items {
		if it != nil {
			s.byID[it.ID] = it
		}
	}
	return s
}

func (s *staticScope) Items() []*ext.Item      { return s.items }
func (s *staticScope) Get(id string) *ext.Item { return s.byID[id] }
func (s *staticScope) Committed() int          { return 0 }

// scopeKey caches an off-branch node's items: the same node can end at a newer leaf
// after the JSONL is merged again.
type scopeKey struct{ node, leaf string }

// scopeLoadedMsg carries an off-branch node's items, loaded by a Loader.
type scopeLoadedMsg struct {
	key   scopeKey
	items []*ext.Item
	err   error
}

// scopeCache holds the loaded off-branch scopes and the loads in flight.
type scopeCache struct {
	done    map[scopeKey]*staticScope
	loading map[scopeKey]bool
}

func newScopeCache() *scopeCache {
	return &scopeCache{done: map[scopeKey]*staticScope{}, loading: map[scopeKey]bool{}}
}

// get returns the cached scope for k, or starts a load (nil scope, load Cmd). A nil
// loader or an empty leaf yields an empty scope.
func (c *scopeCache) get(k scopeKey, load Loader) (*staticScope, tea.Cmd) {
	if s, ok := c.done[k]; ok {
		return s, nil
	}
	if load == nil || k.leaf == "" {
		s := newStaticScope(nil)
		c.done[k] = s
		return s, nil
	}
	if c.loading[k] {
		return nil, nil
	}
	c.loading[k] = true
	return nil, func() tea.Msg {
		items, err := load(k.leaf)
		return scopeLoadedMsg{key: k, items: items, err: err}
	}
}

// loaded stores a finished load, sliced to the node.
func (c *scopeCache) loaded(m scopeLoadedMsg) *staticScope {
	delete(c.loading, m.key)
	s := newStaticScope(sliceNode(m.items, m.key.node))
	c.done[m.key] = s
	return s
}

// failed forgets a load that failed, so the next visit tries again.
func (c *scopeCache) failed(k scopeKey) { delete(c.loading, k) }

// reset forgets every cached scope (session switch).
func (c *scopeCache) reset() {
	clear(c.done)
	clear(c.loading)
}
