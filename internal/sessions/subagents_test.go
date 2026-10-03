package sessions

import (
	"path/filepath"
	"testing"
)

func TestSubagents(t *testing.T) {
	projectDir := filepath.Join(fixtureConfig(), "projects", "-work-demo")
	dir := SubagentsDir(projectDir, sidTools)
	subs, err := ListSubagents(dir)
	if err != nil || len(subs) != 2 {
		t.Fatalf("subagents = %v, %v", subs, err)
	}
	a := subs[0]
	if a.AgentID != "a1b2c3d4" || !a.HasMeta || a.Meta.ToolUseID != "toolu_agent1" ||
		a.Meta.AgentType != "general-purpose" || a.Meta.SpawnDepth != 1 || a.Meta.Description != "Summarize files" {
		t.Fatalf("agent = %+v", a)
	}
	if subs[1].AgentID != "e5f6" || subs[1].HasMeta {
		t.Fatalf("meta-less agent = %+v", subs[1])
	}

	byTool, err := SubagentsByToolUse(dir)
	if err != nil || len(byTool) != 1 || byTool["toolu_agent1"].AgentID != "a1b2c3d4" {
		t.Fatalf("by tool use = %v, %v", byTool, err)
	}

	// The subagent's own chain is all sidechain entries; Main keeps them only when asked.
	tr, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(tr.Main(BranchOptions{})); got != 0 {
		t.Fatalf("main without sidechains = %d", got)
	}
	leaf := tr.Tree.Leaves(true)
	if len(leaf) != 1 {
		t.Fatalf("leaves = %d", len(leaf))
	}
	chain := tr.Tree.Branch(leaf[0], BranchOptions{Sidechains: true})
	if len(chain) != 4 || chain[0].AgentID != "a1b2c3d4" || chain[3].Message.Content.Text() != "Both files define main." {
		t.Fatalf("chain = %v", entryUUIDs(chain))
	}

	if subs, err := ListSubagents(SubagentsDir(projectDir, sidPlain)); err != nil || subs != nil {
		t.Fatalf("missing dir = %v, %v", subs, err)
	}
	if got := ToolResultsDir(projectDir, sidTools); !isDir(got) {
		t.Fatalf("tool results dir %q missing", got)
	}
}
