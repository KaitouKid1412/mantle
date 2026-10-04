package turn

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Exit hints. Claude Code shows the same advice on a first ctrl+c / ctrl+d.
const (
	hintCtrlC = "Press Ctrl-C again to exit"
	hintCtrlD = "Press Ctrl-D again to exit"
)

// vimQuits are typed exits that are not slash commands.
var vimQuits = map[string]bool{":q": true, ":q!": true, ":wq": true, ":wq!": true}

func (st *state) setupControl(r ext.Registrar) {
	r.AddAction(ext.Action{
		ID: ext.ActChatCancel, Context: ext.ContextChat,
		Description: "Interrupt Claude (keeps the work so far)",
		Run:         st.cancelAction,
	})
	// The host's core feature owns app:interrupt and app:exit (press twice to exit);
	// the turn feature gives them their Claude Code meaning on top.
	r.Wrap(string(ext.ActAppInterrupt), func(next ext.ActionFunc) ext.ActionFunc {
		return func(c ext.Ctx) (bool, tea.Cmd) { return st.interruptAction(c) }
	})
	r.Wrap(string(ext.ActAppExit), func(next ext.ActionFunc) ext.ActionFunc {
		return func(c ext.Ctx) (bool, tea.Cmd) { return true, st.doublePress(c, ext.ActAppExit, hintCtrlD) }
	})
	r.AddAction(ext.Action{
		ID: ext.ActTaskBackground, Context: ext.ContextTask,
		Description: "Run the current task in the background",
		Run:         st.backgroundAction,
	})
	r.AddAction(ext.Action{
		ID: ext.ActChatKillAgents, Context: ext.ContextChat,
		Description: "Stop all background agents",
		Run:         st.killAgentsAction,
	})
	r.AddCommand(ext.Command{
		Name: "exit", Aliases: []string{"quit"}, Source: ext.SourceBuiltin,
		Description: "Exit mantle",
		Run:         func(c ext.Ctx, _ string) tea.Cmd { return st.exit(c, "/exit") },
	})
	r.AddPromptStage("turn.quit", 5, func(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
		if d.Mode != "bash" && len(d.Attachments) == 0 && vimQuits[strings.TrimSpace(d.Text)] {
			return ext.Consumed, st.exit(c, strings.TrimSpace(d.Text))
		}
		return ext.Continue, nil
	})
}

// running reports whether a turn is in progress on an engine.
func (st *state) isRunning(engineID string) bool { return st.running[engineKey(engineID)] }

func (st *state) setRunning(c ext.Ctx, engineID string, on bool) {
	engineID = engineKey(engineID)
	if st.running[engineID] == on {
		return
	}
	st.running[engineID] = on
	if engineID == ext.MainEngine {
		c.SetContextActive(ext.ContextTask, on)
	}
}

// onEngineEvent tracks turns, tasks and subagents, and turns some events into notices.
func (st *state) onEngineEvent(c ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	eng := engineKey(m.EngineID)
	switch ev := m.Event.(type) {
	case *proto.SessionStateChanged:
		st.stateEv[eng] = true
		st.setRunning(c, eng, ev.State == proto.StateRunning || ev.State == proto.StateRequiresAction)
	case *proto.SystemInit:
		st.setRunning(c, eng, true)
	case *proto.Result:
		// With session-state events, idle (which also covers background work) ends
		// the turn; without them, the last result does.
		if !st.stateEv[eng] && ev.QueuedTurnCount == 0 {
			st.setRunning(c, eng, false)
		}
	case *proto.TaskStarted:
		label := ev.SubagentType
		if label == "" {
			label = ev.Description
		}
		st.agents[ev.TaskID] = label
	case *proto.BackgroundTasksChanged:
		st.tasks[eng] = ev.Tasks
	case *proto.TaskNotification:
		kept := st.tasks[eng][:0]
		for _, t := range st.tasks[eng] {
			if t.TaskID != ev.TaskID {
				kept = append(kept, t)
			}
		}
		st.tasks[eng] = kept
	case *proto.PermissionDenied:
		return st.deniedNotice(c, eng, ev)
	case *proto.RateLimitEvent:
		return st.onRateLimit(c, eng, ev)
	case *proto.CommandLifecycle:
		return st.onLifecycle(c, eng, ev)
	}
	return nil
}

func (st *state) deniedNotice(c ext.Ctx, engineID string, ev *proto.PermissionDenied) tea.Cmd {
	text := ev.ToolName + " was denied"
	if msg := strings.TrimSpace(ev.Message); msg != "" {
		text += ": " + msg
	} else if ev.DecisionReason != "" {
		text += ": " + ev.DecisionReason
	}
	if who := st.agentLabel(ev.AgentID); who != "" {
		text = who + ": " + text
	}
	if l := engineLabel(engineID); l != "" {
		text = l + ": " + text
	}
	return c.Notify(ext.Notice{Key: "turn.denied." + ev.ToolUseID, Text: text, Level: ext.NoticeWarning, Source: FeatureID})
}

