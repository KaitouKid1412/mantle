package chrome

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func welcomeStories() []ext.Story {
	banner := ext.Story{ID: WelcomeID + "/banner", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		w := newWelcome()
		w.version, w.home = "0.1.0", "/home/dev"
		w.s.Version, w.s.Model, w.s.Cwd = "2.1.288", "claude-opus-5-5", "/home/dev/work/app"
		w.account = proto.Account{SubscriptionType: "max", Organization: "Acme"}
		return ext.Rendered{Text: w.banner(ctx, a.Width)}
	}}
	notes := ext.Story{ID: ReleaseNotesID + "/whats-new", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		n := ParseChangelog("## 2.1.288\n\n- Faster startup when many MCP servers are configured, " +
			"especially over slow networks\n- Fixed a crash when resizing\n\n## 2.1.287\n- Smaller fixes\n")
		return ext.Rendered{Text: renderNotes(ctx, "Claude Code", n, whatsNewItems, a.Width)}
	}}
	return []ext.Story{banner, notes}
}
