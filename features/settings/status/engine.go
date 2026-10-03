package status

import "github.com/KaitouKid1412/mantle/pkg/proto"

// FromEngine fills the engine-reported fields from the initialize response and the
// newest system/init. Either may be nil; system/init wins where both report a value,
// since it is refreshed every turn.
func (in *Inputs) FromEngine(init *proto.InitializeResponse, sys *proto.SystemInit) {
	if init != nil {
		in.Account = init.Account
		in.PermissionMode = init.CurrentPermissionMode
		in.OutputStyle = init.OutputStyle
		in.FastModeState = init.FastModeState
		in.FastModeDisabledReason = init.FastModeDisabledReason
	}
	if sys == nil {
		return
	}
	in.EngineVersion = firstNonEmpty(sys.ClaudeCodeVersion, in.EngineVersion)
	in.Model = firstNonEmpty(sys.Model, in.Model)
	in.SessionID = firstNonEmpty(sys.SessionID, in.SessionID)
	in.Cwd = firstNonEmpty(sys.CWD, in.Cwd)
	in.PermissionMode = firstNonEmpty(sys.PermissionMode, in.PermissionMode)
	in.OutputStyle = firstNonEmpty(sys.OutputStyle, in.OutputStyle)
	in.FastModeState = firstNonEmpty(sys.FastModeState, in.FastModeState)
	in.FastModeDisabledReason = firstNonEmpty(sys.FastModeDisabledReason, in.FastModeDisabledReason)
	if in.Account.APIKeySource == "" {
		in.Account.APIKeySource = sys.APIKeySource
	}
	in.Tools = sys.Tools
	if len(in.MCP) == 0 {
		for _, m := range sys.MCPServers {
			in.MCP = append(in.MCP, MCPServer{Name: m.Name, Status: m.Status, Scope: m.Source})
		}
	}
}

// MCPFromStatus converts an mcp_status reply, which is fresher and more detailed than
// system/init's list.
func MCPFromStatus(r proto.MCPStatusResponse) []MCPServer {
	out := make([]MCPServer, 0, len(r.MCPServers))
	for _, m := range r.MCPServers {
		out = append(out, MCPServer{Name: m.Name, Status: m.Status, Scope: firstNonEmpty(m.Scope, m.Source), Error: m.Error})
	}
	return out
}
