// Package sessions reads Claude Code session transcripts (JSONL) and indexes them.
//
// It never writes a transcript: only the engine does. Everything here is read-only and
// tolerant of files that are being appended to while they are read.
//
//   - paths.go: config dir, project slugs, session and subagent file locations.
//   - reader.go, record.go, content.go: a streaming JSONL decoder that keeps raw JSON.
//   - tree.go, transcript.go: the parentUuid tree and the active branch.
//   - lite.go, index.go: per-session metadata from file heads and tails, with a cache.
//   - subagents.go: subagent transcripts nested under their Agent tool call.
//   - stats.go: cost and token totals for /usage and /stats fallbacks.
//
// Primary owner: plan 06 (docs/plans/06-sessions-context.md).
package sessions
