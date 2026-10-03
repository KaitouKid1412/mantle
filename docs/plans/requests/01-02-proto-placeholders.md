# 01 → 02: proto placeholders

**Status:** resolved 2026-10-03. Not needed in the end: `proto-v1` landed before
`contracts-v1`, so plan 01 merged it and no `pkg/proto/placeholder.go` was ever committed.

`pkg/ext` (contracts-v1) uses these `pkg/proto` names, agreed with session 02:

| Name | Used in `pkg/ext` as |
|---|---|
| `Event` (`interface{ Env() *Envelope }`) | `EngineEventMsg.Event` |
| `ContentBlock` (flat struct union) | `Prompt.Blocks []proto.ContentBlock` |
| `ToolResult` | `Item.Result *proto.ToolResult` |
| `CanUseTool` | `PermissionMsg.Req` |
| `PermissionResult` | `PermissionMsg.Reply func(proto.PermissionResult) tea.Cmd` |

`Item.Data` is `any`: assistant blocks are `*proto.ContentBlock`, tool calls
`*proto.ToolUse`, system events their concrete `Event` types (e.g. `*proto.CompactBoundary`).

`ext.SpawnOpts` carries the fields plan 02's process builder asked for: Cwd, Model,
PermissionMode, Resume, Continue, ForkSession, ResumeSessionAt, ResumeDropsTurn,
SessionID, Name, AddDirs, Settings, ExtraArgs, Env, SafeMode, Stop.
