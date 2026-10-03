package enginetest

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// describe gives a one-line summary of an engine message for test output.
func describe(m tea.Msg) string {
	switch v := m.(type) {
	case ext.EngineEventMsg:
		env := v.Event.Env()
		s := env.Type
		if env.Subtype != "" {
			s += "/" + env.Subtype
		}
		if se, ok := v.Event.(*proto.StreamEvent); ok {
			s += " " + se.Event.Type
			if se.Event.Delta != nil {
				s += fmt.Sprintf(" %q", se.Event.Delta.Text+se.Event.Delta.PartialJSON+se.Event.Delta.Thinking)
			}
		}
		return s
	case ext.ControlResultMsg:
		return fmt.Sprintf("%s err=%v", v.Subtype, v.Err)
	case ext.PermissionMsg:
		return v.Req.ToolName + " " + v.RequestID
	case ext.ControlRequestMsg:
		return v.Subtype + " " + v.RequestID
	case ext.ControlCancelMsg:
		return v.RequestID
	case ext.EngineExitedMsg:
		return fmt.Sprintf("err=%v", v.Err)
	case ext.SessionChangedMsg:
		return fmt.Sprintf("%+v", v.Info)
	}
	return ""
}

// Events returns the proto events among msgs, in order.
func Events(msgs []tea.Msg) []proto.Event {
	var out []proto.Event
	for _, m := range msgs {
		if ev, ok := m.(ext.EngineEventMsg); ok {
			out = append(out, ev.Event)
		}
	}
	return out
}
