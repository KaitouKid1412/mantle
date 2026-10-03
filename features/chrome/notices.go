package chrome

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// engineNotice turns the engine's transient system/notification events into host
// notices (the host draws them above the prompt).
func engineNotice(ctx ext.Ctx, m ext.EngineEventMsg) tea.Cmd {
	n, ok := m.Event.(*proto.Notification)
	if !ok || strings.TrimSpace(n.Text) == "" {
		return nil
	}
	key := n.Key
	if key == "" {
		key = n.Text
	}
	src := m.EngineID
	if src == "" {
		src = ext.MainEngine
	}
	return ctx.Notify(ext.Notice{
		Key:     "engine:" + src + ":" + key,
		Text:    n.Text,
		Level:   noticeLevel(n.Priority, n.Color),
		Timeout: time.Duration(n.TimeoutMS) * time.Millisecond,
		Source:  src,
	})
}

func noticeLevel(priority, color string) ext.NoticeLevel {
	switch strings.ToLower(color) {
	case "error", "red":
		return ext.NoticeError
	case "warning", "yellow", "orange":
		return ext.NoticeWarning
	case "success", "green":
		return ext.NoticeSuccess
	}
	switch strings.ToLower(priority) {
	case "high", "immediate", "urgent":
		return ext.NoticeWarning
	}
	return ext.NoticeInfo
}
