// Package fakeapi is a scripted mock of the Anthropic Messages API, so the real
// claude binary can run end-to-end tests offline, deterministically and for free.
//
// In a Go test:
//
//	srv := httptest.NewServer(fakeapi.New(&fakeapi.Script{Turns: []fakeapi.Turn{
//		fakeapi.TextTurn("Hello from fakeapi."),
//	}}))
//	defer srv.Close()
//	cfg := t.TempDir()
//	fakeapi.SeedConfig(cfg, fakeapi.FakeAPIKey, workDir) // onboarding done, key approved, workDir trusted
//	cmd := exec.Command("claude", "--output-format", "stream-json", ...)
//	cmd.Env = fakeapi.Env(os.Environ(), srv.URL, cfg, fakeapi.FakeAPIKey)
//
// Env removes ANTHROPIC_*, CLAUDE_* and similar variables from the parent environment
// and sets ANTHROPIC_BASE_URL, ANTHROPIC_API_KEY, CLAUDE_CONFIG_DIR and the switches
// that turn off non-essential traffic. Never point the config dir at the real
// ~/.claude.
//
// Script turns are consumed in order by main-conversation requests (requests that
// carry tools). Side calls the engine makes (session titles, prompt suggestions,
// helper calls) carry no tools; they get SideReply and consume nothing. Turns can
// match on the latest user message (Contains, Regex, ToolResult), reply with text,
// thinking and tool_use blocks, override the stop reason, fail with an HTTP error,
// add headers (rate-limit headers) and slow the stream down (interrupt tests).
//
// Streaming follows the Messages API event order (message_start, ping,
// content_block_start/delta/stop, message_delta, message_stop); text arrives in
// several deltas and tool input in several input_json_delta chunks. Every request is
// recorded (Server.Requests) with secrets redacted.
//
// cmd/fakeapi serves a script file on a port for manual runs.
//
// Primary owner: plan 02.
package fakeapi
