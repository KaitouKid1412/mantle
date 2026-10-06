// Command fakeclaude is a scripted stand-in for the claude binary, for tests.
//
// Put it on PATH as "claude" (or point mantle's engine path at it) and set:
//
//	FAKECLAUDE_SCRIPT   path of an enginefake JSONL script (required)
//	FAKECLAUDE_LOG      optional; written at exit as JSON: argv, selected env, stdin lines
//	FAKECLAUDE_VERSION  version printed by --version (default 2.1.288)
//	FAKECLAUDE_NO_SESSIONS  optional; --resume/--continue fail like a headless claude whose
//	                    session never had a turn ("No conversation found …", exit 1)
//
// Exit codes: the script's own, or 3 when the script fails (see stderr).
//
// Primary owner: plan 02.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/KaitouKid1412/mantle/internal/testkit/enginefake"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "record" {
		os.Exit(record(os.Args[2:]))
	}
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			v := os.Getenv("FAKECLAUDE_VERSION")
			if v == "" {
				v = "2.1.288"
			}
			fmt.Printf("%s (Claude Code)\n", v)
			return
		}
	}
	if os.Getenv("FAKECLAUDE_NO_SESSIONS") != "" {
		// Like a headless claude whose sessions never had a turn: nothing to resume.
		if msg := noConversation(os.Args[1:]); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(1)
		}
	}
	path := os.Getenv("FAKECLAUDE_SCRIPT")
	if path == "" {
		fmt.Fprintln(os.Stderr, "fakeclaude: FAKECLAUDE_SCRIPT is not set")
		os.Exit(2)
	}
	s, err := enginefake.ParseFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	rec := &recordingReader{}
	code, _ := s.Run(ctx, rec.wrap(os.Stdin), os.Stdout, os.Stderr)
	if logPath := os.Getenv("FAKECLAUDE_LOG"); logPath != "" {
		writeLog(logPath, rec.lines())
	}
	os.Exit(code)
}

// noConversation returns the engine's error for a resume of a session without a
// transcript, or "" when args don't resume.
func noConversation(args []string) string {
	for i, a := range args {
		switch {
		case strings.HasPrefix(a, "--resume="):
			return "No conversation found with session ID: " + strings.TrimPrefix(a, "--resume=")
		case (a == "--resume" || a == "-r") && i+1 < len(args):
			return "No conversation found with session ID: " + args[i+1]
		case a == "--continue" || a == "-c":
			return "No conversation found to continue"
		}
	}
	return ""
}

func writeLog(path string, stdin []string) {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "CLAUDE") || k == "NODE_OPTIONS" || k == "DEBUG" || k == "ANTHROPIC_BASE_URL" {
			env[k] = v
		}
	}
	cwd, _ := os.Getwd()
	b, _ := json.MarshalIndent(map[string]any{
		"argv":  os.Args[1:],
		"env":   env,
		"cwd":   cwd,
		"stdin": stdin,
	}, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
}
