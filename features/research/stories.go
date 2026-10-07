package research

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/research"
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// fakeNode describes one question of a made-up session: its ID, its parent's ID (""
// for a root) and the prompt as typed (a leading "> " block is a quote). running
// leaves the answer out, so the node has no leaf yet.
type fakeNode struct {
	id, parent, prompt string
	running            bool
}

// fakeTranscript writes a session JSONL with one prompt and one answer per node, in
// the given order (the last node is the engine's tip), and parses it. Stories and
// tests build trees this way, through research.Build, as the real UI does.
func fakeTranscript(nodes []fakeNode) *sessions.Transcript {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	var b strings.Builder
	line := func(m map[string]any) {
		at = at.Add(time.Second)
		m["sessionId"] = "story-session"
		m["timestamp"] = at.Format(time.RFC3339)
		m["isSidechain"] = false
		raw, _ := json.Marshal(m)
		b.Write(raw)
		b.WriteByte('\n')
	}
	for _, n := range nodes {
		var parent any
		if n.parent != "" {
			parent = n.parent + "-a"
		}
		line(map[string]any{"type": "user", "uuid": n.id, "parentUuid": parent,
			"message": map[string]any{"role": "user", "content": n.prompt}})
		if !n.running {
			line(map[string]any{"type": "assistant", "uuid": n.id + "-a", "parentUuid": n.id,
				"message": map[string]any{"role": "assistant", "content": []any{
					map[string]any{"type": "text", "text": "Answer to " + n.id},
				}}})
		}
	}
	t, _ := sessions.Parse(strings.NewReader(b.String()))
	return t
}

// fakeTree builds the research tree of fakeTranscript(nodes).
// Running nodes are marked the way the live tree marks a prompt just sent.
func fakeTree(nodes []fakeNode) *research.Tree {
	t := research.Build(fakeTranscript(nodes))
	for _, n := range nodes {
		if nd := t.Node(n.id); nd != nil && n.running {
			nd.State, nd.LeafUUID = research.Running, ""
		}
	}
	return t
}

// deepSession is a straight line of ten questions with a second branch at q2.
var deepSession = []fakeNode{
	{id: "q1", prompt: "How does the scheduler pick the next goroutine?"},
	{id: "q2", parent: "q1", prompt: "What is the run queue?"},
	{id: "q2b", parent: "q1", prompt: "And the network poller?"},
	{id: "q3", parent: "q2", prompt: "Why is the local run queue 256 entries?"},
	{id: "q4", parent: "q3", prompt: "What happens when it overflows?"},
	{id: "q5", parent: "q4", prompt: "How does work stealing choose a victim?"},
	{id: "q6", parent: "q5", prompt: "Is stealing randomized?"},
	{id: "q7", parent: "q6", prompt: "Show me the code for runqsteal"},
	{id: "q8", parent: "q7", prompt: "What does the atomic load-acquire guard against here?"},
	{id: "q9", parent: "q8", prompt: "Could a relaxed load work on arm64?"},
}

// branchySession has a node with many follow-ups: quoted, running, and the tip.
var branchySession = []fakeNode{
	{id: "r1", prompt: "Explain the trade-offs of B-trees versus LSM trees for a write-heavy workload."},
	{id: "c1", parent: "r1", prompt: "What is write amplification?"},
	{id: "c2", parent: "r1", prompt: "> LSM trees batch writes in memory\n\nHow big is the memtable usually?"},
	{id: "c3", parent: "r1", prompt: "Which one compresses better?"},
	{id: "c4", parent: "r1", prompt: "How do range scans compare?"},
	{id: "c5", parent: "r1", prompt: "What about read amplification and bloom filters, and how do they interact with compaction strategies like leveled and tiered?"},
	{id: "c6", parent: "r1", prompt: "Give me a benchmark plan"},
	{id: "c7", parent: "r1", prompt: "Summarize in one table", running: true},
}

// storyFeature returns an active feature showing node viewing of nodes.
func storyFeature(ctx ext.Ctx, nodes []fakeNode, viewing string) *feature {
	f := newFeature()
	f.tree = fakeTree(nodes)
	f.active = true
	f.navigate(ctx, viewing)
	return f
}

// storyScreen draws both bars around a stand-in for the viewed node.
func storyScreen(ctx ext.Ctx, f *feature, a ext.Area) ext.Rendered {
	t := ctx.Theme()
	anc := (&bar{f: f, id: AncestorsID}).View(ctx, a).Text
	kids := (&bar{f: f, id: ChildrenID}).View(ctx, a).Text
	q := ansi.Truncate(nodeLabel(f.node()), max(1, a.Width-2), "…")
	body := t.Paint(theme.Inactive, "❯ ") + t.Paint(theme.Text, q) + "\n" +
		t.Paint(theme.Inactive, "  ⋮ answer")
	var parts []string
	for _, p := range []string{anc, body, kids} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return ext.Rendered{Text: strings.Join(parts, "\n")}
}

func stories() []ext.Story {
	screen := func(nodes []fakeNode, viewing string) func(ext.Ctx, ext.Area) ext.Rendered {
		return func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			return storyScreen(ctx, storyFeature(ctx, nodes, viewing), a)
		}
	}
	return []ext.Story{
		{ID: "research.bars/deep", Render: screen(deepSession, "q9")},
		{ID: "research.bars/middle", Render: screen(deepSession, "q1")},
		{ID: "research.bars/branches", Render: screen(branchySession, "r1")},
		{ID: "research.bars/leaf", Render: screen(branchySession, "c2")},
		{ID: "research.tree/picker", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			f := storyFeature(ctx, branchySession, "c2")
			d, _ := f.openPicker(ctx, nil)
			return d.View(ctx, ext.Area{Width: a.Width, MaxHeight: 16, Mode: a.Mode})
		}},
	}
}
