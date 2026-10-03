package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Rewind: pick an earlier prompt, then restore the conversation to just before it,
// the files to their state then, or both. The conversation is restored by restarting
// the engine on the same session with --resume-session-at=<the message before it>;
// files through rewind_files (file checkpointing). The chosen prompt goes back into
// the input box.

const (
	ActRewind           ext.ActionID = "mantle:rewind"
	DialogRewind                     = "dialog.rewind"
	DialogRewindOptions              = "dialog.rewind-options"
)

func (f *feature) registerRewind(r ext.Registrar) {
	open := func(ctx ext.Ctx) tea.Cmd { return ctx.OpenDialog(DialogRewind, nil) }
	r.AddAction(ext.Action{
		ID: ActRewind, Context: ext.ContextChat, Description: "Rewind the conversation to an earlier prompt",
		Run: func(ctx ext.Ctx) (bool, tea.Cmd) { return true, open(ctx) },
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("rewind"), Name: "rewind", Aliases: []string{"checkpoint", "undo"}, Source: ext.SourceBuiltin,
		Description: "Restore the conversation and/or code to an earlier point",
		Run:         func(ctx ext.Ctx, args string) tea.Cmd { return open(ctx) },
	})
	r.AddDialog(DialogRewind, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		return f.newRewindSelector(ctx), nil
	})
	r.AddDialog(DialogRewindOptions, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		t, _ := args.(rewindTarget)
		return f.rewindOptions(ctx, t), nil
	})
	ext.Subscribe(r, "sessions.rewind-dryrun", f.onRewindDryRun)
	ext.Subscribe(r, "sessions.rewind-point", f.onRewindPoint)
	ext.Subscribe(r, "sessions.rewind-files", f.onRewindFiles)
}

// rewindEntry is one row of the selector: a prompt of this conversation, or the
// session /clear left behind.
type rewindEntry struct {
	uuid, text string
	cleared    string // session id: restore the pre-/clear conversation
}

// rewindSelector lists the prompts, newest last (MessageSelector, then Select keys).
type rewindSelector struct {
	f       *feature
	entries []rewindEntry
	sel     int
}

func (f *feature) newRewindSelector(ctx ext.Ctx) *rewindSelector {
	s := &rewindSelector{f: f}
	if old := f.lastCleared(); old != "" {
		label := f.titles[old]
		if label == "" {
			label = old
		}
		s.entries = append(s.entries, rewindEntry{cleared: old, text: "Conversation before /clear: " + label})
	}
	if t := ctx.Transcript(); t != nil {
		for _, it := range t.Items() {
			if it.Key != ext.KeyUserPrompt || it.ParentID != "" {
				continue
			}
			u, ok := it.Data.(*proto.User)
			if !ok || u.UUID == "" {
				continue
			}
			s.entries = append(s.entries, rewindEntry{uuid: u.UUID, text: oneLine(u.Message.Content.PlainText())})
		}
	}
	s.sel = len(s.entries) - 1
	return s
}

func (s *rewindSelector) ID() string                      { return DialogRewind }
func (s *rewindSelector) Init(ext.Ctx) tea.Cmd            { return nil }
func (s *rewindSelector) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (s *rewindSelector) KeyContext() string              { return ext.ContextMessageSelector }
func (s *rewindSelector) KeyContexts() []string {
	return []string{ext.ContextMessageSelector, ext.ContextSelect}
}
func (s *rewindSelector) Placement() ext.Placement { return ext.PlaceInline }

func (s *rewindSelector) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (s *rewindSelector) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	defer ctx.Invalidate(DialogRewind)
	switch a {
	case ext.ActSelectPrevious, ext.ActMessageSelectorUp:
		s.sel = max(s.sel-1, 0)
	case ext.ActSelectNext, ext.ActMessageSelectorDown:
		s.sel = min(s.sel+1, len(s.entries)-1)
	case ext.ActSelectFirst, ext.ActMessageSelectorTop:
		s.sel = 0
	case ext.ActSelectLast, ext.ActMessageSelectorBottom:
		s.sel = len(s.entries) - 1
	case ext.ActSelectAccept, ext.ActMessageSelectorSelect:
		return true, s.accept(ctx)
	case ext.ActSelectCancel, ext.ActAppInterrupt, ext.ActConfirmNo:
		return true, ctx.CloseDialog(DialogRewind)
	default:
		return false, nil
	}
	return true, nil
}

func (s *rewindSelector) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		return s.HandleAction(ctx, ext.ActSelectPrevious)
	case "down", "j":
		return s.HandleAction(ctx, ext.ActSelectNext)
	case "enter":
		return s.HandleAction(ctx, ext.ActSelectAccept)
	case "esc":
		return s.HandleAction(ctx, ext.ActSelectCancel)
	}
	return true, nil
}

