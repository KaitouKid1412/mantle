package input

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

func TestSubmitWhenIdle(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'hello world'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 1 {
		t.Fatalf("sent %d prompts", len(ps))
	}
	if len(ps[0].Blocks) != 1 || ps[0].Blocks[0].Text != "hello world" || ps[0].Priority != "" || ps[0].UUID == "" {
		t.Fatalf("prompt %+v", ps[0])
	}
	if !r.s.ed.Empty() || !r.s.busy {
		t.Fatal("editor cleared, engine busy")
	}
	es, _ := history.Load(r.s.histPath)
	if len(es) != 1 || es[0].Display != "hello world" || es[0].Project != "/work/demo" {
		t.Fatalf("history %+v", es)
	}
}

func TestEmptySubmitDoesNothing(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'   '", "enter")
	if len(r.eng.prompts()) != 0 {
		t.Fatal("blank prompt sent")
	}
}

func TestBackslashEnterIsNewline(t *testing.T) {
	r := newRig(t, nil)
	r.keys(`'one\'`, "enter", "'two'")
	if r.text() != "one\ntwo" || len(r.eng.prompts()) != 0 {
		t.Fatalf("%q", r.text())
	}
	r.keys("ctrl+j", "'three'")
	if r.text() != "one\ntwo\nthree" {
		t.Fatalf("ctrl+j: %q", r.text())
	}
}

func TestQueueWhileBusyAndTakeBack(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'first'", "enter")
	r.keys("'second'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 2 || ps[1].Priority != proto.PriorityLater {
		t.Fatalf("second prompt priority %q", ps[1].Priority)
	}
	q, ok := lastMsg[ext.QueuedPromptsMsg](r)
	if !ok || len(q.Prompts) != 1 || q.Prompts[0].Text != "second" || q.Prompts[0].UUID != ps[1].UUID {
		t.Fatalf("queued %+v", q)
	}
	// Up on the empty prompt takes it back.
	r.keys("up")
	if r.text() != "second" || len(r.s.queue) != 0 {
		t.Fatalf("take back: %q", r.text())
	}
	cs := r.eng.controlsOf(proto.SubCancelAsyncMessage)
	if len(cs) != 1 || cs[0].req.(proto.CancelAsyncMessageRequest).MessageUUID != ps[1].UUID {
		t.Fatalf("cancel_async_message %+v", cs)
	}

	// Send now and queue-submit priorities.
	r.s.ed.Clear()
	r.keys("'urgent'")
	r.action(ext.ActChatSendNow)
	r.keys("'later'")
	r.action(ext.ActChatQueueSubmit)
	ps = r.eng.prompts()
	if ps[2].Priority != proto.PriorityNow || ps[3].Priority != proto.PriorityLater {
		t.Fatalf("priorities %q %q", ps[2].Priority, ps[3].Priority)
	}
	// The engine starting the queued prompt removes it from the queue.
	r.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.CommandLifecycle{CommandUUID: ps[3].UUID, State: proto.LifecycleStarted}})
	if len(r.s.queue) != 0 {
		t.Fatal("started prompt still queued")
	}
	// Turn end makes the engine idle.
	r.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.Result{}})
	if r.s.busy {
		t.Fatal("result ends busy")
	}
}

func TestPastesAndImagesInPrompt(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'see '")
	_, cmd := (&promptComp{s: r.s}).HandlePaste(r.c, tea.PasteMsg{Content: "l1\nl2\nl3\nl4"})
	r.run(cmd)
	r.event(editor.ImagePastedMsg{Image: &editor.Image{MediaType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}}})
	r.keys("'done'")
	want := "see [Pasted text #1 +3 lines] [Image #2] done"
	if r.text() != want {
		t.Fatalf("display %q", r.text())
	}
	r.keys("enter")
	p := r.eng.prompts()[0]
	if len(p.Blocks) != 2 || p.Blocks[0].Type != proto.BlockImage || p.Blocks[1].Text != "see l1\nl2\nl3\nl4 [Image #2] done" {
		t.Fatalf("blocks %+v", p.Blocks)
	}
	if p.Blocks[0].Source == nil || p.Blocks[0].Source.MediaType != "image/png" {
		t.Fatal("image source")
	}
	if len(p.InlinePastes) != 1 || p.InlinePastes[0] != "l1\nl2\nl3\nl4" {
		t.Fatalf("inline_pastes %q", p.InlinePastes)
	}
	es, _ := history.Load(r.s.histPath)
	if es[0].Display != want || es[0].PastedContents["1"].Content != "l1\nl2\nl3\nl4" {
		t.Fatalf("history %+v", es[0])
	}
}

