package sessions

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func (f *feature) registerClear(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID:          ext.CommandID("clear"),
		Name:        "clear",
		Aliases:     []string{"reset", "new"},
		Description: "Start a new conversation (this one stays resumable)",
		Source:      ext.SourceBuiltin,
		Run:         f.runClear,
	})
	r.AddCommand(ext.Command{
		ID:          ext.CommandID("compact"),
		Name:        "compact",
		Description: "Summarize the conversation to free up context",
		ArgHint:     "[instructions for the summary]",
		Source:      ext.SourceBuiltin,
		Run:         f.runCompact,
	})
}

// sendCommand sends a slash command line to an engine as a user message, which is how
// the engine runs its headless-capable commands.
func sendCommand(ctx ext.Ctx, engineID, line string) tea.Cmd {
	eng := ctx.Engine(engineID)
	if eng == nil {
		return notice(ctx, "no-engine", "Claude is not running", ext.NoticeWarning)
	}
	return eng.Send(ext.Prompt{Blocks: []proto.ContentBlock{proto.Text(line)}})
}

// runClear asks the engine for a fresh conversation. The engine answers with
// conversation_reset (handled in onConversationReset).
func (f *feature) runClear(ctx ext.Ctx, args string) tea.Cmd {
	return sendCommand(ctx, ext.MainEngine, "/clear")
}

// runCompact passes /compact through; the engine reports status "compacting", then a
// compact boundary (only if it compacted) and a result.
func (f *feature) runCompact(ctx ext.Ctx, args string) tea.Cmd {
	line := "/compact"
	if a := strings.TrimSpace(args); a != "" {
		line += " " + a
	}
	return sendCommand(ctx, ext.MainEngine, line)
}

// onConversationReset: the transcript store empties itself on the same event; the
// screen is redrawn and the session id moves on. The old session stays on disk and is
// remembered so it can be restored.
func (f *feature) onConversationReset(ctx ext.Ctx, engineID string, e *proto.ConversationReset) tea.Cmd {
	st := f.engine(engineID)
	old := st.session
	if old == "" && engineID == ext.MainEngine {
		old = ctx.Session().SessionID
	}
	if old != "" && old != e.NewConversationID {
		f.cleared = append(f.cleared, old)
	}
	// new_conversation_id is not the id the engine continues under (that one arrives
	// with the engine's own session report), so it is neither stored nor published.
	st.session, st.shown = "", "cleared"
	return ctx.Reprint()
}

// lastCleared is the most recent session left by /clear ("" if none).
func (f *feature) lastCleared() string {
	if len(f.cleared) == 0 {
		return ""
	}
	return f.cleared[len(f.cleared)-1]
}
