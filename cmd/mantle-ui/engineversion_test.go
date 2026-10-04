package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEngineVersionHint(t *testing.T) {
	state := filepath.Join(t.TempDir(), "engines.json")

	// Native installer: claude -> .../versions/2.1.289
	dir := t.TempDir()
	versioned := filepath.Join(dir, "versions", "2.1.289")
	os.MkdirAll(filepath.Dir(versioned), 0o755)
	os.WriteFile(versioned, []byte("#!/bin/sh\n"), 0o755)
	link := filepath.Join(dir, "claude")
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MANTLE_CLAUDE_BIN", link)
	if v := engineVersionHint(state); v != "2.1.289" {
		t.Fatalf("native install: %q", v)
	}

	// npm install: bin -> node_modules/@anthropic-ai/claude-code/cli.js
	pkgDir := filepath.Join(t.TempDir(), "node_modules", "@anthropic-ai", "claude-code")
	os.MkdirAll(pkgDir, 0o755)
	os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(`{"name":"@anthropic-ai/claude-code","version":"2.1.288"}`), 0o644)
	cli := filepath.Join(pkgDir, "cli.js")
	os.WriteFile(cli, []byte("#!/usr/bin/env node\n"), 0o755)
	t.Setenv("MANTLE_CLAUDE_BIN", cli)
	if v := engineVersionHint(state); v != "2.1.288" {
		t.Fatalf("npm install: %q", v)
	}

	// Unknown: a binary with no version in its path.
	plain := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("MANTLE_CLAUDE_BIN", plain)
	if v := engineVersionHint(state); v != "" {
		t.Fatalf("unknown should be empty: %q", v)
	}
}
