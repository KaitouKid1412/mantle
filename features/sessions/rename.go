package sessions

import (
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func (f *feature) registerRename(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("rename"), Name: "rename", Source: ext.SourceBuiltin,
		Description: "Rename this conversation (no name: suggest one)",
		ArgHint:     "[name]",
		Run:         f.runRename,
	})
	r.AddCommand(ext.Command{
		ID: ext.CommandID("recap"), Name: "recap", Source: ext.SourceBuiltin,
		Description: "Summarize the conversation in a line",
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			return sendCommand(ctx, ext.MainEngine, "/recap")
		},
	})
	ext.Subscribe(r, "sessions.titled", f.onTitled)
	ext.Subscribe(r, "sessions.blur", f.onBlur)
	ext.Subscribe(r, "sessions.focus", f.onFocus)
}

type titledMsg struct {
	sid, title string
	err        error
}

// runRename sets the title through rename_session, or asks the engine to suggest one
// from the conversation (generate_session_title) when no name is given. Engines
// without those requests get their own /rename command.
func (f *feature) runRename(ctx ext.Ctx, args string) tea.Cmd {
	eng := ctx.Engine(ext.MainEngine)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	name := strings.TrimSpace(args)
	sid := ctx.Session().SessionID
	switch {
	case name != "" && eng.Supports(proto.SubRenameSession):
		return controlCmd(eng.Control(proto.SubRenameSession, proto.RenameSessionRequest{Title: name}), func(r ext.ControlResultMsg) tea.Msg {
			return titledMsg{sid: sid, title: name, err: r.Err}
		})
	case name == "" && eng.Supports(proto.SubGenerateSessionTitle):
		desc := conversationDigest(ctx.Transcript(), 2000)
		if desc == "" {
			return notice(ctx, "rename", "Nothing to name yet: send a prompt first", ext.NoticeInfo)
		}
		req := proto.GenerateSessionTitleRequest{Description: desc, Persist: true}
		return controlCmd(eng.Control(proto.SubGenerateSessionTitle, req), func(r ext.ControlResultMsg) tea.Msg {
			var t proto.TitleResponse
			err := decodeControl(r, &t)
			return titledMsg{sid: sid, title: strings.TrimSpace(t.Title), err: err}
		})
	}
	return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(joinCommand("/rename", name))}})
}

func (f *feature) onTitled(ctx ext.Ctx, m titledMsg) tea.Cmd {
	if m.err != nil {
		return notice(ctx, "rename", "Rename failed: "+m.err.Error(), ext.NoticeError)
	}
	if m.title == "" {
		return nil
	}
	if m.sid != "" {
		f.titles[m.sid] = m.title
	}
	info := ctx.Session()
	if info.SessionID == m.sid {
		info.Title = m.title
	}
	return tea.Batch(
		ext.Msg(ext.SessionChangedMsg{EngineID: ext.MainEngine, Info: info}),
		notice(ctx, "rename", "Conversation renamed to "+m.title, ext.NoticeSuccess),
	)
}

// conversationDigest is the user's prompts (most recent last), cut to max bytes from
// the end, for title generation.
func conversationDigest(t ext.Transcript, max int) string {
	if t == nil {
		return ""
	}
	var parts []string
	for _, it := range t.Items() {
		if u, ok := it.Data.(*proto.User); ok && it.Key == ext.KeyUserPrompt {
			if s := oneLine(u.Message.Content.PlainText()); s != "" {
				parts = append(parts, s)
			}
		}
	}
	s := strings.Join(parts, "\n")
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// Away recap: Claude Code shows a one-line recap when you come back after 5+ minutes;
// the headless engine never does it on its own, so mantle asks for /recap.

const awayAfter = 5 * time.Minute

type awayState struct {
	since time.Time // when the terminal lost focus (zero while focused)
}

// awayEnabled: CLAUDE_CODE_ENABLE_AWAY_SUMMARY wins; otherwise awaySummaryEnabled
// (default on).
func awayEnabled(ctx ext.Ctx) bool {
	if v, err := strconv.ParseBool(os.Getenv("CLAUDE_CODE_ENABLE_AWAY_SUMMARY")); err == nil {
		return v
	}
	return ext.ClaudeBool(ctx.Settings(), "awaySummaryEnabled", true)
}

func (f *feature) onBlur(ctx ext.Ctx, _ tea.BlurMsg) tea.Cmd {
	f.away.since = f.now()
	return nil
}

func (f *feature) onFocus(ctx ext.Ctx, _ tea.FocusMsg) tea.Cmd {
	since := f.away.since
	f.away.since = time.Time{}
	if since.IsZero() || f.now().Sub(since) < awayAfter || !awayEnabled(ctx) {
		return nil
	}
	st := f.engine(ext.MainEngine)
	if st.state == proto.StateRunning || st.state == proto.StateRequiresAction || storeEmpty(ctx) {
		return nil
	}
	return sendCommand(ctx, ext.MainEngine, "/recap")
}
