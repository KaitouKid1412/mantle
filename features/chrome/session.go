package chrome

import (
	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// sessionState is what chrome components know about the main session and the editor.
// Each component keeps its own copy and feeds it from Update (UI goroutine only), so a
// mod that replaces one component doesn't break the others.
type sessionState struct {
	Mode        string // permission mode
	SessionID   string
	Cwd         string
	ProjectDir  string // the first cwd seen: where the session started
	Title       string // session name (SessionInfo: /rename, -n)
	AITitle     string // engine-generated title (session_title_changed)
	Model       string
	Version     string // engine version
	OutputStyle string
	Effort      string
	FastMode    string // init fast_mode_state
	State       string // idle | running | requires_action
	Background  int    // background tasks (background_tasks_changed)

	EditorMode  string // "prompt" | "bash"
	Vim         string
	EditorEmpty bool
}

func newSessionState() sessionState { return sessionState{EditorEmpty: true} }

// isMain reports whether an engine ID is the main session.
func isMain(engineID string) bool { return engineID == "" || engineID == ext.MainEngine }

// observe folds msg into the state and reports whether anything changed.
func (s *sessionState) observe(msg tea.Msg) bool {
	before := *s
	switch m := msg.(type) {
	case ext.SessionChangedMsg:
		if !isMain(m.EngineID) {
			return false
		}
		i := m.Info
		set(&s.SessionID, i.SessionID)
		set(&s.Cwd, i.Cwd)
		set(&s.Title, i.Title)
		set(&s.Model, i.Model)
		set(&s.Mode, i.PermissionMode)
		set(&s.Version, i.ClaudeVersion)
		set(&s.OutputStyle, i.OutputStyle)
	case ext.EditorStateMsg:
		s.EditorMode, s.Vim, s.EditorEmpty = m.Mode, m.Vim, m.Empty
	case ext.EngineEventMsg:
		if !isMain(m.EngineID) {
			return false
		}
		s.observeEvent(m.Event)
	default:
		return false
	}
	if s.ProjectDir == "" {
		s.ProjectDir = s.Cwd
	}
	return *s != before
}

func (s *sessionState) observeEvent(ev proto.Event) {
	switch e := ev.(type) {
	case *proto.SystemInit:
		set(&s.SessionID, e.SessionID)
		set(&s.Cwd, e.CWD)
		set(&s.Model, e.Model)
		set(&s.Mode, e.PermissionMode)
		set(&s.Version, e.ClaudeCodeVersion)
		set(&s.OutputStyle, e.OutputStyle)
		s.Effort = e.Effort
		s.FastMode = e.FastModeState
	case *proto.Status:
		set(&s.Mode, e.PermissionMode)
	case *proto.SessionStateChanged:
		s.State = e.State
	case *proto.BackgroundTasksChanged:
		n := 0
		for _, t := range e.Tasks {
			if !t.Ambient {
				n++
			}
		}
		s.Background = n
	case *proto.SessionTitleChanged:
		set(&s.AITitle, e.Title)
	case *proto.ConversationReset:
		set(&s.SessionID, e.NewConversationID)
		s.Background = 0
	}
}

// Name is the session's display name: the custom name, else the generated title.
func (s *sessionState) Name() string {
	if s.Title != "" {
		return s.Title
	}
	return s.AITitle
}

// set assigns v when it is non-empty: engines omit fields they don't know.
func set(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
