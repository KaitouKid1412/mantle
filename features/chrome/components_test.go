package chrome

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func ev(e proto.Event) ext.EngineEventMsg {
	return ext.EngineEventMsg{EngineID: ext.MainEngine, Event: e}
}

func plainView(c ext.Component, ctx ext.Ctx, w int) string {
	return ansi.Strip(c.View(ctx, ext.Area{Width: w}).Text)
}

func TestFooterTracksMode(t *testing.T) {
	ctx := exttest.NewCtx()
	f := newFooter()
	f.Init(ctx)
	if got := plainView(f, ctx, 100); !strings.Contains(got, "manual approval") || !strings.Contains(got, "? for shortcuts") {
		t.Errorf("initial footer = %q", got)
	}
	f.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{PermissionMode: ModePlan}})
	if !slices.Contains(ctx.Invalidated, FooterID) || !strings.Contains(plainView(f, ctx, 100), "planning only") {
		t.Error("SessionChangedMsg should switch to plan mode")
	}
	f.Update(ctx, ev(&proto.Status{PermissionMode: ModeAcceptEdits}))
	if !strings.Contains(plainView(f, ctx, 100), "edits auto-approved") {
		t.Error("status event should update the mode")
	}
	f.Update(ctx, ext.EngineEventMsg{EngineID: "builder", Event: &proto.Status{PermissionMode: ModeBypass}})
	if strings.Contains(plainView(f, ctx, 100), "all permission checks off") {
		t.Error("other engines must not change the footer")
	}
	f.Update(ctx, ext.EditorStateMsg{Mode: "prompt", Vim: "INSERT", Empty: false})
	got := plainView(f, ctx, 100)
	if !strings.Contains(got, "-- INSERT --") || strings.Contains(got, "? for shortcuts") {
		t.Errorf("vim/typing footer = %q", got)
	}
	f.Update(ctx, ev(&proto.BackgroundTasksChanged{Tasks: []proto.BackgroundTask{{TaskID: "a"}, {TaskID: "b", Ambient: true}}}))
	if !strings.Contains(plainView(f, ctx, 100), "1 background task · /tasks") {
		t.Errorf("background count = %q", plainView(f, ctx, 100))
	}
}

func TestFooterLayoutLikeClaudeCode(t *testing.T) {
	ctx := exttest.NewCtx()
	f := newFooter()
	f.Init(ctx)
	// Manual mode: the shortcuts hint follows the indicator; no effort known yet.
	if got := plainView(f, ctx, 100); !strings.HasPrefix(got, "  ⏸ manual approval · ? for shortcuts") ||
		strings.Contains(got, "to change") || strings.Contains(got, "/effort") {
		t.Errorf("manual footer = %q", got)
	}
	// Another mode names the cycle key instead; the engine's effort shows on the right.
	f.Update(ctx, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{PermissionMode: ModePlan}})
	f.Update(ctx, ev(&proto.SystemInit{Effort: "medium"}))
	got := plainView(f, ctx, 100)
	if !strings.HasPrefix(got, "  ⏸ planning only · shift+tab to change") || !strings.HasSuffix(got, "◑ medium · /effort") ||
		len([]rune(got)) != 98 || // two columns of margin on each side, as Claude Code
		strings.Contains(got, "? for shortcuts") {
		t.Errorf("plan footer = %q", got)
	}
	// A menu or hint below the prompt takes the footer's place.
	f.Update(ctx, ext.EditorStateMsg{Mode: "prompt", Empty: false, Panel: true})
	if got := plainView(f, ctx, 100); got != "" {
		t.Errorf("footer under a menu = %q", got)
	}
	f.Update(ctx, ext.EditorStateMsg{Mode: "prompt", Empty: true})
	if plainView(f, ctx, 100) == "" {
		t.Error("the footer comes back when the menu closes")
	}
	// The effortLevel setting stands in until the engine reports one.
	ctx2 := exttest.NewCtx()
	ctx2.SettingsV.ClaudeM["effortLevel"] = "high"
	f2 := newFooter()
	f2.Init(ctx2)
	if got := plainView(f2, ctx2, 100); !strings.HasSuffix(got, "◕ high · /effort") {
		t.Errorf("effort from settings = %q", got)
	}
	// Nothing sets it, but the default model takes an effort level: the default shows.
	ctx3 := exttest.NewCtx()
	f3 := newFooter()
	f3.Init(ctx3)
	resp, _ := json.Marshal(proto.InitializeResponse{Models: []proto.ModelInfo{
		{Value: "default", SupportsEffort: true}, {Value: "haiku", ResolvedModel: "claude-haiku-4-5"}}})
	f3.Update(ctx3, ext.ControlResultMsg{EngineID: ext.MainEngine, Subtype: proto.SubInitialize, Resp: resp})
	if got := plainView(f3, ctx3, 100); !strings.HasSuffix(got, "◑ medium · /effort") {
		t.Errorf("default effort = %q", got)
	}
	f3.Update(ctx3, ev(&proto.SystemInit{Model: "claude-haiku-4-5"}))
	if got := plainView(f3, ctx3, 100); strings.Contains(got, "/effort") {
		t.Errorf("a model without effort levels shows no hint: %q", got)
	}
}