func (s *rewindSelector) accept(ctx ext.Ctx) tea.Cmd {
	if s.sel < 0 || s.sel >= len(s.entries) {
		return ctx.CloseDialog(DialogRewind)
	}
	e := s.entries[s.sel]
	if e.cleared != "" {
		meta := sessions.SessionMeta{ID: e.cleared}
		return tea.Sequence(ctx.CloseDialog(DialogRewind),
			s.f.switchTo(ctx, meta, switchOpts{command: "/rewind", reason: "Restored the conversation from before /clear"}))
	}
	return tea.Sequence(ctx.CloseDialog(DialogRewind), s.f.rewindDryRun(ctx, rewindTarget{uuid: e.uuid, text: e.text}))
}

func (s *rewindSelector) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 20)
	lines := []string{
		th.Fg(theme.Accent).Bold(true).Render("Rewind"),
		th.Paint(theme.Inactive, "Restore the conversation to just before a prompt, the code to how it was then, or both."),
		"",
	}
	if len(s.entries) == 0 {
		lines = append(lines, th.Paint(theme.Inactive, "Nothing to rewind to yet."))
	}
	rows := a.MaxHeight - 6
	if rows <= 0 {
		rows = 12
	}
	start := max(0, min(s.sel-rows/2, len(s.entries)-rows))
	for i := start; i < len(s.entries) && i < start+rows; i++ {
		e := s.entries[i]
		text := e.text
		if text == "" {
			text = "(empty prompt)"
		}
		if i == s.sel {
			lines = append(lines, fit(th.Paint(theme.Suggestion, "› ")+th.Fg(theme.Suggestion).Bold(true).Render(text), w))
		} else {
			lines = append(lines, fit("  "+text, w))
		}
	}
	lines = append(lines, "", th.Paint(theme.Inactive, "↑↓ select · enter choose · esc cancel"))
	for i := range lines {
		lines[i] = fit(lines[i], w)
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

// rewindTarget is the chosen prompt and what a dry run says about its files.
type rewindTarget struct {
	uuid, text string
	codeOK     bool   // file checkpoints can restore the code
	codeNote   string // what restoring the code would do, or why it can't
}

type rewindDryRunMsg struct{ t rewindTarget }

// rewindDryRun asks the engine what restoring files would change, then opens the
// options.
func (f *feature) rewindDryRun(ctx ext.Ctx, t rewindTarget) tea.Cmd {
	eng := ctx.Engine(ext.MainEngine)
	switch {
	case !ext.ClaudeBool(ctx.Settings(), "fileCheckpointingEnabled", true):
		t.codeNote = "File checkpointing is off (fileCheckpointingEnabled)"
		return ext.Msg(rewindDryRunMsg{t})
	case eng == nil || !eng.Supports(proto.SubRewindFiles):
		t.codeNote = "Restoring code needs a running session with file checkpoints"
		return ext.Msg(rewindDryRunMsg{t})
	}
	req := proto.RewindFilesRequest{UserMessageID: t.uuid, DryRun: true}
	return controlCmd(eng.Control(proto.SubRewindFiles, req), func(r ext.ControlResultMsg) tea.Msg {
		var resp proto.RewindFilesResponse
		err := decodeControl(r, &resp)
		switch {
		case err != nil:
			t.codeNote = "Can't check the code: " + err.Error()
		case !resp.CanRewind:
			t.codeNote = "Code can't be restored"
			if resp.Error != "" {
				t.codeNote += ": " + resp.Error
			}
		default:
			t.codeOK = true
			t.codeNote = filesNote(resp)
		}
		return rewindDryRunMsg{t}
	})
}

// filesNote summarises a rewind_files reply ("3 files, +10 −4").
func filesNote(r proto.RewindFilesResponse) string {
	var files []string
	_ = json.Unmarshal(r.FilesChanged, &files)
	if len(files) == 0 && r.Insertions == 0 && r.Deletions == 0 {
		return "No file changes since then"
	}
	n := plural(len(files), "file", "files")
	if len(files) == 0 {
		n = "files"
	}
	return fmt.Sprintf("%s changed since then (+%d −%d)", n, r.Insertions, r.Deletions)
}

func (f *feature) onRewindDryRun(ctx ext.Ctx, m rewindDryRunMsg) tea.Cmd {
	return ctx.OpenDialog(DialogRewindOptions, m.t)
}

func (f *feature) rewindOptions(ctx ext.Ctx, t rewindTarget) ext.Dialog {
	codeDetail := ""
	if !t.codeOK {
		codeDetail = "unavailable"
	}
	return &choiceDialog{
		id: DialogRewindOptions, title: "Rewind to before: " + truncateRunes(t.text, 60),
		subtitle: t.codeNote,
		choices: []choice{
			{label: "Restore code and conversation", detail: codeDetail, disabled: !t.codeOK,
				run: func(ctx ext.Ctx) tea.Cmd { return f.restoreFiles(ctx, t, true) }},
			{label: "Restore conversation",
				run: func(ctx ext.Ctx) tea.Cmd { return f.restoreConversation(ctx, t) }},
			{label: "Restore code", detail: codeDetail, disabled: !t.codeOK,
				run: func(ctx ext.Ctx) tea.Cmd { return f.restoreFiles(ctx, t, false) }},
			{label: "Summarize from here", detail: "in Claude Code",
				run: func(ctx ext.Ctx) tea.Cmd { return ctx.OpenDialog(DialogHandoff, []string{"/rewind"}) }},
			{label: "Summarize up to here", detail: "in Claude Code",
				run: func(ctx ext.Ctx) tea.Cmd { return ctx.OpenDialog(DialogHandoff, []string{"/rewind"}) }},
		},
	}
}

type rewindFilesMsg struct {
	t        rewindTarget
	resp     proto.RewindFilesResponse
	err      error
	thenConv bool
}

// restoreFiles rewinds the files, then (thenConv) the conversation.
func (f *feature) restoreFiles(ctx ext.Ctx, t rewindTarget, thenConv bool) tea.Cmd {
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	req := proto.RewindFilesRequest{UserMessageID: t.uuid}
	return controlCmd(eng.Control(proto.SubRewindFiles, req), func(r ext.ControlResultMsg) tea.Msg {
		m := rewindFilesMsg{t: t, thenConv: thenConv}
		m.err = decodeControl(r, &m.resp)
		return m
	})
}

func (f *feature) onRewindFiles(ctx ext.Ctx, m rewindFilesMsg) tea.Cmd {
	if m.err != nil || (!m.resp.CanRewind && m.resp.Error != "") {
		msg := m.resp.Error
		if m.err != nil {
			msg = m.err.Error()
		}
		return notice(ctx, "rewind", "Code was not restored: "+msg, ext.NoticeError)
	}
	cmds := []tea.Cmd{notice(ctx, "rewind", "Code restored: "+strings.TrimSuffix(filesNote(m.resp), " since then"), ext.NoticeSuccess)}
	if m.thenConv {
		cmds = append(cmds, f.restoreConversation(ctx, m.t))
	}
	return tea.Sequence(cmds...)
}

type rewindPointMsg struct {
	t      rewindTarget
	sid    string
	parent string // the message before the target ("" = the target opened the conversation)
	path   string
	err    error
}

// restoreConversation finds the message before the target in the transcript, then
// restarts the engine there.
func (f *feature) restoreConversation(ctx ext.Ctx, t rewindTarget) tea.Cmd {
	if warn, ok := f.restartGuard(ctx, ext.MainEngine, "rewind", "the rewind"); !ok {
		return warn
	}
	sid, cwd, l := ctx.Session().SessionID, f.cwd(ctx), f.layout
	if sid == "" {
		return notice(ctx, "rewind", "No conversation to rewind", ext.NoticeInfo)
	}
	return func() tea.Msg {
		m := rewindPointMsg{t: t, sid: sid}
		p, err := l.FindSession(sid, cwd)
		if err != nil {
			m.err = err
			return m
		}
		m.path = p
		tr, err := sessions.Load(p)
		if err != nil {
			m.err = err
			return m
		}
		n := tr.Tree.Node(t.uuid)
		if n == nil {
			m.err = errors.New("that prompt is not in the saved conversation yet")
			return m
		}
		if n.Parent != nil {
			m.parent = n.Parent.Entry.UUID
		}
		return m
	}
}

func (f *feature) onRewindPoint(ctx ext.Ctx, m rewindPointMsg) tea.Cmd {
	if m.err != nil {
		return notice(ctx, "rewind", "Could not rewind: "+m.err.Error(), ext.NoticeError)
	}
	refill := f.refillPrompt(ctx, m.t.text)
	if m.parent == "" {
		// Rewinding to before the first prompt is a fresh conversation; the old one
		// stays resumable.
		return tea.Sequence(sendCommand(ctx, ext.MainEngine, "/clear"), refill)
	}
	st := f.engine(ext.MainEngine)
	st.loading = true
	return tea.Sequence(f.loadCmd(loadReq{
		engineID: ext.MainEngine, mode: modeSwitch, id: m.sid, path: m.path, cwd: f.cwd(ctx),
		title: f.titles[m.sid], leaf: m.parent,
		sw: switchOpts{at: m.parent, command: "the rewind", reason: "Conversation rewound"},
	}), refill)
}

// refillPrompt puts the rewound prompt back into the input box (EditorSetTextMsg once
// the input feature has it); until then the text is shown in a notice.
func (f *feature) refillPrompt(ctx ext.Ctx, text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return notice(ctx, "rewind-prompt", "Rewound prompt: "+truncateRunes(text, 200), ext.NoticeInfo)
}
