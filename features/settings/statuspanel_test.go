package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestStatusPanel(t *testing.T) {
	g := newRig(t)
	g.loadModels()
	os.MkdirAll(filepath.Join(g.home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(g.home, ".claude", "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	os.WriteFile(filepath.Join(g.home, ".claude", "CLAUDE.md"), []byte("# me"), 0o644)
	os.MkdirAll(filepath.Join(g.home, "repo", ".claude"), 0o755)
	os.WriteFile(filepath.Join(g.home, "repo", ".claude", "settings.json"), []byte(`{"model":`), 0o644)

	g.a.engine.sys = &proto.SystemInit{Model: "opus", ClaudeCodeVersion: "2.1.288", CWD: filepath.Join(g.home, "repo"),
		PermissionMode: "plan", Tools: []string{"Bash", "Read"},
		MCPServers: []proto.MCPServerInfo{{Name: "stale", Status: "pending"}}}
	g.eng.responses[proto.SubMCPStatus] = proto.MCPStatusResponse{MCPServers: []proto.MCPServerStatus{
		{Name: "github", Status: "connected"}, {Name: "linear", Status: "failed", Error: "timeout"}}}
	g.c.SettingsV.ClaudeM["permissions"] = map[string]any{"additionalDirectories": []any{"/srv/shared"}}

	g.command("status", "")
	if !reflect.DeepEqual(g.eng.subtypes(), []string{proto.SubMCPStatus}) {
		t.Errorf("controls %v", g.eng.subtypes())
	}
	g.mustContain(160,
		"Claude Code", "2.1.288", "Opus (claude-opus-5-5)", "Plan", "Also allowed", "/srv/shared",
		"github", "linear", "failed: timeout", "1 connected, 1 failed",
		"~/.claude/settings.json", "(ignored:", "~/repo/.claude/settings.local.json (not present)",
		"~/.claude/CLAUDE.md")
	if strings.Contains(g.screen(160), "stale") {
		t.Error("mcp_status should replace the system/init list")
	}
	g.press(ext.ActSettingsRetry)
	if n := len(g.eng.controls); n != 2 {
		t.Errorf("retry sent %d controls", n)
	}
	g.press(ext.ActSelectCancel)
	if g.dialog != nil {
		t.Error("esc did not close status")
	}
}

func TestVersionCommand(t *testing.T) {
	g := newRig(t)
	g.a.engine.sys = &proto.SystemInit{ClaudeCodeVersion: "2.1.288"}
	g.command("version", "")
	if len(g.c.Printed) != 1 || !strings.HasPrefix(g.c.Printed[0], "mantle ") ||
		!strings.HasSuffix(g.c.Printed[0], "Claude Code 2.1.288") {
		t.Errorf("printed %q", g.c.Printed)
	}
	mantleVersion = "1.2.3"
	defer func() { mantleVersion = "" }()
	if versionString() != "1.2.3" {
		t.Error("ldflags version ignored")
	}
}
