package status

import (
	"encoding/json"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestFromEngine(t *testing.T) {
	var init proto.InitializeResponse
	if err := json.Unmarshal([]byte(`{"output_style":"default","current_permission_mode":"plan",
		"account":{"email":"user@example.com","subscriptionType":"pro","apiProvider":"firstParty"},
		"fast_mode_state":"off"}`), &init); err != nil {
		t.Fatal(err)
	}
	var sys proto.SystemInit
	if err := json.Unmarshal([]byte(`{"type":"system","subtype":"init","session_id":"s1","cwd":"/w",
		"model":"claude-opus-5-5","permissionMode":"acceptEdits","claude_code_version":"2.1.288",
		"output_style":"Explanatory","tools":["Bash","Read"],"apiKeySource":"none",
		"mcp_servers":[{"name":"gh","status":"connected","source":"user"}]}`), &sys); err != nil {
		t.Fatal(err)
	}
	var in Inputs
	in.FromEngine(&init, &sys)
	if in.Account.Email != "user@example.com" || in.Account.APIKeySource != "none" {
		t.Errorf("account %+v", in.Account)
	}
	if in.PermissionMode != "acceptEdits" || in.OutputStyle != "Explanatory" || in.EngineVersion != "2.1.288" ||
		in.SessionID != "s1" || in.Cwd != "/w" || len(in.Tools) != 2 || in.Model != "claude-opus-5-5" {
		t.Errorf("inputs %+v", in)
	}
	if len(in.MCP) != 1 || in.MCP[0].Scope != "user" {
		t.Errorf("mcp %+v", in.MCP)
	}

	// init alone still works, and nil inputs are ignored.
	var only Inputs
	only.FromEngine(&init, nil)
	if only.PermissionMode != "plan" {
		t.Errorf("init only: %+v", only)
	}
	only.FromEngine(nil, nil)
}

func TestMCPFromStatus(t *testing.T) {
	r := proto.MCPStatusResponse{MCPServers: []proto.MCPServerStatus{
		{Name: "a", Status: "failed", Error: "boom", Source: "project"},
		{Name: "b", Status: "connected", Scope: "user"},
	}}
	got := MCPFromStatus(r)
	if len(got) != 2 || got[0].Error != "boom" || got[0].Scope != "project" || got[1].Scope != "user" {
		t.Errorf("got %+v", got)
	}
}
