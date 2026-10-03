package discovery

import (
	"reflect"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	doc := `---
name: code-reviewer
description: Reviews code: finds bugs, suggests fixes
tools: Read, Grep, Glob
model: sonnet
color: "blue"
argument-hint: '[file] [focus]'
disable-model-invocation: true
user-invocable: false
# a comment
allowed-tools:
  - Bash(git diff:*)
  - Read
paths: [src/**, "docs/*.md"]
when_to_use: |
  Use after a change.

  Twice.
summary: >-
  folded
  text
wrapped: first part
  second part
hooks:
  PreToolUse:
    - matcher: Bash
      hooks:
        - type: command
          command: ./check.sh
metadata:
  owner: team-a
  empty:
---
Body line 1
Body line 2`
	fm, err := ParseFrontmatter(doc)
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"name":          "code-reviewer",
		"description":   "Reviews code: finds bugs, suggests fixes",
		"model":         "sonnet",
		"color":         "blue",
		"argument-hint": "[file] [focus]",
		"when_to_use":   "Use after a change.\n\nTwice.\n",
		"summary":       "folded text",
		"wrapped":       "first part second part",
	}
	for k, want := range checks {
		if got := fm.String(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := fm.List("tools"); !reflect.DeepEqual(got, []string{"Read", "Grep", "Glob"}) {
		t.Errorf("tools = %q", got)
	}
	if got := fm.List("allowed-tools"); !reflect.DeepEqual(got, []string{"Bash(git diff:*)", "Read"}) {
		t.Errorf("allowed-tools = %q", got)
	}
	if got := fm.List("paths"); !reflect.DeepEqual(got, []string{"src/**", "docs/*.md"}) {
		t.Errorf("paths = %q", got)
	}
	if v, ok := fm.Bool("disable-model-invocation"); !ok || !v {
		t.Errorf("disable-model-invocation = %v %v", v, ok)
	}
	if v, ok := fm.Bool("user-invocable"); !ok || v {
		t.Errorf("user-invocable = %v %v", v, ok)
	}
	if _, ok := fm.Bool("missing"); ok {
		t.Error("missing bool reported ok")
	}
	hooks, _ := fm.Fields["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("hooks = %#v", fm.Fields["hooks"])
	}
	entry := pre[0].(map[string]any)
	if entry["matcher"] != "Bash" {
		t.Errorf("matcher = %#v", entry["matcher"])
	}
	inner := entry["hooks"].([]any)[0].(map[string]any)
	if inner["type"] != "command" || inner["command"] != "./check.sh" {
		t.Errorf("inner hook = %#v", inner)
	}
	meta := fm.Fields["metadata"].(map[string]any)
	if meta["owner"] != "team-a" || meta["empty"] != "" {
		t.Errorf("metadata = %#v", meta)
	}
	if fm.Body != "Body line 1\nBody line 2" {
		t.Errorf("body = %q", fm.Body)
	}
	if fm.Keys[0] != "name" || fm.Keys[len(fm.Keys)-1] != "metadata" {
		t.Errorf("keys = %q", fm.Keys)
	}
}

func TestParseFrontmatterEdgeCases(t *testing.T) {
	fm, err := ParseFrontmatter("# Just markdown\n")
	if err != nil || len(fm.Fields) != 0 || fm.Body != "# Just markdown\n" {
		t.Errorf("no frontmatter: %+v, %v", fm, err)
	}
	if _, err := ParseFrontmatter("---\nname: x\nno closing"); err == nil {
		t.Error("unterminated frontmatter accepted")
	}
	fm, err = ParseFrontmatter("\xef\xbb\xbf---\r\nname: crlf\r\n---\r\nbody")
	if err != nil || fm.String("name") != "crlf" {
		t.Errorf("CRLF/BOM: %+v, %v", fm, err)
	}
	fm, err = ParseFrontmatter("---\nname: \"esc\\\"aped\"\nother: 'it''s'\nnum: 3 # comment\n---\n")
	if err != nil || fm.String("name") != `esc"aped` || fm.String("other") != "it's" || fm.String("num") != "3" {
		t.Errorf("quotes: %+v, %v", fm.Fields, err)
	}
	fm, err = ParseFrontmatter("---\nname: ok\njust words\n---\n")
	if err == nil {
		t.Errorf("broken line accepted: %+v", fm.Fields)
	}
	if fm.String("name") != "ok" {
		t.Errorf("fields parsed before the error should survive: %+v", fm.Fields)
	}
	fm, _ = ParseFrontmatter("---\ntools:\n- Read\n- Edit\n---\n")
	if got := fm.List("tools"); !reflect.DeepEqual(got, []string{"Read", "Edit"}) {
		t.Errorf("zero-indent list = %q", got)
	}
}
