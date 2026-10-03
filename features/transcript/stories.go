package transcript

import (
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// storyFeature returns a feature whose store holds the sample session.
func storyFeature() *Feature {
	sf := New("story")
	sf.store.now = storyClock()
	for _, line := range strings.Split(sampleSession, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		ev, err := proto.Decode([]byte(line))
		if err != nil {
			continue
		}
		if init, ok := ev.(*proto.SystemInit); ok {
			sf.cwd = init.CWD
		}
		sf.store.Apply(ev)
	}
	sf.renderers = sf.rendererTable()
	return sf
}

// storyItems lists, per story, the sample items it shows.
var storyItems = []struct {
	id  string
	ids []string
}{
	{"user.prompt", []string{"user:u1"}},
	{"assistant.thinking", []string{"thk:a1:0"}},
	{"assistant.text", []string{"txt:a18:0"}},
	{"tool.Read", []string{"t_read"}},
	{"tool.Grep", []string{"t_grep"}},
	{"tool.Glob", []string{"t_glob"}},
	{"tool.Bash", []string{"t_bash"}},
	{"tool.Edit", []string{"t_edit"}},
	{"tool.Write", []string{"t_write"}},
	{"tool.TodoWrite", []string{"t_todo"}},
	{"tool.Agent", []string{"t_agent"}},
	{"tool.mcp", []string{"t_mcp1", "t_mcp2"}},
	{"tool.WebFetch", []string{"t_fetch"}},
	{"tool.error", []string{"t_bad"}},
	{"tool.AskUserQuestion", []string{"t_ask"}},
	{"system.informational", []string{"sys:inf1"}},
	{"system.result", []string{"result:r1"}},
	{"system.compact_boundary", []string{"sys:c1"}},
	{"system.error", []string{"err:a19"}},
	{"system.local_command", []string{"local:a20"}},
}

func (f *Feature) registerStories(r ext.Registrar) {
	for _, s := range storyItems {
		ids := s.ids
		r.AddStory(ext.Story{
			ID: FeatureID + "/" + s.id,
			Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
				sf := storyFeature()
				var lines []string
				for _, id := range ids {
					it := sf.store.Get(id)
					if it == nil {
						continue
					}
					if len(lines) > 0 {
						lines = append(lines, "")
					}
					rc := sf.renderCtx(c, it, a.Width)
					lines = append(lines, sf.rendererFor(it.Key)(rc, it).Lines...)
				}
				return ext.Rendered{Text: strings.Join(lines, "\n")}
			},
		})
	}
	r.AddStory(ext.Story{
		ID: FeatureID + "/session",
		Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			sf := storyFeature()
			var lines []string
			for _, it := range sf.store.Items() {
				out := sf.rendererFor(it.Key)(sf.renderCtx(c, it, a.Width), it).Lines
				lines = appendItem(lines, out)
			}
			return ext.Rendered{Text: strings.Join(lines, "\n")}
		},
	})
}

// storyClock is a fixed clock, so stories render the same every time.
func storyClock() func() time.Time {
	t := time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC)
	return func() time.Time { return t }
}