// cancelAction is esc in the prompt: interrupt a running turn (or a usage-limit wait).
// Otherwise it declines, so the editor's own esc handling applies.
func (st *state) cancelAction(c ext.Ctx) (bool, tea.Cmd) {
	if st.limit != nil {
		return true, st.cancelLimitWait(c)
	}
	eng := c.Engine(ext.MainEngine)
	if eng == nil || !st.isRunning(ext.MainEngine) {
		return false, nil
	}
	return true, eng.Interrupt(false)
}

// interruptAction is ctrl+c once the editor declined it (empty prompt): interrupt a
// running turn and drop its queued messages (esc keeps them; they send next), else
// exit on a second press.
func (st *state) interruptAction(c ext.Ctx) (bool, tea.Cmd) {
	if eng := c.Engine(ext.MainEngine); eng != nil && st.isRunning(ext.MainEngine) {
		delete(st.presses, ext.ActAppInterrupt)
		return true, eng.Interrupt(true)
	}
	if st.limit != nil {
		return true, st.cancelLimitWait(c)
	}
	return true, st.doublePress(c, ext.ActAppInterrupt, hintCtrlC)
}

// doublePress exits on the second press within doublePressWindow and shows hint after
// the first.
func (st *state) doublePress(c ext.Ctx, id ext.ActionID, hint string) tea.Cmd {
	now := c.Clock().Now()
	if last, ok := st.presses[id]; ok && now.Sub(last) <= doublePressWindow {
		delete(st.presses, id)
		return st.exit(c, string(id))
	}
	st.presses[id] = now
	return c.Notify(ext.Notice{Key: "turn.exitHint", Text: hint, Level: ext.NoticeInfo, Timeout: doublePressWindow, Source: FeatureID})
}

// exit prints the resume hint and quits. The engine manager stops engines gracefully
// (end_session, then EOF and signals) when the program ends.
func (st *state) exit(c ext.Ctx, reason string) tea.Cmd {
	var cmds []tea.Cmd
	if id := c.Session().SessionID; id != "" {
		cmds = append(cmds, c.Print("Resume this session with: mantle --resume "+id))
	}
	cmds = append(cmds, ext.Msg(ext.ExitMsg{Code: 0, Reason: reason}))
	return tea.Sequence(cmds...)
}

// backgroundAction is ctrl+b while a turn runs (task:background).
func (st *state) backgroundAction(c ext.Ctx) (bool, tea.Cmd) {
	eng := c.Engine(ext.MainEngine)
	if eng == nil || !st.isRunning(ext.MainEngine) {
		return false, nil
	}
	if !eng.Supports(proto.SubBackgroundTasks) {
		return true, c.Notify(ext.Notice{Key: "turn.background", Text: "This claude version can't move work to the background.", Level: ext.NoticeWarning, Source: FeatureID})
	}
	return true, eng.Control(proto.SubBackgroundTasks, proto.BackgroundTasksRequest{})
}

// killAgentsAction is ctrl+x ctrl+k: stop every background agent.
func (st *state) killAgentsAction(c ext.Ctx) (bool, tea.Cmd) {
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		return false, nil
	}
	var cmds []tea.Cmd
	for _, t := range st.tasks[ext.MainEngine] {
		if isAgentTask(t) {
			cmds = append(cmds, eng.Control(proto.SubStopTask, proto.StopTaskRequest{TaskID: t.TaskID}))
		}
	}
	if len(cmds) == 0 {
		return true, c.Notify(ext.Notice{Key: "turn.killAgents", Text: "No background agents are running.", Level: ext.NoticeInfo, Source: FeatureID})
	}
	text := "Stopping 1 background agent"
	if len(cmds) > 1 {
		text = fmt.Sprintf("Stopping %d background agents", len(cmds))
	}
	cmds = append(cmds, c.Notify(ext.Notice{Key: "turn.killAgents", Text: text, Level: ext.NoticeInfo, Source: FeatureID}))
	return true, tea.Batch(cmds...)
}

// isAgentTask reports whether a background task is a subagent (not a shell command).
func isAgentTask(t proto.BackgroundTask) bool {
	return t.TaskType == "" || strings.Contains(strings.ToLower(t.TaskType), "agent")
}

// onControlResult reacts to answers to control requests this feature (or anyone) sent.
func (st *state) onControlResult(c ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	eng := engineKey(m.EngineID)
	switch m.Subtype {
	case proto.SubSetPermissionMode:
		return st.onModeResult(c, m)
	case proto.SubListModels:
		var resp proto.ModelsResponse
		if m.Err == nil {
			_ = jsonUnmarshal(m.Resp, &resp)
		}
		st.setModels(c, eng, resp.Models)
		return st.maybeStartupMode(c, eng)
	case proto.SubInitialize:
		// The handshake finished: its models say whether auto mode is available (none, or
		// a failed handshake, means it isn't). Now the startup mode can be applied.
		var resp proto.InitializeResponse
		if m.Err == nil {
			_ = jsonUnmarshal(m.Resp, &resp)
		}
		st.setModels(c, eng, resp.Models)
		return st.maybeStartupMode(c, eng)
	case proto.SubStopTask, proto.SubBackgroundTasks, proto.SubInterrupt:
		if m.Err != nil {
			return c.Notify(ext.Notice{Key: "turn." + m.Subtype, Text: m.Subtype + " failed: " + m.Err.Error(), Level: ext.NoticeWarning, Source: FeatureID})
		}
	}
	return nil
}
