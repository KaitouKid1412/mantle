package sessions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/clipboard"
	"github.com/KaitouKid1412/mantle/internal/term/clipcmd"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeClipboard captures what is copied.
func fakeClipboard(t *testing.T) *string {
	t.Helper()
	var got string
	old := clipcmd.Copier
	clipcmd.Copier = clipboard.Copier{
		Env:      terminal.Map{},
		GOOS:     "darwin",
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Run: func(_ context.Context, _ string, _ []string, stdin []byte) error {
			got = string(stdin)
			return nil
		},
	}
	t.Cleanup(func() { clipcmd.Copier = old })
	return &got
}

func textItem(id, text string) *ext.Item {
	return &ext.Item{ID: id, Key: ext.KeyAssistantText, State: ext.Done, Data: &proto.ContentBlock{Type: "text", Text: text}}
}

func toolItem(id, name, input, result string) *ext.Item {
	tu := &proto.ToolUse{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(input)}
	it := &ext.Item{ID: id, Key: ext.ToolKey(name), State: ext.Done, Data: tu}
	if result != "" {
		it.Result = &proto.ToolResult{ToolUseID: id, Content: proto.TextContent(result)}
	}
	return it
}

func sampleTranscript() *fakeTranscript {
	long := strings.Repeat("line\n", 25)
	return &fakeTranscript{items: []*ext.Item{
		promptItem("p1", "List the files"),
		toolItem("t1", "Bash", `{"command":"ls -la","description":"list"}`, long),
		textItem("a1", "There are two files.\nBoth are Go."),
		{ID: "c", Key: ext.KeySystemCompactBoundary, Data: &proto.CompactBoundary{}},
		promptItem("p2", "Show main"),
		textItem("a2", "Here:\n\n```go\npackage main\n\nfunc main() {}\n```\n\nAnd a script:\n\n```bash\ngo run .\n```"),
		{ID: "child", ParentID: "t1", Key: ext.KeyAssistantText, Data: &proto.ContentBlock{Text: "nested"}},
	}}
}

func TestTranscriptText(t *testing.T) {
	text := transcriptText(sampleTranscript())
	for _, want := range []string{
		"> List the files",
		"⏺ Bash(ls -la)\n  ⎿ line\n    line",
		"    … +5 lines",
		"⏺ There are two files.\n  Both are Go.",
		"─── conversation compacted ───",
		"> Show main",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("export lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "nested") {
		t.Error("subagent output exported at top level")
	}
}

func TestExport(t *testing.T) {
	h := newHarness(t)
	h.command("export", "")
	if len(h.ctx.Notices) != 1 || !strings.Contains(h.ctx.Notices[0].Text, "Nothing to export") {
		t.Fatalf("empty: %+v", h.ctx.Notices)
	}
	h.ctx.TranscriptV = sampleTranscript()
	dir := t.TempDir()
	h.f.getwd = func() (string, error) { return dir, nil }
	h.ctx.SessionValue.Cwd = dir
	h.command("export", "notes")
	b, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || !strings.Contains(string(b), "> Show main") {
		t.Fatalf("file: %v %q", err, b)
	}

	// No name: the dialog offers the clipboard or a dated file.
	got := fakeClipboard(t)
	h.ctx.SessionValue.Title = "Explain the Build!"
	h.command("export", "")
	d := h.openDialog(DialogExport, nil)
	if v := d.View(h.ctx, ext.Area{Width: 80}).Text; !strings.Contains(v, "2026-01-02-150405-explain-the-build.txt") {
		t.Fatalf("dialog = %q", v)
	}
	h.key(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !strings.Contains(*got, "> List the files") {
		t.Fatalf("clipboard = %q", *got)
	}
	if n := h.ctx.Notices[len(h.ctx.Notices)-1]; n.Text != "Copied to clipboard" {
		t.Fatalf("notice = %+v", n)
	}
}

func TestCopy(t *testing.T) {
	got := fakeClipboard(t)
	h := newHarness(t)
	h.ctx.TranscriptV = sampleTranscript()
	if rs := responses(h.ctx.TranscriptV); len(rs) != 2 {
		t.Fatalf("responses = %q", rs)
	}
	// The older response has no code: copied straight away.
	h.command("copy", "2")
	if *got != "There are two files.\nBoth are Go." {
		t.Fatalf("clipboard = %q", *got)
	}
	h.command("copy", "9")
	if n := h.ctx.Notices[len(h.ctx.Notices)-1]; !strings.Contains(n.Text, "Only 2 responses") {
		t.Fatalf("notice = %+v", n)
	}
	// The latest has code blocks: a picker.
	h.command("copy", "")
	d := h.openDialog(DialogCopy, responses(h.ctx.TranscriptV)[1])
	v := d.View(h.ctx, ext.Area{Width: 80}).Text
	if !strings.Contains(v, "Code block 1 (go)") || !strings.Contains(v, "Code block 2 (bash)") {
		t.Fatalf("picker = %q", v)
	}
	h.key(tea.KeyPressMsg{Code: '2', Text: "2"})
	if *got != "package main\n\nfunc main() {}" {
		t.Fatalf("clipboard = %q", *got)
	}
	// w writes the highlighted option to a file in the cwd.
	dir := t.TempDir()
	h.f.getwd = func() (string, error) { return dir, nil }
	h.ctx.SessionValue.Cwd = dir
	d = h.openDialog(DialogCopy, responses(h.ctx.TranscriptV)[1])
	h.key(tea.KeyPressMsg{Code: tea.KeyDown})
	h.key(tea.KeyPressMsg{Code: tea.KeyDown})
	h.key(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if b, err := os.ReadFile(filepath.Join(dir, "snippet.sh")); err != nil || string(b) != "go run .\n" {
		t.Fatalf("snippet: %v %q", err, b)
	}
	// copyFullResponse skips the picker.
	h.ctx.SettingsV = exttest.NewSettings(map[string]any{"copyFullResponse": true})
	opened := len(h.ctx.Opened)
	h.command("copy", "")
	if len(h.ctx.Opened) != opened || !strings.HasPrefix(*got, "Here:") {
		t.Fatalf("copyFullResponse: opened %v, clipboard %q", h.ctx.Opened, *got)
	}
}