func TestFooterStatusLineAndHideVim(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["statusLine"] = map[string]any{"type": "command", "command": "x", "hideVimModeIndicator": true}
	f := newFooter()
	f.Init(ctx)
	f.Update(ctx, ext.EditorStateMsg{Mode: "prompt", Vim: "INSERT", Empty: true})
	got := plainView(f, ctx, 100)
	if strings.Contains(got, "? for shortcuts") || strings.Contains(got, "INSERT") {
		t.Errorf("footer with statusLine = %q", got)
	}
	delete(ctx.SettingsV.ClaudeM, "statusLine")
	f.Update(ctx, ext.SettingsMsg{Changed: []string{"statusLine"}})
	if got := plainView(f, ctx, 100); !strings.Contains(got, "INSERT") || !strings.Contains(got, "? for shortcuts") {
		t.Errorf("after settings change = %q", got)
	}
}

func TestBarNarrow(t *testing.T) {
	ctx := exttest.NewCtx()
	f := newFooter()
	f.s.Mode = ModeBypass
	for _, w := range []int{1, 5, 12, 20, 30} {
		out := f.View(ctx, ext.Area{Width: w}).Text
		if ansi.StringWidth(out) > w {
			t.Errorf("width %d: %q is %d wide", w, ansi.Strip(out), ansi.StringWidth(out))
		}
	}
	if f.View(ctx, ext.Area{Width: 0}).Text != "" {
		t.Error("zero width renders nothing")
	}
}

// focusEditor is a Focusable editor that records calls.
type focusEditor struct {
	storyEditor
	keys    []string
	actions []ext.ActionID
	focused bool
}

func (e *focusEditor) View(ext.Ctx, ext.Area) ext.Rendered {
	return ext.Rendered{Text: "❯ hi", Cursor: &tea.Cursor{Position: tea.Position{X: 4, Y: 0}}}
}
func (e *focusEditor) KeyContext() string { return ext.ContextChat }
func (e *focusEditor) KeyContexts() []string {
	return []string{ext.ContextAutocomplete, ext.ContextChat}
}
func (e *focusEditor) OnFocus(ext.Ctx) tea.Cmd { e.focused = true; return nil }
func (e *focusEditor) OnBlur(ext.Ctx) tea.Cmd  { e.focused = false; return nil }
func (e *focusEditor) HandleKey(_ ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	e.keys = append(e.keys, k.String())
	return true, nil
}
func (e *focusEditor) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }
func (e *focusEditor) HandleAction(_ ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	e.actions = append(e.actions, a)
	return true, nil
}

