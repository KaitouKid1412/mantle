package enginefake

import "encoding/json"

// Version is the engine version the fake reports (get_binary_version, InitEvent).
const Version = "2.1.288"

// DefaultInitializeResponse is a minimal initialize reply (no real account data).
var DefaultInitializeResponse = json.RawMessage(`{"commands":[{"name":"compact","description":"Compact the conversation","argumentHint":"<instructions>"},{"name":"context","description":"Show context usage","argumentHint":""}],"agents":[{"name":"general-purpose","description":"General agent"}],"output_style":"default","available_output_styles":["default"],"models":[{"value":"default","displayName":"Default","description":"Default model"}],"account":{"apiProvider":"firstParty"},"pid":4242,"current_permission_mode":"default","session_state":"idle","capabilities":["ui_surface_v1"]}`)

// InitializeRule answers every initialize request with resp (nil means
// DefaultInitializeResponse). Most scripts want this first.
func InitializeRule(resp any) Step {
	if resp == nil {
		resp = DefaultInitializeResponse
	}
	return On(json.RawMessage(`{"type":"control_request","request":{"subtype":"initialize"}}`), resp)
}

// InitEvent returns a system/init line for session sid.
func InitEvent(sid string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"type": "system", "subtype": "init", "session_id": sid, "uuid": "init-" + sid,
		"cwd": "/home/user/project", "tools": []string{"Bash", "Edit", "Read"}, "mcp_servers": []any{},
		"model": "claude-test", "permissionMode": "default", "slash_commands": []string{"compact", "context"},
		"apiKeySource": "none", "claude_code_version": "2.1.288", "output_style": "default",
		"capabilities": []string{"interrupt_receipt_v1", "msg_lifecycle_v1"},
	})
	return b
}
