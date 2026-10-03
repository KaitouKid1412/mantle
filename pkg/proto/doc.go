// Package proto holds the Claude Code stream-json protocol types (stdin, stdout and control messages).
//
// Reading: Decode turns one stdout line into an Event. Switch on the concrete type
// (*Assistant, *StreamEvent, *User, *Result, *SystemInit and the other system subtypes,
// *ControlRequest, *ControlResponse, ...). Unknown types become *Unknown and are never
// dropped; every event keeps its line in Env().Raw.
//
// Writing: NewUserInput(...).MarshalLine for prompts, MarshalControlRequest for client
// requests (each request type implements Request), MarshalControlSuccess/Error for
// answers to CLI requests such as *CanUseTool.
//
// Undocumented control subtypes are not here; they live in internal/engine/unstable.go.
//
// Additive-only after the proto-v1 tag: no renames, removals or signature changes.
//
// Primary owner: plan 02 (docs/plans/02-proto-engine.md).
package proto