func TestPromptFrameDelegates(t *testing.T) {
	ctx := exttest.NewCtx()
	ed := &focusEditor{}
	w := wrapFrame(ed)
	if w.ID() != EditorID {
		t.Errorf("ID = %q", w.ID())
	}
	fw := w.(*promptFrame)
	if fw.KeyContext() != ext.ContextChat || !slices.Equal(fw.KeyContexts(), []string{ext.ContextAutocomplete, ext.ContextChat}) {
		t.Error("contexts not delegated")
	}
	if ok, _ := fw.HandleKey(ctx, tea.KeyPressMsg{Code: 'a', Text: "a"}); !ok || len(ed.keys) != 1 {
		t.Error("HandleKey not delegated")
	}
	if ok, _ := fw.HandleAction(ctx, ext.ActChatSubmit); !ok || ed.actions[0] != ext.ActChatSubmit {
		t.Error("HandleAction not delegated")
	}
	fw.OnFocus(ctx)
	if !ed.focused {
		t.Error("OnFocus not delegated")
	}
	r := w.View(ctx, ext.Area{Width: 20, MaxHeight: 5})
	lines := strings.Split(ansi.Strip(r.Text), "\n")
	if len(lines) != 3 || lines[1] != "❯ hi" || lines[0] != strings.Repeat("─", 20) {
		t.Errorf("frame = %q", lines)
	}
	if r.Cursor == nil || r.Cursor.Y != 1 || r.Cursor.X != 4 {
		t.Errorf("cursor = %+v", r.Cursor)
	}

	// A non-focusable inner component: safe defaults.
	plain := wrapFrame(storyEditor{"x"}).(*promptFrame)
	if ok, _ := plain.HandleKey(ctx, tea.KeyPressMsg{}); ok || plain.KeyContexts() != nil {
		t.Error("non-focusable defaults")
	}
	if wrapFrame(storyEditor{""}).View(ctx, ext.Area{Width: 20}).Text != "" {
		t.Error("an empty editor gets no frame")
	}
}

func TestPromptFrameColourAndInvalidate(t *testing.T) {
	ctx := exttest.NewCtx()
	f := wrapFrame(storyEditor{"x"}).(*promptFrame)
	f.Update(ctx, ext.EditorStateMsg{Mode: "bash"})
	if !slices.Contains(ctx.Invalidated, EditorID) {
		t.Error("mode change must invalidate the editor's ID")
	}
	if frameToken("bash", ModePlan, "red") != "bashBorder" || frameToken("prompt", ModePlan, "red") != "planMode" ||
		frameToken("prompt", ModeDefault, "") != "promptBorder" || frameToken("prompt", ModeDefault, "red") != "red_FOR_SUBAGENTS_ONLY" ||
		frameToken("prompt", ModeDefault, "mauve") != "promptBorder" {
		t.Error("frame tokens")
	}
}

func TestWrapRegistered(t *testing.T) {
	for _, f := range ext.Pending() {
		if f.ID != "chrome.promptFrame" {
			continue
		}
		r, err := exttest.Setup(f)
		if err != nil {
			t.Fatal(err)
		}
		ws := r.Wrapped[EditorID]
		if len(ws) != 1 {
			t.Fatalf("wraps = %v", ws)
		}
		fn, ok := ws[0].(func(ext.Component) ext.Component)
		if !ok {
			t.Fatalf("wrap has type %T", ws[0])
		}
		if fn(storyEditor{"x"}).ID() != EditorID {
			t.Error("wrapped ID")
		}
		return
	}
	t.Fatal("chrome.promptFrame not registered")
}

// fakeSL wires a statusLineComp to a fake exec.
type fakeSL struct {
	mu    sync.Mutex
	stdin [][]byte
	out   string
	err   error
}

func (f *fakeSL) exec(_ context.Context, req statusline.ExecRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stdin = append(f.stdin, req.Stdin)
	return []byte(f.out), f.err
}

func (f *fakeSL) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var m map[string]any
	_ = json.Unmarshal(f.stdin[len(f.stdin)-1], &m)
	return m
}

func newTestSL(fx *fakeSL) *statusLineComp {
	c := newStatusLine()
	c.newRunner = func(cfg statusline.Config) *statusline.Runner {
		cfg.Exec, cfg.Debounce = fx.exec, time.Millisecond
		return statusline.NewRunner(cfg)
	}
	return c
}

// feed delivers msg and runs the returned Cmds, feeding addressed results back in.
func feed(t *testing.T, ctx ext.Ctx, c ext.Component, msg tea.Msg) {
	t.Helper()
	cmd := c.Update(ctx, msg)
	for _, m := range exttest.Exec(cmd) {
		if am, ok := m.(ext.AddressedMsg); ok && am.To == c.ID() {
			if _, ok := am.Msg.(slResultMsg); ok {
				c.Update(ctx, am.Msg) // the next wait Cmd is dropped: one result per feed
			}
		}
	}
}

