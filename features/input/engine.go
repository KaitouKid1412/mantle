package input

import (
	"encoding/json"
	"strings"

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
	case queueTakenMsg:
		return s.queueTaken(c, m)
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
	case ext.DialogClosedMsg:
		if m.ID == DialogHistorySearch && s.searchDialog {
			// Closed by the host (layout switch): drop the search.
			s.search, s.searchDialog = nil, false
			return s.changed(c)
		}
	case ext.DialogOpenedMsg:
		if s.echo != "" && !s.echoClear && m.ID != DialogHistorySearch {
			return s.echoItem(c)
		}
	case ext.CommandsMsg, ext.CommandVisibilityMsg:
		// The command list changed: refresh an open / menu.
		return s.refreshSlashMenu(c)
	case ext.EditorSetTextMsg:
		// Rewind (plan 06) puts the rewound prompt back in the box.
		s.setText(m.Text)
		return s.changed(c)
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
		case proto.SubReloadSkills:
			var r proto.ReloadSkillsResponse
			if m.Err == nil && json.Unmarshal(m.Resp, &r) == nil {
				var names []string
				for _, sk := range r.Skills {
					names = append(names, sk.Name)
				}
				s.addSkills(names)
				return s.refreshSlashMenu(c)
			}
		case proto.SubFileSuggestions:
			return s.comp.fileResults(c, s, m)
		case proto.TypeUser:
			if m.Err != nil {
				return s.sendFailed(c, m)
			}
		}
	case ext.EngineEventMsg:
		if m.EngineID == ext.MainEngine {
			return s.engineEvent(c, m.Event)
		}
	case ext.EngineAttachMsg:
		if m.EngineID == ext.MainEngine && m.Engine != nil {
			s.startErr = nil
			s.skillsAsked = false // a new engine: ask again when / opens
			return tea.Batch(warmFiles(m.Engine), s.flushStarting(m.Engine))
		}
	case ext.EngineExitedMsg:
		if m.EngineID == ext.MainEngine {
			s.busy = false
			if m.Err != nil {
				// Startup failed or the engine died: until another
				// attach, prompts stay in the box. So does a prompt
				// the engine had not taken up (its echo goes).
				s.startErr = m.Err
				return tea.Batch(s.failStarting(c, m.Err), s.restoreUnsent(c))
			}
			if len(s.starting) == 0 {
				return s.clearQueue()
			}
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
	case *proto.Assistant:
		if e.ParentToolUseID == "" {
			s.promptTaken()
		}
	case *proto.Result:
		s.busy = false
		s.promptTaken()
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
		var refresh tea.Cmd
		if e.Skills != nil {
			s.addSkills(e.Skills)
			// Bundled skills only show up here, with the first turn:
			// remember them so the next session's first / menu is right.
			_ = c.Store(FeatureID).Set(skillsKey, e.Skills)
			refresh = s.refreshSlashMenu(c)
		}
		if s.mergeCommandNames(e.SlashCommands) {
			return tea.Batch(refresh, s.commandsCmd())
		}
		return refresh
	case *proto.ConversationReset:
		if e.NewConversationID != "" {
			s.sessionID = e.NewConversationID
		}
		var echo tea.Cmd
		if s.echoClear {
			echo = s.echoItem(c) // the new transcript starts with "/clear"
		}
		return tea.Batch(s.clearQueue(), echo)
	case *proto.User:
		if e.IsReplay && e.UUID == s.sentUUID {
			s.promptTaken()
		}
		// A forwarded native command came back as a replay: the engine
		// echoes it, so ours is not needed.
		if e.IsReplay && s.echo != "" && !s.echoClear && strings.TrimSpace(e.Message.Content.PlainText()) == s.echo {
			s.echo = ""
		}
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
			Hidden: headlessOnly[sc.Name] || strings.HasPrefix(sc.Name, "__"),
		})
	}
	return ext.Msg(ext.CommandsMsg{Source: ext.SourceEngine, EngineID: ext.MainEngine, Commands: cmds})
}

const skillsKey = "skills"

// refreshSlashMenu recomputes an open / menu after the command list or the
// skill set changed.
func (s *state) refreshSlashMenu(c ext.Ctx) tea.Cmd {
	if s.comp.kind != compSlash && s.comp.kind != compArgs {
		return nil
	}
	cmd := s.comp.update(c, s)
	s.invalidate(c)
	return cmd
}

// askSkills asks the engine for its skills once, when the / menu first
// opens: system/init (which lists them) only arrives with the first turn.
// Lazily, so startup sends nothing extra.
func (s *state) askSkills(c ext.Ctx) tea.Cmd {
	if s.skillsAsked {
		return nil
	}
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		return nil
	}
	s.skillsAsked = true
	return eng.Control(proto.SubReloadSkills, proto.ReloadSkillsRequest{})
}

// addSkills records engine skill names (left out of the unfiltered menu).
func (s *state) addSkills(names []string) {
	if s.skills == nil {
		s.skills = map[string]bool{}
	}
	for _, n := range names {
		s.skills[n] = true
	}
}

// headlessOnly are engine commands the interactive claude keeps out of its
// / menu (2.1.289/2.1.290): the headless variant of auto-mode-setup is for SDK
// hosts, /agents is hidden ("removed"), heapdump is a debugging aid and
// workflow-launch-exec serves server-launched workflow sessions. They still run
// when typed.
var headlessOnly = map[string]bool{
	"auto-mode-setup": true, "agents": true, "heapdump": true, "workflow-launch-exec": true,
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
