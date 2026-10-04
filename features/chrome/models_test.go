package chrome

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/internal/term/osc"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

func TestIndicatorFor(t *testing.T) {
	for _, m := range []string{ModeDefault, ModeAcceptEdits, ModePlan, ModeAuto, ModeDontAsk, ModeBypass} {
		ind := IndicatorFor(m)
		if ind.Glyph == "" || ind.Label == "" {
			t.Errorf("%s: empty indicator", m)
		}
	}
	if !IndicatorFor("").Quiet || !IndicatorFor(ModeDefault).Quiet || IndicatorFor(ModePlan).Quiet {
		t.Error("only manual mode is quiet")
	}
	if got := IndicatorFor("futureMode").Text(); got != "• futureMode mode" {
		t.Errorf("unknown mode = %q", got)
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func labels(items []Todo) []string {
	var out []string
	for _, it := range items {
		out = append(out, string(it.Status)+":"+it.Label())
	}
	return out
}

func TestTodosTodoWrite(t *testing.T) {
	var td Todos
	if td.OnToolUse("t1", "TodoWrite", raw(`{"todos":[
		{"content":"Read code","status":"completed","activeForm":"Reading code"},
		{"content":"Write tests","status":"in_progress","activeForm":"Writing tests"},
		{"content":"Ship","status":"pending","activeForm":"Shipping"}]}`)) {
		t.Fatal("nothing changes before the result")
	}
	if !td.OnToolResult("t1", false, "Todos updated", nil) {
		t.Fatal("a successful TodoWrite result should change the list")
	}
	want := []string{"completed:Read code", "in_progress:Writing tests", "pending:Ship"}
	if got := labels(td.Items()); !slices.Equal(got, want) {
		t.Errorf("items = %v", got)
	}
	if done, total := td.Counts(); done != 1 || total != 3 {
		t.Errorf("counts = %d/%d", done, total)
	}
	if cur, ok := td.Current(); !ok || cur.Subject != "Write tests" {
		t.Errorf("Current = %+v", cur)
	}
	// A rejected call (the engine has no such tool) changes nothing.
	td.OnToolUse("rej", "TodoWrite", raw(`{"todos":[{"content":"Nope","status":"pending"}]}`))
	if td.OnToolResult("rej", true, "No such tool available: TodoWrite", nil) || len(td.Items()) != 3 {
		t.Errorf("rejected call applied: %+v", td.Items())
	}
	td.OnToolUse("t2", "TodoWrite", raw(`{"todos":[{"content":"Only","status":"completed"}]}`))
	td.OnToolResult("t2", false, "", nil)
	if !td.AllDone() || len(td.Items()) != 1 || td.Items()[0].ID != "1" {
		t.Errorf("replace: %+v", td.Items())
	}
	if td.OnToolUse("t3", "TodoWrite", raw(`not json`)) {
		t.Error("bad input must not change the list")
	}
	if td.OnToolUse("t4", "Bash", raw(`{}`)) {
		t.Error("other tools are ignored")
	}
}

func TestTodosTaskTools(t *testing.T) {
	var td Todos
	if td.OnToolUse("c1", "TaskCreate", raw(`{"subject":"Plan","activeForm":"Planning"}`)) {
		t.Error("create waits for its result")
	}
	td.OnToolUse("c2", "TaskCreate", raw(`{"subject":"Build"}`))
	td.OnToolUse("c3", "TaskCreate", raw(`{"subject":"Fails"}`))
	td.OnToolUse("c4", "TaskCreate", raw(`{"subject":"Structured"}`))
	if !td.OnToolResult("c1", false, "Task #1 created successfully: Plan", nil) {
		t.Fatal("result adds the task")
	}
	td.OnToolResult("c2", false, "Task #2 created successfully: Build", nil)
	if td.OnToolResult("c3", true, "error", nil) {
		t.Error("failed create adds nothing")
	}
	td.OnToolResult("c4", false, "", raw(`{"task":{"id":"9","subject":"Structured"}}`))
	if td.OnToolResult("zz", false, "Task #5", nil) {
		t.Error("unknown tool_use_id")
	}
	ids := []string{}
	for _, it := range td.Items() {
		ids = append(ids, it.ID)
	}
	if !slices.Equal(ids, []string{"1", "2", "9"}) {
		t.Errorf("ids = %v", ids)
	}

	for id, in := range map[string]string{
		"u1": `{"taskId":"1","status":"in_progress"}`,
		"u2": `{"taskId":2,"status":"completed","subject":"Build it"}`,
		"u3": `{"taskId":"9","status":"deleted"}`,
	} {
		td.OnToolUse(id, "TaskUpdate", raw(in))
		td.OnToolResult(id, false, "Updated", nil)
	}
	td.OnToolUse("u4", "TaskUpdate", raw(`{"taskId":"404","status":"completed"}`))
	if td.OnToolResult("u4", false, "", nil) {
		t.Error("unknown id")
	}
	want := []string{"in_progress:Planning", "completed:Build it"}
	if got := labels(td.Items()); !slices.Equal(got, want) {
		t.Errorf("items = %v", got)
	}
	td.Reset()
	if len(td.Items()) != 0 {
		t.Error("Reset")
	}
}

func TestTodosWindow(t *testing.T) {
	var td Todos
	var b strings.Builder
	b.WriteString(`{"todos":[`)
	for i := range 8 {
		status := "pending"
		if i < 4 {
			status = "completed"
		} else if i == 4 {
			status = "in_progress"
		}
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"content":"t` + string(rune('0'+i)) + `","status":"` + status + `"}`)
	}
	b.WriteString(`]}`)
	td.OnToolUse("x", "TodoWrite", raw(b.String()))
	td.OnToolResult("x", false, "", nil)

	shown, before, after := td.Window(5)
	if len(shown) != 5 || shown[0].Subject != "t3" || before != 3 || after != 0 {
		t.Errorf("window = %v before=%d after=%d", labels(shown), before, after)
	}
	shown, before, after = td.Window(3)
	if shown[0].Subject != "t3" || before != 3 || after != 2 {
		t.Errorf("window(3) = %v before=%d after=%d", labels(shown), before, after)
	}
	if shown, _, _ := td.Window(10); len(shown) != 8 {
		t.Error("small list shows everything")
	}
}

func TestWindowTitle(t *testing.T) {
	tests := []struct {
		in   TitleInput
		want string
	}{
		{TitleInput{SessionName: "auth refactor", AITitle: "Fix login", Cwd: "/w/app", FromRename: true}, "auth refactor"},
		{TitleInput{SessionName: "auth refactor", AITitle: "Fix login", Cwd: "/w/app", FromRename: false}, "Fix login"},
		{TitleInput{SessionName: "  ", Cwd: "/w/app/", FromRename: true}, "app"},
		{TitleInput{Cwd: "/"}, "/"},
		{TitleInput{AITitle: "bad\x1b]0;x\atitle"}, "bad]0;xtitle"},
		{TitleInput{}, ""},
	}
	for _, tt := range tests {
		if got := WindowTitle(tt.in); got != tt.want {
			t.Errorf("WindowTitle(%+v) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if TitleDisabled(terminal.Map{}) || TitleDisabled(terminal.Map{"CLAUDE_CODE_DISABLE_TERMINAL_TITLE": "0"}) {
		t.Error("unset or 0 keeps titles")
	}
	if !TitleDisabled(terminal.Map{"CLAUDE_CODE_DISABLE_TERMINAL_TITLE": "1"}) {
		t.Error("1 disables titles")
	}
}

func TestProgressTracker(t *testing.T) {
	var p ProgressTracker
	if p.State(true) != osc.ProgressNone {
		t.Error("starts empty")
	}
	p.OnState(StateRunning)
	if p.State(true) != osc.ProgressIndeterminate || p.State(false) != osc.ProgressNone {
		t.Error("running")
	}
	p.OnState(StateRequiresAction)
	if p.State(true) != osc.ProgressPause {
		t.Error("waiting on the user")
	}
	p.OnState(StateRunning)
	p.OnResult(true)
	p.OnState(StateIdle)
	if p.State(true) != osc.ProgressError {
		t.Error("error lingers after idle")
	}
	p.OnState(StateRunning)
	if p.State(true) != osc.ProgressIndeterminate {
		t.Error("new turn")
	}
	p.OnResult(false)
	p.OnState(StateIdle)
	if p.State(true) != osc.ProgressNone {
		t.Error("success clears")
	}
	p.OnResult(true)
	p.Clear()
	if p.State(true) != osc.ProgressNone {
		t.Error("Clear")
	}
}

const sampleChangelog = `# Changelog

## 2.1.288

- Fixed a thing
- Added another

## 2.1.287 (2026-09-30)
- Older change

## v2.1.10
- Ancient

# Appendix
not a version
`

func TestChangelog(t *testing.T) {
	notes := ParseChangelog(sampleChangelog)
	if len(notes) != 3 {
		t.Fatalf("notes = %+v", notes)
	}
	if notes[0].Version != "2.1.288" || !slices.Equal(notes[0].Lines, []string{"- Fixed a thing", "- Added another"}) {
		t.Errorf("first = %+v", notes[0])
	}
	if notes[1].Version != "2.1.287" || notes[2].Version != "2.1.10" {
		t.Errorf("versions = %s %s", notes[1].Version, notes[2].Version)
	}
	if !slices.Equal(notes[2].Lines, []string{"- Ancient"}) {
		t.Errorf("appendix leaked: %q", notes[2].Lines)
	}
	since := NotesSince(notes, "2.1.287", 0)
	if len(since) != 1 || since[0].Version != "2.1.288" {
		t.Errorf("since = %+v", since)
	}
	if got := NotesSince(notes, "", 2); len(got) != 2 {
		t.Errorf("limit: %d", len(got))
	}
	if got := LatestVersion(notes); got != "2.1.288" {
		t.Errorf("latest = %q", got)
	}
	if LatestVersion(nil) != "" {
		t.Error("empty")
	}
	if p := ClaudeChangelogPath(terminal.Map{"CLAUDE_CONFIG_DIR": "/cfg"}); p != "/cfg/cache/changelog.md" {
		t.Errorf("path = %q", p)
	}
}