// settle feeds msg, then waits for the run it triggers and feeds its result back.
func settle(t *testing.T, ctx ext.Ctx, c *statusLineComp, msg tea.Msg) {
	t.Helper()
	c.Update(ctx, msg)
	select {
	case res := <-c.runner.Results():
		c.Update(ctx, slResultMsg{gen: c.gen, res: res})
	case <-time.After(5 * time.Second):
		t.Fatal("no status line run")
	}
}

func TestStatusLineComponent(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["statusLine"] = map[string]any{"type": "command", "command": "sl", "padding": float64(1)}
	fx := &fakeSL{out: "\x1b[36mOpus\x1b[0m | 92% left\n"}
	c := newTestSL(fx)
	c.Init(ctx)
	c.d.configDir = "/cfg"
	if c.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Fatal("nothing before the session starts")
	}
	feed(t, ctx, c, ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: ext.SessionInfo{
		SessionID: "s1", Cwd: "/w/app", Model: "claude-opus-5-5", ClaudeVersion: "2.1.288", PermissionMode: "default",
	}})
	defer c.stop()
	if got := plainView(c, ctx, 80); got != "   Opus | 92% left" { // the footer's two-column inset, then the padding
		t.Fatalf("view = %q", got)
	}
	p := fx.last()
	if p["session_id"] != "s1" || p["cwd"] != "/w/app" || p["version"] != "2.1.288" ||
		p["transcript_path"] != "/cfg/projects/-w-app/s1.jsonl" {
		t.Errorf("payload = %v", p)
	}
	if m := p["model"].(map[string]any); m["id"] != "claude-opus-5-5" || m["display_name"] != "Opus 5.5" {
		t.Errorf("model = %v", m)
	}
	if cw := p["context_window"].(map[string]any); cw["used_percentage"] != nil || cw["context_window_size"] != float64(200000) {
		t.Errorf("context_window = %v", cw)
	}

	// A response with usage, then a result with cost and the model's window.
	settle(t, ctx, c, ev(&proto.Assistant{Message: proto.Message{Model: "claude-opus-5-5",
		Usage: &proto.Usage{InputTokens: 100, CacheReadInputTokens: 9900, OutputTokens: 50}}}))
	settle(t, ctx, c, ev(&proto.Result{TotalCostUSD: 0.5, DurationAPIMS: 1200,
		ModelUsage: map[string]proto.ModelUsage{"claude-opus-5-5": {ContextWindow: 1_000_000}}}))
	p = fx.last()
	cw := p["context_window"].(map[string]any)
	if cw["total_input_tokens"] != float64(10000) || cw["context_window_size"] != float64(1_000_000) || cw["used_percentage"] != float64(1) {
		t.Errorf("context_window after result = %v", cw)
	}
	if cost := p["cost"].(map[string]any); cost["total_cost_usd"] != 0.5 || cost["total_api_duration_ms"] != float64(1200) {
		t.Errorf("cost = %v", cost)
	}

	// Edits are counted from structured patches.
	c.Update(ctx, ev(&proto.User{
		Message:       proto.UserMessage{Role: "user", Content: proto.BlockContent(proto.ContentBlock{Type: proto.BlockToolResult, ToolUseID: "e1"})},
		ToolUseResult: json.RawMessage(`{"structuredPatch":[{"lines":[" a","-b","+c","+d"]}]}`),
	}))
	settle(t, ctx, c, ev(&proto.Assistant{Message: proto.Message{Model: "claude-opus-5-5"}}))
	if cost := fx.last()["cost"].(map[string]any); cost["total_lines_added"] != float64(2) || cost["total_lines_removed"] != float64(1) {
		t.Errorf("line counts = %v", cost)
	}

	// Dialogs hide the status line.
	c.Update(ctx, ext.DialogOpenedMsg{ID: "dialog.permission"})
	if c.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Error("hidden while a dialog is open")
	}
	c.Update(ctx, ext.DialogClosedMsg{ID: "dialog.permission"})
	if c.View(ctx, ext.Area{Width: 80}).Text == "" {
		t.Error("shown again after the dialog closes")
	}

	// Removing the setting stops the runner and clears the output.
	delete(ctx.SettingsV.ClaudeM, "statusLine")
	c.Update(ctx, ext.SettingsMsg{Changed: []string{"statusLine"}})
	if c.runner != nil || c.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Error("statusLine removed")
	}
}

