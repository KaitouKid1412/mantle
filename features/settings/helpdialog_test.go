package settings

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func typeText(g *rig, s string) {
	for _, r := range s {
		g.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestHelpCommandsAndSearch(t *testing.T) {
	g := newRig(t)
	g.c.CommandList = append(g.c.CommandList,
		ext.Command{Name: "pirate", Description: "Talk like a pirate", Source: ext.SourceMod},
		ext.Command{Name: "secret", Hidden: true, Source: ext.SourceBuiltin})
	g.a.engine.commands = []proto.SlashCommand{
		{Name: "compact", Description: "Summarise", Builtin: true},
		{Name: "model", Description: "engine twin", Builtin: true},
		{Name: "review-pr", Description: "Review a pull request"},
	}
	g.command("help", "")
	if g.dlgID != dialogHelp {
		t.Fatalf("dialog %q", g.dlgID)
	}
	g.mustContain(100, "[General]", "mantle runs Claude Code", "Shortcuts", "permission mode", "meta+p", "code.claude.com/docs")
	g.press(ext.ActTabsNext)
	g.mustContain(100, "[Commands]", "Built-in", "/model [model]", "Claude Code", "/compact", "Custom commands", "/review-pr", "mantle mods", "/pirate")
	if strings.Contains(g.screen(100), "engine twin") || strings.Contains(g.screen(100), "/secret") {
		t.Errorf("shadowed or hidden command shown:\n%s", g.screen(100))
	}

	// "/" starts a search; typing filters; esc clears; a second esc closes.
	g.key(tea.KeyPressMsg{Code: '/', Text: "/"})
	if got := g.dialog.(ext.ContextStack).KeyContexts(); !reflect.DeepEqual(got, []string{ext.ContextHelp}) {
		t.Errorf("search contexts %v", got)
	}
	typeText(g, "pir")
	g.mustContain(100, "Search: pir", "/pirate")
	if strings.Contains(g.screen(100), "/compact") {
		t.Error("filter did not apply")
	}
	g.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	g.mustContain(100, "Search: pi")
	g.press(ext.ActHelpDismiss)
	if g.dialog == nil || strings.Contains(g.screen(100), "Search:") {
		t.Fatal("esc while searching should only clear the search")
	}
	g.press(ext.ActHelpDismiss)
	if g.dialog != nil {
		t.Error("second esc did not close help")
	}
	if p := g.printed(); len(p) != 1 || p[0] != "  ⎿  Help closed" {
		t.Errorf("printed %q", p)
	}
}

func TestHelpShortcuts(t *testing.T) {
	g := newRig(t)
	g.c.Bindings = append(g.c.Bindings, ext.Binding{Context: ext.ContextChat, Keys: "ctrl+m", Action: ext.ActChatModelPicker})
	g.command("help", "shortcuts")
	g.mustContain(160, "[Shortcuts]", "Global", "Chat")
	// Search for the model picker shows both the default and the user's key.
	g.key(tea.KeyPressMsg{Code: '/', Text: "/"})
	typeText(g, "choose the model")
	g.mustContain(160, "meta+p, ctrl+m", "Choose the model")
	g.press(ext.ActHelpDismiss) // clear
	g.press(ext.ActTabsPrevious)
	g.mustContain(100, "[Commands]")
	g.press(ext.ActTabsNext, ext.ActTabsNext) // wraps to General
	g.mustContain(100, "[General]")
}

func TestHelpScrollsAndAction(t *testing.T) {
	g := newRig(t)
	g.c.H = 14 // a short terminal forces scrolling
	g.command("help", "shortcuts")
	first := g.screen(100)
	if !strings.Contains(first, "more") {
		t.Fatalf("no scroll indicator:\n%s", first)
	}
	g.press(ext.ActSelectPageDown)
	if g.screen(100) == first {
		t.Error("page down did not scroll")
	}
	g.press(ext.ActSelectFirst)
	if g.screen(100) != first {
		t.Error("home did not return to the top")
	}
	g.press(ext.ActSelectCancel)
	if !g.action(ext.ActAppHelp) || g.dlgID != dialogHelp {
		t.Error("app:help did not open help")
	}
}
