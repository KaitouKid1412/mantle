package input

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/vim"
)

// update handles every message the prompt cares about.
func (s *state) update(c ext.Ctx, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case historyLoadedMsg:
		if m.err != nil {
			c.Log().Warn("input: reading history", "err", m.err)
			return nil
		}
		s.setHistory(m.entries)
		s.invalidate(c)
	case historyAppendedMsg:
		c.Log().Warn("input: writing history", "err", m.err)
	case editor.VimTimeoutMsg:
		s.ed.Update(m)
		return s.changed(c)
	case editor.VimEventMsg:
		return s.vimEvent(c, m.Event)
	case editor.ImagePastedMsg:
		if m.Err != nil {
			return c.Notify(ext.Notice{Key: "input.image", Text: m.Err.Error(), Level: ext.NoticeWarning, Source: FeatureID})
		}
		s.ed.InsertImage(m.Image)
		return s.changed(c)
	case editor.PasteStoreErrMsg:
		c.Log().Warn("input: writing paste cache", "err", m.Err)
	case imagesLoadedMsg:
		return s.imagesLoaded(c, m)
	case fileTickMsg:
		return s.comp.fileTick(c, s, m)
	case bashDoneMsg:
		return s.bashDone(c, m)
	case spellTickMsg:
		return s.spell.tick(s, m)
	case spellResultMsg:
		return s.spell.result(c, s, m)
	case editorDoneMsg:
		return s.externalEditorDone(c, m)
	case ext.SettingsMsg:
		s.applySettings(c)
		return s.changed(c)
	case ext.ThemeChangedMsg:
		s.invalidate(c)
	case ext.SessionChangedMsg:
		if m.EngineID == ext.MainEngine || m.EngineID == "" {
			s.syncSession(m.Info)
		}
	case ext.ControlResultMsg:
		if m.EngineID != ext.MainEngine {
			return nil
		}
		switch m.Subtype {
		case proto.SubInitialize:
			var r proto.InitializeResponse
			if m.Err == nil && json.Unmarshal(m.Resp, &r) == nil && r.Commands != nil {
				s.engineCmds = r.Commands
				return s.commandsCmd()
			}
		case proto.SubCancelAsyncMessage:
			return s.cancelResult(c, m)
		case proto.SubFileSuggestions:
			return s.comp.fileResults(c, s, m)
		case proto.TypeUser:
			if m.Err != nil {
				return c.Notify(ext.Notice{Key: "input.send", Text: "Could not send the prompt: " + m.Err.Error(), Level: ext.NoticeError, Source: FeatureID})
			}
		}
	case ext.EngineEventMsg:
		if m.EngineID == ext.MainEngine {
			return s.engineEvent(c, m.Event)
		}
	case ext.EngineExitedMsg:
		if m.EngineID == ext.MainEngine {
			s.busy = false
			return s.clearQueue()
		}
	}
	return nil
}

func (s *state) engineEvent(c ext.Ctx, ev proto.Event) tea.Cmd {
	switch e := ev.(type) {
	case *proto.SessionStateChanged:
		s.busy = e.State != proto.StateIdle
		if !s.busy {
			return tea.Batch(s.clearQueue(), s.invalidated(c))
		}
	case *proto.Result:
		s.busy = false
		uuids := append([]string{e.UserMessageUUID}, e.UserMessageUUIDs...)
		return tea.Batch(s.dequeue(uuids...), s.invalidated(c))
	case *proto.CommandLifecycle:
		switch e.State {
		case proto.LifecycleStarted:
			s.busy = true
			return s.dequeue(e.CommandUUID)
		case proto.LifecycleCompleted, proto.LifecycleCancelled, proto.LifecycleDiscarded, proto.LifecycleRefused:
			return s.dequeue(e.CommandUUID)
		}
	// Replays (--replay-user-messages) are not used to prune the queue: they
	// may echo a prompt on receipt, before it starts (spike S2).
	case *proto.CommandsChanged:
		s.engineCmds = e.Commands
		return s.commandsCmd()
	case *proto.SystemInit:
		if e.SessionID != "" {
			s.sessionID = e.SessionID
		}
		if s.mergeCommandNames(e.SlashCommands) {
			return s.commandsCmd()
		}
	case *proto.ConversationReset:
		if e.NewConversationID != "" {
			s.sessionID = e.NewConversationID
		}
		return s.clearQueue()
	case *proto.PromptSuggestion:
		if s.cfg.suggestions && s.ed.Empty() && s.mode == modePrompt && e.Suggestion != "" {
			s.suggestion = e.Suggestion
			s.ed.SetGhost(e.Suggestion)
			s.invalidate(c)
		}
	}
	return nil
}

func (s *state) invalidated(c ext.Ctx) tea.Cmd {
	s.invalidate(c)
	return nil
}

// mergeCommandNames adds names from system/init that the full list lacks.
func (s *state) mergeCommandNames(names []string) bool {
	have := map[string]bool{}
	for _, c := range s.engineCmds {
		have[c.Name] = true
	}
	added := false
	for _, n := range names {
		if n != "" && !have[n] {
			s.engineCmds = append(s.engineCmds, proto.SlashCommand{Name: n})
			have[n] = true
			added = true
		}
	}
	return added
}

// commandsCmd publishes the engine's commands to the host registry, so the
// / menu and /help list them. They have no Run: the pipeline sends them to
// the engine as prompt text.
func (s *state) commandsCmd() tea.Cmd {
	cmds := make([]ext.Command, 0, len(s.engineCmds))
	for _, sc := range s.engineCmds {
		cmds = append(cmds, ext.Command{
			ID: ext.CommandID(sc.Name), Name: sc.Name, Description: sc.Description,
			ArgHint: sc.ArgumentHint, Aliases: sc.Aliases, Source: ext.SourceEngine,
		})
	}
	return ext.Msg(ext.CommandsMsg{Source: ext.SourceEngine, EngineID: ext.MainEngine, Commands: cmds})
}

func (s *state) vimEvent(c ext.Ctx, ev vim.Event) tea.Cmd {
	switch ev {
	case vim.EventHistorySearch:
		return s.openSearch(c)
	case vim.EventHistoryPrev:
		if s.hist.AtDraft() {
			s.draft = s.save()
		}
		if e, ok := s.hist.Older(); ok {
			s.loadEntry(e)
			s.ed.SetVimMode(vim.Normal)
			return s.changed(c)
		}
	case vim.EventHistoryNext:
		if _, cmd := s.historyNext(c); cmd != nil {
			s.ed.SetVimMode(vim.Normal)
			return cmd
		}
	}
	return nil
}