func TestStatusLineErrorNotice(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["statusLine"] = map[string]any{"command": "sl"}
	fx := &fakeSL{err: errString("exit status 1")}
	c := newTestSL(fx)
	c.Init(ctx)
	feed(t, ctx, c, ext.SessionChangedMsg{Info: ext.SessionInfo{SessionID: "s"}})
	defer c.stop()
	if got := plainView(c, ctx, 80); got != "  status line command failed: exit status 1" {
		t.Errorf("view = %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestStatusLineConfig(t *testing.T) {
	s := exttest.NewSettings(map[string]any{"statusLine": map[string]any{
		"type": "command", "command": " ~/sl.sh ", "padding": float64(2), "refreshInterval": 0.2, "hideVimModeIndicator": true}})
	c := statusLineConfig(s)
	if c.Command != "~/sl.sh" || c.Padding != 2 || c.RefreshInterval != time.Second || !c.HideVimModeIndicator {
		t.Errorf("cfg = %+v", c)
	}
	if statusLineConfig(exttest.NewSettings(map[string]any{"statusLine": map[string]any{"type": "static", "command": "x"}})).Command != "" {
		t.Error("only type command runs")
	}
	if statusLineConfig(exttest.NewSettings(map[string]any{"disableAllHooks": true, "statusLine": map[string]any{"command": "x"}})).Command != "" {
		t.Error("disableAllHooks")
	}
	if statusLineConfig(exttest.NewSettings(map[string]any{"statusLine": "nope"})).Command != "" {
		t.Error("wrong shape")
	}
}

func TestModelDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5":           "Opus 5.5",
		"claude-sonnet-5-5[1m]":     "Sonnet 5.5 (1m context)",
		"claude-haiku-4-5-20251001": "Haiku 4.5",
		"claude-fable-5-1":          "Fable 5.1",
		"gpt-x":                     "gpt-x",
		"":                          "",
	} {
		if got := modelDisplayName(in); got != want {
			t.Errorf("modelDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranscriptPath(t *testing.T) {
	if got := transcriptPath("/c", "/Users/me/my.app/x y", "abc"); got != "/c/projects/-Users-me-my-app-x-y/abc.jsonl" {
		t.Errorf("path = %q", got)
	}
	long := "/" + strings.Repeat("deep/", 60) + "proj"
	if got, want := transcriptPath("/c", long, "id"), sessions.SessionFile(sessions.Layout{ConfigDir: "/c"}.ProjectDir(long), "id"); got != want || len(filepath.Base(filepath.Dir(got))) > 220 {
		t.Errorf("long path = %q, want %q", got, want)
	}
	if transcriptPath("/c", "", "abc") != "" || transcriptPath("/c", "/w", "") != "" {
		t.Error("unknown parts give no path")
	}
}

func TestRateLimitsInPayload(t *testing.T) {
	d := newSLData()
	now := time.Unix(1_000, 0)
	d.observe(now, ev(&proto.RateLimitEvent{RateLimitInfo: proto.RateLimitInfo{
		RateLimitType: "five_hour", Utilization: 0.25, ResetsAt: json.RawMessage(`5000`)}}))
	in := d.input(now)
	if in.RateLimits == nil || in.RateLimits.FiveHour.UsedPercentage != 25 || in.RateLimits.FiveHour.ResetsAt != 5000 {
		t.Fatalf("rate limits = %+v", in.RateLimits)
	}
	if d.dropExpired(time.Unix(4_999, 0)) || !d.dropExpired(time.Unix(5_000, 0)) || d.rate != nil {
		t.Error("expiry")
	}
}

// toolOK is the user message carrying a successful result for tool call id.
func toolOK(id string) *proto.User {
	return &proto.User{Message: proto.UserMessage{Role: "user", Content: proto.BlockContent(
		proto.ContentBlock{Type: proto.BlockToolResult, ToolUseID: id, Content: &proto.Content{Text: "ok"}})}}
}

func todoWrite(id string, todos string) *proto.Assistant {
	return &proto.Assistant{Message: proto.Message{Content: []proto.ContentBlock{{
		Type: proto.BlockToolUse, ID: id, Name: "TodoWrite", Input: json.RawMessage(`{"todos":` + todos + `}`)}}}}
}

func TestTodoPanel(t *testing.T) {
	ctx := exttest.NewCtx()
	p := newTodoPanel()
	p.Init(ctx)
	if p.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Fatal("empty list draws nothing")
	}
	p.Update(ctx, ev(todoWrite("t1", `[{"content":"A","status":"in_progress","activeForm":"Doing A"},{"content":"B","status":"pending"}]`)))
	p.Update(ctx, ev(toolOK("t1")))
	if got := plainView(p, ctx, 80); got != "Tasks 0/2 done · 1 in progress (ctrl+t to hide)\n  ▸ Doing A\n  ○ B" {
		t.Errorf("expanded by default = %q", got)
	}
	sub := todoWrite("t2", `[{"content":"Sub","status":"pending"}]`)
	sub.ParentToolUseID = "agent-1"
	p.Update(ctx, ev(sub))
	if len(p.list.Items()) != 2 {
		t.Error("subagent todos must not replace the main list")
	}

	handled, _ := p.toggle(ctx)
	if got := plainView(p, ctx, 80); !handled || got != "Tasks 0/2 done · 1 in progress · Doing A (ctrl+t to show)" {
		t.Errorf("collapsed = %q", got)
	}
	saved := true
	if ok, _ := ctx.Store(TodosID).Get("expanded", &saved); !ok || saved {
		t.Error("collapsed state not persisted")
	}
	p2 := newTodoPanel()
	p2.Init(ctx)
	if p2.expanded {
		t.Error("collapsed state not restored")
	}

	ctx.SettingsV.ClaudeM["todoFeatureEnabled"] = false
	p.Update(ctx, ext.SettingsMsg{Changed: []string{"todoFeatureEnabled"}})
	if p.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Error("todoFeatureEnabled=false hides the panel")
	}

	p.Update(ctx, ev(&proto.ConversationReset{NewConversationID: "s2"}))
	if len(p.list.Items()) != 0 {
		t.Error("reset clears the list")
	}
}

type fakeTranscript struct{ items []*ext.Item }

func (f fakeTranscript) Items() []*ext.Item { return f.items }
func (f fakeTranscript) Get(id string) *ext.Item {
	for _, it := range f.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}
func (f fakeTranscript) Committed() int { return 0 }

func TestTodoPanelRebuildsOnResume(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.TranscriptV = fakeTranscript{items: []*ext.Item{
		{ID: "c1", Key: "tool.TaskCreate", Data: &proto.ToolUse{ID: "c1", Name: "TaskCreate", Input: json.RawMessage(`{"subject":"Plan"}`)},
			Result: &proto.ToolResult{ToolUseID: "c1", Content: proto.TextContent("Task #1 created successfully: Plan")}},
		{ID: "u1", Key: "tool.TaskUpdate", Data: &proto.ToolUse{ID: "u1", Name: "TaskUpdate", Input: json.RawMessage(`{"taskId":"1","status":"completed"}`)},
			Result: &proto.ToolResult{ToolUseID: "u1", Content: proto.TextContent("Updated task #1")}},
		{ID: "x", ParentID: "agent", Data: &proto.ToolUse{ID: "x", Name: "TodoWrite", Input: json.RawMessage(`{"todos":[]}`)}},
	}}
	p := newTodoPanel()
	p.Init(ctx)
	p.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{SessionID: "resumed"}})
	items := p.list.Items()
	if len(items) != 1 || items[0].Status != TodoCompleted || items[0].Subject != "Plan" {
		t.Errorf("rebuilt = %+v", items)
	}
}

