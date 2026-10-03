package chrome

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/prbadge"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Stories render each component with fixed data, for goldens, `mantle-ui story` and the
// /mantle builder.

func footerStories() []ext.Story {
	mk := func(id string, s sessionState, statusLine bool) ext.Story {
		return ext.Story{ID: FooterID + "/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			f := newFooter()
			f.s, f.statusLine = s, statusLine
			return f.View(ctx, a)
		}}
	}
	st := func(mode string) sessionState {
		s := newSessionState()
		s.Mode = mode
		return s
	}
	busy := st(ModeAcceptEdits)
	busy.Vim, busy.Background, busy.EditorEmpty = "INSERT", 2, false
	prStory := ext.Story{ID: FooterID + "/pr-links", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		f := newFooter()
		f.s = st(ModeDefault)
		f.pr = prWatch{links: true, maxLinks: 3,
			pr: &prbadge.PR{Number: 446, URL: "https://example.com/acme/app/pull/446", Review: prbadge.Pending},
			found: []prbadge.Link{
				{Text: "PROJ-12", URL: "https://jira.example/browse/PROJ-12"},
				{Text: "PROJ-31", URL: "https://jira.example/browse/PROJ-31"},
			}}
		return f.View(ctx, a)
	}}
	return []ext.Story{
		prStory,
		mk("default", st(ModeDefault), false),
		mk("accept-edits", st(ModeAcceptEdits), false),
		mk("plan", st(ModePlan), false),
		mk("auto", st(ModeAuto), false),
		mk("bypass", st(ModeBypass), false),
		mk("dont-ask", st(ModeDontAsk), true),
		mk("vim-background", busy, false),
	}
}

// storyEditor stands in for plan 04's editor in frame stories.
type storyEditor struct{ text string }

func (e storyEditor) ID() string                          { return EditorID }
func (e storyEditor) Init(ext.Ctx) tea.Cmd                { return nil }
func (e storyEditor) Update(ext.Ctx, tea.Msg) tea.Cmd     { return nil }
func (e storyEditor) View(ext.Ctx, ext.Area) ext.Rendered { return ext.Rendered{Text: e.text} }

func frameStories() []ext.Story {
	mk := func(id, text, editorMode, mode, name string) ext.Story {
		return ext.Story{ID: "chrome.promptFrame/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			f := wrapFrame(storyEditor{text}).(*promptFrame)
			f.s.EditorMode, f.s.Mode, f.s.Title = editorMode, mode, name
			return f.View(ctx, a)
		}}
	}
	return []ext.Story{
		mk("prompt", "❯ refactor the parser", "prompt", ModeDefault, ""),
		mk("bash", "! git status", "bash", ModeDefault, ""),
		mk("plan-named", "❯ outline the migration\n  then list the risks", "prompt", ModePlan, "db-migration"),
	}
}

func statusLineStories() []ext.Story {
	mk := func(id string, lines []string, notice string) ext.Story {
		return ext.Story{ID: StatusLineID + "/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			return ext.Rendered{Text: renderStatusLines(ctx, lines, notice, a)}
		}}
	}
	return []ext.Story{
		mk("two-lines", []string{
			"\x1b[36mOpus 5.5\x1b[0m | \x1b[32m92% left\x1b[0m | \x1b[34m~/work/app\x1b[0m | \x1b[35mmain\x1b[0m",
			"  \x1b]8;;https://example.com/acme/app\x1b\\acme/app\x1b]8;;\x1b\\ · $0.42 · 12m",
		}, ""),
		mk("failed", nil, "status line command failed: exit status 127: jq: command not found"),
	}
}

func todoStories() []ext.Story {
	list := func() Todos {
		var td Todos
		td.OnToolUse("t", "TodoWrite", json.RawMessage(`{"todos":[
			{"content":"Read the parser","status":"completed","activeForm":"Reading the parser"},
			{"content":"Write failing tests","status":"completed","activeForm":"Writing failing tests"},
			{"content":"Fix operator precedence","status":"in_progress","activeForm":"Fixing operator precedence"},
			{"content":"Update the docs","status":"pending","activeForm":"Updating the docs"},
			{"content":"Run the full suite","status":"pending","activeForm":"Running the full suite"},
			{"content":"Open a pull request","status":"pending","activeForm":"Opening a pull request"},
			{"content":"Tag a release","status":"pending","activeForm":"Tagging a release"}]}`))
		return td
	}
	mk := func(id string, expanded bool) ext.Story {
		return ext.Story{ID: TodosID + "/" + id, Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
			p := newTodoPanel()
			p.list, p.expanded = list(), expanded
			return p.View(ctx, a)
		}}
	}
	return []ext.Story{mk("collapsed", false), mk("expanded", true)}
}
