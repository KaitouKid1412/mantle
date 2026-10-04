package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsolatedEnv(t *testing.T) {
	env := IsolatedEnv([]string{"PATH=/bin", "ANTHROPIC_API_KEY=real", "CLAUDE_CONFIG_DIR=/home/x/.claude",
		"CLAUDE_CODE_OAUTH_TOKEN=t", "CLAUDE_CODE_USE_VERTEX=1", "HOME=/h"}, "/tmp/cfg", "http://127.0.0.1:1")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "real") || strings.Contains(joined, "/home/x") || strings.Contains(joined, "OAUTH") || strings.Contains(joined, "VERTEX") {
		t.Errorf("leaked: %s", joined)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "CLAUDE_CONFIG_DIR=/tmp/cfg") ||
		!strings.Contains(joined, "ANTHROPIC_BASE_URL=http://127.0.0.1:1") {
		t.Errorf("env %s", joined)
	}
}

func TestParseHelpTableArity(t *testing.T) {
	flags, cmds := ParseHelp("Usage: claude [options]\n\nOptions:\n  -r, --resume [value]  r\n  --tools <tools...>  t\n  --x [a...]  x\n  --model <m>  m\n  --bare  b\n\nCommands:\n  stop|kill <id>  s\n")
	want := map[string]Arity{"--resume": ArityOptional, "--tools": ArityVariadic, "--x": ArityOptional, "--model": ArityRequired, "--bare": ArityNone}
	for _, f := range flags {
		if f.TableArity() != want[f.Long] {
			t.Errorf("%s: %v", f.Long, f.TableArity())
		}
	}
	if len(flags) != 5 || flags[0].Short != "-r" || len(cmds) != 1 || cmds[0].Aliases[0] != "kill" {
		t.Errorf("flags %+v cmds %+v", flags, cmds)
	}
}

func TestClaudeHelpSafety(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	log := filepath.Join(dir, "calls")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\necho \"Usage: claude $1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := ClaudeHelp(ctx, bin, "mcp", time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"remote-control", "rc", "kill", "fix the bug"} {
		if _, err := ClaudeHelp(ctx, bin, bad, time.Minute); err == nil {
			t.Errorf("%q was run", bad)
		}
	}
	calls, _ := os.ReadFile(log)
	if string(calls) != "mcp --help\n" {
		t.Errorf("calls %q", calls)
	}
}
