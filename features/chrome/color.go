package chrome

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// promptColors are the names /color accepts; each maps to the theme's named colour.
var promptColors = []string{"red", "blue", "green", "yellow", "purple", "orange", "pink", "cyan"}

// promptColorMsg changes the prompt frame's colour ("" = default).
type promptColorMsg struct{ Color string }

func colorToken(name string) (theme.Token, bool) {
	if !slices.Contains(promptColors, name) {
		return "", false
	}
	return theme.Token(name + "_FOR_SUBAGENTS_ONLY"), true
}

// colorCommand is /color [<color>|default].
func colorCommand(ctx ext.Ctx, args string) tea.Cmd {
	name := strings.ToLower(strings.TrimSpace(args))
	if name == "" {
		return ctx.Notify(ext.Notice{Key: "chrome:color",
			Text:  "Usage: /color <" + strings.Join(promptColors, "|") + "|default>",
			Level: ext.NoticeInfo, Source: "chrome.promptFrame"})
	}
	if name == "default" || name == "reset" || name == "none" {
		name = ""
	} else if _, ok := colorToken(name); !ok {
		return ctx.Notify(ext.Notice{Key: "chrome:color",
			Text:  "Unknown colour " + name + "; pick one of " + strings.Join(promptColors, ", ") + " or default",
			Level: ext.NoticeWarning, Source: "chrome.promptFrame"})
	}
	cmds := []tea.Cmd{ext.Msg(promptColorMsg{Color: name})}
	if eng := ctx.Engine(""); eng != nil && eng.Supports(proto.SubSetColor) {
		c := name
		if c == "" {
			c = "default"
		}
		cmds = append(cmds, eng.Control(proto.SubSetColor, proto.SetColorRequest{Color: c}))
	}
	return tea.Batch(cmds...)
}

func colorCompletions(_ ext.Ctx, prefix string) []ext.Completion {
	var out []ext.Completion
	for _, c := range append(slices.Clone(promptColors), "default") {
		if strings.HasPrefix(c, strings.ToLower(prefix)) {
			out = append(out, ext.Completion{Value: c})
		}
	}
	return out
}