func TestDroppedImageLoadsBeforeSend(t *testing.T) {
	r := newRig(t, nil)
	dir := t.TempDir()
	path := dir + "/shot.png"
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\xa7\x35\x81\x84\x00\x00\x00\x00IEND\xaeB`\x82")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	(&promptComp{s: r.s}).HandlePaste(r.c, tea.PasteMsg{Content: path})
	if r.text() != "[Image #1] " {
		t.Fatalf("%q", r.text())
	}
	r.keys("'what is this'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 1 || ps[0].Blocks[0].Type != proto.BlockImage {
		t.Fatalf("prompts %+v", ps)
	}
}

func TestSlashRouting(t *testing.T) {
	r := newRig(t, nil)
	ran := ""
	r.c.CommandList = append(r.c.CommandList, ext.Command{Name: "native", Source: ext.SourceBuiltin,
		Run: func(_ ext.Ctx, args string) tea.Cmd { ran = "native:" + args; return nil }})
	r.keys("'/native a b'", "enter")
	if ran != "native:a b" || len(r.eng.prompts()) != 0 {
		t.Fatalf("native ran %q, prompts %d", ran, len(r.eng.prompts()))
	}
	// Engine commands and unknown names go to the engine as text.
	r.keys("'/compact keep the plan'", "esc", "enter")
	r.keys("'/nosuch'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 2 || ps[0].Blocks[0].Text != "/compact keep the plan" || ps[1].Blocks[0].Text != "/nosuch" {
		t.Fatalf("engine prompts %+v", ps)
	}
	es, _ := history.Load(r.s.histPath)
	if len(es) != 3 || es[0].Display != "/native a b" {
		t.Fatal("slash commands are recorded in history")
	}
}

func TestSlashMenu(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'/co'")
	p := &promptComp{s: r.s}
	if !r.s.comp.open() || p.KeyContexts()[0] != ext.ContextAutocomplete {
		t.Fatal("menu should open")
	}
	names := itemValues(r.s.comp.items)
	if strings.Join(names, ",") != "cost,config,compact" {
		t.Fatalf("prefix matches, shortest first: %v", names)
	}
	r.s.ed.Clear()
	r.keys("'/cmt'")
	if names := itemValues(r.s.comp.items); len(names) == 0 || names[0] != "compact" {
		t.Fatalf("fuzzy match: %v", names)
	}
	r.s.ed.Clear()
	r.keys("'/co'")
	if r.s.ed.Ghost() == "" {
		t.Fatal("ghost completes the top match")
	}
	r.keys("down", "tab")
	if r.text() != "/"+names[1]+" " || r.s.comp.open() {
		t.Fatalf("accept: %q", r.text())
	}

	// Aliases match and are shown.
	r.s.ed.Clear()
	r.keys("'/bash'")
	if len(r.s.comp.items) == 0 || r.s.comp.items[0].value != "tasks" || r.s.comp.items[0].alias != "bashes" {
		t.Fatalf("alias: %+v", r.s.comp.items)
	}
	// Esc dismisses until the token changes.
	r.keys("esc")
	if r.s.comp.open() {
		t.Fatal("esc dismisses")
	}
	r.keys("'e'")
	if !r.s.comp.open() {
		t.Fatal("typing reopens")
	}

	// Enter on the menu runs the highlighted command.
	r.s.ed.Clear()
	r.keys("'/comp'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 1 || ps[0].Blocks[0].Text != "/compact" {
		t.Fatalf("enter runs the command: %+v", ps)
	}
}

func TestArgumentCompletion(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'/model '")
	if r.s.comp.kind != compArgs || len(r.s.comp.items) != 3 {
		t.Fatalf("args: %+v", r.s.comp.items)
	}
	r.keys("'so'", "tab")
	if r.text() != "/model sonnet " {
		t.Fatalf("%q", r.text())
	}
}

func itemValues(items []compItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.value)
	}
	return out
}

func TestMidPromptSlash(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'then run /comp'")
	if !r.s.comp.open() || r.s.comp.items[0].value != "compact" {
		t.Fatal("mid-prompt / completes")
	}
	r.keys("'xyz'")
	if r.s.comp.open() {
		t.Fatal("mid-prompt matching is prefix only")
	}
}

func TestFileMentions(t *testing.T) {
	r := newRig(t, nil)
	r.eng.reply = func(sub string, req any) (json.RawMessage, error) {
		q := req.(proto.FileSuggestionsRequest).Query
		return json.Marshal(proto.FileSuggestionsResponse{Suggestions: []proto.FileSuggestion{{Path: q + "in.go"}, {Path: "pkg/"}}})
	}
	r.keys("'look at @ma'")
	cs := r.eng.controlsOf(proto.SubFileSuggestions)
	if len(cs) != 3 || cs[2].req.(proto.FileSuggestionsRequest).Query != "ma" {
		t.Fatalf("file_suggestions %+v", cs)
	}
	if !r.s.comp.open() || r.s.comp.items[0].value != "main.go" {
		t.Fatalf("items %+v", r.s.comp.items)
	}
	r.keys("tab")
	if r.text() != "look at @main.go " {
		t.Fatalf("%q", r.text())
	}
	// Directories keep completing.
	r.keys("'@'", "down", "tab")
	if r.text() != "look at @main.go @pkg/" {
		t.Fatalf("dir: %q", r.text())
	}
}

func TestEmoji(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'I :hear'")
	if !r.s.comp.open() || r.s.comp.kind != compEmoji {
		t.Fatal("emoji menu")
	}
	r.keys("esc", "'t:'")
	if r.text() != "I ❤️" {
		t.Fatalf("closing colon replaces: %q", r.text())
	}
	off := newRig(t, map[string]any{"emojiCompletionEnabled": false})
	off.keys("'a :smile:'")
	if off.text() != "a :smile:" || off.s.comp.open() {
		t.Fatal("emoji completion off")
	}
}

func TestHistoryRecall(t *testing.T) {
	r := newRig(t, nil)
	r.event(historyLoadedMsg{entries: []history.Entry{
		{Display: "older", Project: "/work/demo"},
		{Display: "!ls -la", Project: "/work/demo"},
		{Display: "elsewhere", Project: "/other"},
		{Display: "newest\nsecond line", Project: "/work/demo"},
	}})
	r.keys("'draft'")
	r.keys("up")
	if r.text() != "newest\nsecond line" {
		t.Fatalf("up: %q", r.text())
	}
	// The cursor is at the end: up first moves within the entry.
	r.keys("up")
	if r.text() != "newest\nsecond line" {
		t.Fatal("up moves the cursor first")
	}
	r.keys("up")
	if r.text() != "ls -la" || r.s.mode != modeBash {
		t.Fatalf("bash entry: %q %s", r.text(), r.s.mode)
	}
	r.keys("up")
	if r.text() != "older" {
		t.Fatalf("project filter: %q", r.text())
	}
	r.keys("down", "down")
	r.keys("down", "down")
	if r.text() != "draft" || r.s.mode != modePrompt {
		t.Fatalf("draft restored: %q", r.text())
	}
	r.keys("down")
	if a := r.c.RanActions; len(a) == 0 || a[len(a)-1] != ActFooterSelect {
		t.Fatalf("down at the draft selects the footer: %v", a)
	}
}

func TestEscLadder(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'text'", "esc")
	if r.text() != "text" || len(r.c.Notices) == 0 {
		t.Fatal("first esc hints")
	}
	r.keys("esc")
	if !r.s.ed.Empty() {
		t.Fatal("double esc clears")
	}
	r.action(ext.ActChatUndo)
	if r.text() != "text" {
		t.Fatal("clear is undoable")
	}
	r.s.ed.Clear()
	r.c.ClockV.Advance(doubleEscWindow * 2)
	r.keys("esc", "esc")
	if a := r.c.RanActions; len(a) == 0 || a[len(a)-1] != ActRewind {
		t.Fatalf("double esc on empty opens rewind: %v", a)
	}
	// While busy, esc belongs to plan 05.
	r.s.busy = true
	if r.action(ext.ActChatCancel) {
		t.Fatal("esc while busy must be declined")
	}
}

func TestCtrlCAndCtrlD(t *testing.T) {
	r := newRig(t, nil)
	if r.action(ext.ActAppInterrupt) || r.action(ext.ActAppExit) {
		t.Fatal("empty prompt declines ctrl+c and ctrl+d")
	}
	r.keys("'abc'", "home")
	if !r.action(ext.ActAppExit) || r.text() != "bc" {
		t.Fatalf("ctrl+d deletes forward: %q", r.text())
	}
	if !r.action(ext.ActAppInterrupt) || !r.s.ed.Empty() {
		t.Fatal("ctrl+c clears")
	}
}

func TestBashMode(t *testing.T) {
	r := newRig(t, nil)
	r.s.cwd = t.TempDir()
	r.keys("'!'")
	if r.s.mode != modeBash || !r.s.ed.Empty() {
		t.Fatal("! enters bash mode")
	}
	st, _ := lastMsg[ext.EditorStateMsg](r)
	if st.Mode != modeBash {
		t.Fatalf("state %+v", st)
	}
	r.keys("backspace")
	if r.s.mode != modePrompt {
		t.Fatal("backspace on empty leaves bash mode")
	}
	r.keys("'!'", "'echo hi; echo oops >&2'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 1 || !ps[0].Composed || len(ps[0].Blocks) != 2 || ps[0].ShouldQuery == nil || !*ps[0].ShouldQuery {
		t.Fatalf("bash prompt %+v", ps)
	}
	if ps[0].Blocks[0].Text != "<bash-input>echo hi; echo oops >&2</bash-input>" ||
		ps[0].Blocks[1].Text != "<bash-stdout>hi\n</bash-stdout><bash-stderr>oops\n</bash-stderr>" {
		t.Fatalf("blocks %q / %q", ps[0].Blocks[0].Text, ps[0].Blocks[1].Text)
	}
	es, _ := history.Load(r.s.histPath)
	if es[0].Display != "!echo hi; echo oops >&2" {
		t.Fatalf("bash history %q", es[0].Display)
	}
}

func TestBashWithoutResponse(t *testing.T) {
	r := newRig(t, map[string]any{"respondToBashCommands": false})
	r.s.cwd = t.TempDir()
	r.keys("'!'", "'true'", "enter")
	ps := r.eng.prompts()
	if len(ps) != 1 || ps[0].ShouldQuery == nil || *ps[0].ShouldQuery || r.s.busy {
		t.Fatalf("output recorded without a turn: %+v busy=%v", ps, r.s.busy)
	}
}

func TestPrefill(t *testing.T) {
	cli.SetCurrent(cli.Startup{Prefill: "from a deep link"})
	defer cli.ClearCurrent()
	r := newRig(t, nil)
	if r.text() != "from a deep link" || !r.s.ed.AtEnd() || len(r.eng.prompts()) != 0 {
		t.Fatalf("prefill: %q", r.text())
	}
}

func TestBashCompletions(t *testing.T) {
	r := newRig(t, nil)
	dir := t.TempDir()
	os.Mkdir(dir+"/src", 0o700)
	os.WriteFile(dir+"/server.go", nil, 0o600)
	os.WriteFile(dir+"/.hidden", nil, 0o600)
	r.s.cwd = dir
	r.s.histAll = []history.Entry{{Display: "!go test ./...", Project: dir}, {Display: "!git status", Project: dir}}
	r.keys("'!'", "'go t'")
	if r.s.ed.Ghost() != "est ./..." {
		t.Fatalf("history ghost %q", r.s.ed.Ghost())
	}
	r.keys("tab")
	if r.text() != "go test ./..." {
		t.Fatalf("tab accepts: %q", r.text())
	}
	r.s.ed.Clear()
	r.keys("'cat ./s'")
	if r.s.comp.kind != compPath || len(r.s.comp.items) != 2 {
		t.Fatalf("path items %+v", r.s.comp.items)
	}
	r.keys("tab")
	if r.text() != "cat ./server.go " && r.text() != "cat ./src/" {
		t.Fatalf("path accept: %q", r.text())
	}
}

func TestSetText(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'draft'")
	(&promptComp{s: r.s}).HandlePaste(r.c, tea.PasteMsg{Content: "a\nb\nc\nd"})
	r.keys("'!'")
	r.s.setText("rewound [Pasted text #1 +3 lines] prompt")
	if r.text() != "rewound [Pasted text #1 +3 lines] prompt" || !r.s.ed.AtEnd() || len(r.s.ed.Chips()) != 1 {
		t.Fatalf("set text: %q", r.text())
	}
	if len(r.eng.prompts()) != 0 {
		t.Fatal("never submitted")
	}
	r.action(ext.ActChatUndo)
	if r.text() != "draft[Pasted text #1 +3 lines]!" {
		t.Fatalf("undoable: %q", r.text())
	}
	// Rewind sends it as a message.
	r.event(ext.EditorSetTextMsg{Text: "from rewind"})
	if r.text() != "from rewind" || len(r.eng.prompts()) != 0 {
		t.Fatalf("EditorSetTextMsg: %q", r.text())
	}
	if st, _ := lastMsg[ext.EditorStateMsg](r); st.Empty {
		t.Fatal("editor state follows")
	}
}

func TestInvisibleCharactersNeedSecondEnter(t *testing.T) {
	r := newRig(t, nil)
	r.s.ed.SetValue("hi​there")
	r.keys("enter")
	if len(r.eng.prompts()) != 0 || r.text() != "hithere" {
		t.Fatalf("first enter strips: %q", r.text())
	}
	r.keys("enter")
	if len(r.eng.prompts()) != 1 {
		t.Fatal("second enter sends")
	}
}

func TestHistorySearch(t *testing.T) {
	r := newRig(t, nil)
	r.s.sessionID = "s1"
	r.event(historyLoadedMsg{entries: []history.Entry{
		{Display: "build the docs", Project: "/work/demo", SessionID: "s0"},
		{Display: "build it", Project: "/other", SessionID: "s0"},
		{Display: "fix the build", Project: "/work/demo", SessionID: "s1"},
	}})
	r.keys("'draft'", "ctrl+r", "'build'")
	p := &promptComp{s: r.s}
	if p.KeyContexts()[0] != ext.ContextHistorySearch {
		t.Fatal("search context")
	}
	if m, _ := r.s.search.match(); m.Display != "fix the build" {
		t.Fatalf("newest match first: %q", m.Display)
	}
	r.keys("ctrl+r")
	if m, _ := r.s.search.match(); m.Display != "build the docs" {
		t.Fatalf("ctrl+r older: %q", m.Display)
	}
	r.keys("ctrl+s") // project -> all
	if len(r.s.search.matches) != 3 {
		t.Fatalf("all scope: %d", len(r.s.search.matches))
	}
	r.keys("ctrl+s") // all -> session
	if len(r.s.search.matches) != 1 {
		t.Fatalf("session scope: %d", len(r.s.search.matches))
	}
	r.keys("ctrl+c")
	if r.s.search != nil || r.text() != "draft" {
		t.Fatal("ctrl+c cancels and restores")
	}
	r.keys("ctrl+r", "'docs'", "tab")
	if r.text() != "build the docs" {
		t.Fatalf("tab accepts: %q", r.text())
	}
	r.s.ed.Clear()
	r.keys("ctrl+r", "'fix'", "enter")
	if ps := r.eng.prompts(); len(ps) != 1 || ps[0].Blocks[0].Text != "fix the build" {
		t.Fatal("enter executes")
	}
}

func TestStash(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'half written'")
	(&promptComp{s: r.s}).HandlePaste(r.c, tea.PasteMsg{Content: "a\nb\nc\nd"})
	r.keys("ctrl+s")
	if !r.s.ed.Empty() || r.s.stash == nil {
		t.Fatal("ctrl+s stashes")
	}
	r.keys("ctrl+s")
	if r.text() != "half written[Pasted text #1 +3 lines]" || r.s.ed.Text() != "half writtena\nb\nc\nd" {
		t.Fatalf("restore keeps chips: %q", r.text())
	}
	r.keys("ctrl+s", "'quick question'", "enter")
	if r.text() != "half written[Pasted text #1 +3 lines]" {
		t.Fatalf("stash returns after submit: %q", r.text())
	}
	// The stash survives a restart through the feature store.
	r.keys("ctrl+s")
	s2 := newState()
	s2.loadStash(r.c)
	if s2.stash == nil || s2.stash.Pastes["1"] != "a\nb\nc\nd" {
		t.Fatalf("persisted stash %+v", s2.stash)
	}
}

func TestEngineCommandsPublished(t *testing.T) {
	r := newRig(t, nil)
	resp, _ := json.Marshal(proto.InitializeResponse{Commands: []proto.SlashCommand{
		{Name: "review", Description: "Review a PR", ArgumentHint: "<pr>"},
	}})
	r.event(ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: resp})
	m, ok := lastMsg[ext.CommandsMsg](r)
	if !ok || m.Source != ext.SourceEngine || len(m.Commands) != 1 || m.Commands[0].ArgHint != "<pr>" || m.Commands[0].Run != nil {
		t.Fatalf("commands %+v", m)
	}
	r.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.SystemInit{SlashCommands: []string{"review", "skill-x"}}})
	m, _ = lastMsg[ext.CommandsMsg](r)
	if len(m.Commands) != 2 || m.Commands[1].Name != "skill-x" {
		t.Fatalf("init merge %+v", m.Commands)
	}
	r.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.CommandsChanged{Commands: []proto.SlashCommand{{Name: "only"}}}})
	m, _ = lastMsg[ext.CommandsMsg](r)
	if len(m.Commands) != 1 || m.Commands[0].Name != "only" {
		t.Fatalf("commands_changed %+v", m.Commands)
	}
}

func TestPromptSuggestion(t *testing.T) {
	r := newRig(t, nil)
	r.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.PromptSuggestion{Suggestion: "run the tests"}})
	if r.s.ed.Ghost() != "run the tests" {
		t.Fatal("suggestion shows as ghost text")
	}
	r.keys("tab")
	if r.text() != "run the tests" {
		t.Fatalf("tab accepts: %q", r.text())
	}
	off := newRig(t, map[string]any{"promptSuggestionEnabled": false})
	off.event(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: &proto.PromptSuggestion{Suggestion: "x"}})
	if off.s.ed.Ghost() != "" {
		t.Fatal("suggestions off")
	}
}

func TestVimMode(t *testing.T) {
	r := newRig(t, map[string]any{"editorMode": "vim", "vimInsertModeRemaps": map[string]any{"jj": "<Esc>"}})
	if !r.s.ed.VimEnabled() {
		t.Fatal("editorMode vim")
	}
	r.keys("'hello'", "esc")
	st, _ := lastMsg[ext.EditorStateMsg](r)
	if st.Vim != "NORMAL" {
		t.Fatalf("esc enters NORMAL: %+v", st)
	}
	r.keys("'0'", "'d'", "'w'")
	if !r.s.ed.Empty() {
		t.Fatalf("dw: %q", r.text())
	}
	r.keys("'/'")
	if r.s.search == nil {
		t.Fatal("/ in NORMAL opens history search")
	}
	r.keys("ctrl+c", "'i'", "'x'", "'j'", "'j'")
	if st, _ := lastMsg[ext.EditorStateMsg](r); st.Vim != "NORMAL" || r.text() != "x" {
		t.Fatalf("jj remap: %+v %q", st, r.text())
	}
	// A lone j types after the timeout.
	r.deliverTimeouts = true
	r.keys("'a'", "'j'")
	if r.text() != "xj" {
		t.Fatalf("timeout flush: %q", r.text())
	}
	// /vim (plan 08) writes editorMode; the editor follows the setting.
	r.c.SettingsV.ClaudeM["editorMode"] = "normal"
	r.event(ext.SettingsMsg{Changed: []string{"editorMode"}})
	if r.s.ed.VimEnabled() {
		t.Fatal("editorMode normal turns vim off")
	}
}

func TestHelpPanel(t *testing.T) {
	r := newRig(t, nil)
	r.keys("'?'")
	if !r.s.help || !r.s.ed.Empty() {
		t.Fatal("? on empty prompt shows help")
	}
	view := r.s.viewMenu(r.c, ext.Area{Width: 100})
	if !strings.Contains(view.Text, "for bash mode") || !strings.Contains(view.Text, "ctrl+r") {
		t.Fatalf("help:\n%s", view.Text)
	}
	r.keys("'a'")
	if r.s.help || r.text() != "a" {
		t.Fatal("typing hides help")
	}
	r.keys("'?'")
	if r.text() != "a?" {
		t.Fatal("? types normally when the prompt has text")
	}
}

func TestAttachmentsContext(t *testing.T) {
	r := newRig(t, nil)
	r.event(editor.ImagePastedMsg{Image: &editor.Image{Data: []byte{1}}})
	r.s.ed.EnterAttachments()
	p := &promptComp{s: r.s}
	if p.KeyContexts()[0] != ext.ContextAttachments {
		t.Fatal("attachments context")
	}
	r.keys("backspace")
	if len(r.s.ed.Chips()) != 0 || r.s.ed.InAttachments() {
		t.Fatal("remove")
	}
}

func TestSendRejectedWithoutEngine(t *testing.T) {
	r := newRig(t, nil)
	delete(r.c.Engines, ext.MainEngine)
	r.keys("'keep me'", "enter")
	if r.text() != "keep me" {
		t.Fatalf("draft kept: %q", r.text())
	}
	if len(r.noticeTexts()) == 0 {
		t.Fatal("notice")
	}
}

func TestEditorStateBroadcast(t *testing.T) {
	r := newRig(t, nil)
	st, ok := lastMsg[ext.EditorStateMsg](r)
	if !ok || st.Mode != modePrompt || !st.Empty || st.Vim != "" {
		t.Fatalf("initial %+v", st)
	}
	n := len(r.msgs)
	r.keys("'a'")
	st, _ = lastMsg[ext.EditorStateMsg](r)
	if st.Empty {
		t.Fatal("non-empty")
	}
	r.keys("'b'")
	for _, m := range r.msgs[n+1:] {
		if _, ok := m.(ext.EditorStateMsg); ok {
			t.Fatal("unchanged state is not re-sent")
		}
	}
}

func TestUltrathinkDecoration(t *testing.T) {
	r := newRig(t, nil)
	r.s.ed.SetValue("please ultrathink about it, not ultrathinking")
	spans := r.s.decorate(0, strings.Split("please ultrathink about it, not ultrathinking", ""))
	if len(spans) != 10 || spans[0].Start != 7 {
		t.Fatalf("spans %d", len(spans))
	}
}

func TestSpellcheck(t *testing.T) {
	r := newRig(t, map[string]any{"spellcheck": map[string]any{"enabled": true, "checker": "fake"}})
	var asked [][]string
	r.s.spell.run = func(_ context.Context, checker string, words []string) ([]string, error) {
		asked = append(asked, words)
		var bad []string
		for _, w := range words {
			if w == "teh" {
				bad = append(bad, w)
			}
		}
		return bad, nil
	}
	r.keys("'fix teh bug in foo/bar.go'")
	if len(asked) == 0 {
		t.Fatal("checker not run")
	}
	got := strings.Join(asked[len(asked)-1], " ")
	if strings.Contains(got, "bar") || strings.Contains(got, "go") && !strings.Contains(got, "fix") {
		t.Fatalf("paths are not checked: %q", got)
	}
	spans := r.s.decorate(0, graphemes(r.text()))
	if len(spans) != 1 || spans[0].Start != 4 || spans[0].End != 7 {
		t.Fatalf("underline spans %+v", spans)
	}
	// Known words are not asked again.
	n := len(asked)
	r.keys("' teh fix'")
	for _, a := range asked[n:] {
		for _, w := range a {
			if w == "teh" || w == "fix" {
				t.Fatalf("re-checked %q", w)
			}
		}
	}
	// A failing checker turns spellcheck off with a notice.
	r.s.spell.run = func(context.Context, string, []string) ([]string, error) { return nil, errNoChecker }
	r.keys("' zzyzx'")
	if !r.s.spell.failed || !strings.Contains(strings.Join(r.noticeTexts(), "|"), "Spellcheck is off") {
		t.Fatal("checker failure")
	}
}

func TestStoriesRender(t *testing.T) {
	r := newRig(t, nil)
	for _, st := range r.reg.Stories {
		out := st.Render(r.c, ext.Area{Width: 60})
		if out.Text == "" {
			t.Errorf("%s renders nothing", st.ID)
		}
	}
}
