package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
	"github.com/KaitouKid1412/mantle/features/settings/termsetup"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func TestKeybindingsCreatesAndOpens(t *testing.T) {
	g := newRig(t)
	g.command("keybindings", "")
	path := filepath.Join(g.home, ".claude", "keybindings.json")
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), `"$schema"`) || !strings.Contains(string(b), `"bindings": []`) {
		t.Fatalf("skeleton %q %v", b, err)
	}
	if len(g.edited) != 1 || g.edited[0] != path {
		t.Errorf("edited %v", g.edited)
	}
	if !strings.Contains(lastNotice(g), "apply right away") {
		t.Errorf("notice %q", lastNotice(g))
	}
	// An existing file is never overwritten.
	os.WriteFile(path, []byte(`{"bindings":[{"context":"Chat","bindings":{"ctrl+s":null}}]}`), 0o644)
	g.command("keybindings", "")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "ctrl+s") {
		t.Error("existing keybindings overwritten")
	}
	g.command("keybindings", "mantle")
	if g.edited[len(g.edited)-1] != filepath.Join(g.home, ".mantle", "keybindings.json") {
		t.Errorf("mantle file %v", g.edited)
	}
}

func TestVimToggle(t *testing.T) {
	g := newRig(t)
	g.command("vim", "")
	if v := g.applied()[patch.User]["editorMode"]; v != "vim" || lastNotice(g) != "Vim keys on" {
		t.Errorf("on: %v %q", v, lastNotice(g))
	}
	g.c.SettingsV.ClaudeM["editorMode"] = "vim"
	g.command("vim", "")
	if v := g.applied()[patch.User]["editorMode"]; v != "normal" || lastNotice(g) != "Vim keys off" {
		t.Errorf("off: %v %q", v, lastNotice(g))
	}
}

func TestTerminalSetupApply(t *testing.T) {
	g := newRig(t)
	var gotKitty bool
	var applied []termsetup.Proposal
	g.a.termLoad = func(home string, kitty bool) (termsetup.Proposal, error) {
		gotKitty = kitty
		return termsetup.Proposal{Terminal: termsetup.VSCode, Status: termsetup.Edit, Kind: termsetup.TargetFile,
			Path: filepath.Join(home, "kb.json"), Summary: "Add a binding.",
			Diff: "--- a\n+++ b\n+  added line\n-  removed line\n"}, nil
	}
	g.a.termApply = func(home string, p termsetup.Proposal) (string, error) {
		applied = append(applied, p)
		return filepath.Join(home, "kb.json.mantle-backup-1"), nil
	}
	g.deliver(tea.KeyboardEnhancementsMsg{})
	g.command("terminal-setup", "")
	g.mustContain(100, "Terminal: VS Code", "Config: ~/kb.json", "Add a binding.", "+  added line", "apply (a backup is kept)")
	if gotKitty {
		t.Error("kitty keyboard reported without support")
	}
	g.press(ext.ActConfirmYes)
	if len(applied) != 1 || g.dialog != nil {
		t.Fatalf("applied %v dialog %v", applied, g.dialog)
	}
	if !strings.Contains(lastNotice(g), "Restart VS Code") || !strings.Contains(lastNotice(g), "~/kb.json.mantle-backup-1") {
		t.Errorf("notice %q", lastNotice(g))
	}
}

func TestTerminalSetupNothingToDo(t *testing.T) {
	g := newRig(t)
	g.a.termLoad = func(string, bool) (termsetup.Proposal, error) {
		return termsetup.Proposal{Terminal: termsetup.Ghostty, Status: termsetup.Native,
			Summary: "Shift+Enter already works here.", Notes: []string{"Nothing to install."}}, nil
	}
	called := false
	g.a.termApply = func(string, termsetup.Proposal) (string, error) { called = true; return "", nil }
	g.command("terminal-setup", "")
	g.mustContain(100, "Ghostty", "already works", "• Nothing to install.", "enter to close")
	g.press(ext.ActConfirmYes)
	if called || g.dialog != nil {
		t.Errorf("native: applied=%v open=%v", called, g.dialog != nil)
	}

	g2 := newRig(t)
	g2.a.termLoad = func(string, bool) (termsetup.Proposal, error) {
		return termsetup.Proposal{Terminal: termsetup.ITerm2}, errors.New("defaults: no such domain")
	}
	g2.command("terminal-setup", "")
	g2.mustContain(100, "Could not read the terminal's config: defaults: no such domain")
}

func TestTerminalSetupApplyError(t *testing.T) {
	g := newRig(t)
	g.a.termLoad = func(string, bool) (termsetup.Proposal, error) {
		return termsetup.Proposal{Terminal: termsetup.Alacritty, Status: termsetup.Edit, Diff: "+x\n"}, nil
	}
	g.a.termApply = func(string, termsetup.Proposal) (string, error) { return "", termsetup.ErrStale }
	g.command("terminal-setup", "")
	g.press(ext.ActConfirmYes)
	if !strings.Contains(lastNotice(g), "changed since") {
		t.Errorf("notice %q", lastNotice(g))
	}
}

func TestThemeEditCustomOpensEditor(t *testing.T) {
	g := newRig(t)
	dir := filepath.Join(g.home, ".claude", "themes")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ocean.json"), []byte(`{}`), 0o644)
	g.command("theme", "")
	g.press(ext.ActSelectLast, ext.ActThemeEditCustom)
	if len(g.edited) != 1 || g.edited[0] != filepath.Join(dir, "ocean.json") {
		t.Errorf("edited %v", g.edited)
	}
}