func TestTerminalState(t *testing.T) {
	ctx := exttest.NewCtx()
	env := terminal.Map{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.2.0"}
	c := newTerminal(env)
	c.Init(ctx)
	c.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Cwd: "/w/app"}})
	ts := c.TerminalState(ctx)
	if ts.WindowTitle != "app" || ts.Progress != nil {
		t.Errorf("idle state = %+v", ts)
	}
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRunning}))
	if ts := c.TerminalState(ctx); ts.Progress == nil || ts.Progress.State != tea.ProgressBarIndeterminate {
		t.Errorf("running = %+v", ts.Progress)
	}
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRequiresAction}))
	if ts := c.TerminalState(ctx); ts.Progress.State != tea.ProgressBarWarning {
		t.Errorf("waiting = %+v", ts.Progress)
	}
	c.Update(ctx, ev(&proto.Result{IsError: true}))
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateIdle}))
	if ts := c.TerminalState(ctx); ts.Progress == nil || ts.Progress.State != tea.ProgressBarError {
		t.Errorf("failed = %+v", ts.Progress)
	}
	c.Update(ctx, ext.EditorStateMsg{Mode: "prompt", Empty: false})
	if ts := c.TerminalState(ctx); ts.Progress != nil {
		t.Errorf("typing clears the error: %+v", ts.Progress)
	}

	c.Update(ctx, ev(&proto.SessionTitleChanged{Title: "Fix the parser"}))
	if got := c.TerminalState(ctx).WindowTitle; got != "Fix the parser" {
		t.Errorf("AI title = %q", got)
	}
	c.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Title: "parser-work"}})
	if got := c.TerminalState(ctx).WindowTitle; got != "parser-work" {
		t.Errorf("renamed = %q", got)
	}
	ctx.SettingsV.ClaudeM["terminalTitleFromRename"] = false
	ctx.SettingsV.ClaudeM["terminalProgressBarEnabled"] = false
	c.Update(ctx, ext.SettingsMsg{})
	c.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRunning}))
	if ts := c.TerminalState(ctx); ts.WindowTitle != "Fix the parser" || ts.Progress != nil {
		t.Errorf("settings off = %+v", ts)
	}

	off := newTerminal(terminal.Map{"CLAUDE_CODE_DISABLE_TERMINAL_TITLE": "1", "TERM_PROGRAM": "Apple_Terminal"})
	off.Init(ctx)
	off.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Cwd: "/w/app"}})
	off.Update(ctx, ev(&proto.SessionStateChanged{State: proto.StateRunning}))
	if ts := off.TerminalState(ctx); ts.WindowTitle != "" || ts.Progress != nil {
		t.Errorf("disabled/unsupported = %+v", ts)
	}
	if off.View(ctx, ext.Area{Width: 80}).Text != "" {
		t.Error("terminal component has no rows")
	}
}

func TestActionsRegistered(t *testing.T) {
	want := map[ext.ActionID]bool{ext.ActAppToggleTodos: false, ext.ActChatClearScreen: false, ActFooterSelect: false}
	for _, f := range ext.Pending() {
		if !strings.HasPrefix(f.ID, "chrome.") {
			continue
		}
		r, err := exttest.Setup(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range r.Actions {
			want[a.ID] = true
			if a.Run == nil {
				t.Errorf("%s has no Run", a.ID)
			}
		}
		if len(f.Parity) == 0 {
			t.Errorf("%s has no Parity tags", f.ID)
		}
	}
	for id, ok := range want {
		if !ok {
			t.Errorf("action %s not registered", id)
		}
	}
	for _, f := range ext.Pending() {
		if !strings.HasPrefix(f.ID, "chrome.") {
			continue
		}
		r, _ := exttest.Setup(f)
		for _, a := range r.Actions {
			if a.ID == ext.ActAppRedraw {
				t.Errorf("%s registers app:redraw; the host core owns it", f.ID)
			}
		}
	}
}

func TestEffortLineFullscreenOnly(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["effortLevel"] = "low"
	e := &effortLine{s: newSessionState()}
	f := newFooter()
	f.Init(ctx)
	if got := plainView(e, ctx, 40); got != "" {
		t.Errorf("inline: the effort line stays empty, got %q", got)
	}
	if !strings.HasSuffix(plainView(f, ctx, 100), "◔ low · /effort") {
		t.Error("inline: the footer carries the hint")
	}
	ctx.LayoutMode = ext.Fullscreen
	e.Update(ctx, ext.LayoutChangedMsg{Mode: ext.Fullscreen})
	if got := plainView(e, ctx, 40); got != strings.Repeat(" ", 23)+"◔ low · /effort" {
		t.Errorf("fullscreen effort line = %q", got)
	}
	if strings.Contains(plainView(f, ctx, 100), "/effort") {
		t.Error("fullscreen: the footer drops the hint")
	}
}
