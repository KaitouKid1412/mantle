package fullscreen

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// scrollSpeedKey is the mantle setting for wheel lines per notch.
const scrollSpeedKey = "fullscreen.scrollSpeed"

const (
	defaultScrollSpeed = 3
	maxScrollSpeed     = 20
)

// scrollSpeed is lines per wheel notch: CLAUDE_CODE_SCROLL_SPEED, else the mantle
// setting, else 3.
func scrollSpeed(s ext.Settings, env terminal.Env) int {
	if n, err := strconv.Atoi(strings.TrimSpace(terminal.Get(env, "CLAUDE_CODE_SCROLL_SPEED"))); err == nil && n > 0 {
		return min(n, maxScrollSpeed)
	}
	switch v := s.Mantle(scrollSpeedKey).(type) {
	case float64:
		return max(1, min(int(v), maxScrollSpeed))
	case int:
		return max(1, min(v, maxScrollSpeed))
	}
	return defaultScrollSpeed
}

// scrollSpeedCommand is /scroll-speed [1-20]: without an argument it reports the
// current speed.
func scrollSpeedCommand(env terminal.Env) ext.CommandFunc {
	return func(ctx ext.Ctx, args string) tea.Cmd {
		args = strings.TrimSpace(args)
		if args == "" {
			return ctx.Notify(ext.Notice{Key: "fullscreen.scrollSpeed",
				Text: fmt.Sprintf("Wheel scroll speed: %d lines per notch (/scroll-speed 1–%d to change)",
					scrollSpeed(ctx.Settings(), env), maxScrollSpeed),
				Source: ViewportID})
		}
		n, err := strconv.Atoi(args)
		if err != nil || n < 1 || n > maxScrollSpeed {
			return ctx.Notify(ext.Notice{Key: "fullscreen.scrollSpeed",
				Text:  fmt.Sprintf("Scroll speed must be a number from 1 to %d", maxScrollSpeed),
				Level: ext.NoticeWarning, Source: ViewportID})
		}
		cmds := []tea.Cmd{ctx.Settings().SetMantle(scrollSpeedKey, n),
			ctx.Notify(ext.Notice{Key: "fullscreen.scrollSpeed",
				Text: fmt.Sprintf("Wheel scroll speed set to %d lines per notch", n), Source: ViewportID})}
		if terminal.Get(env, "CLAUDE_CODE_SCROLL_SPEED") != "" {
			cmds = append(cmds, ctx.Notify(ext.Notice{Key: "fullscreen.scrollSpeedEnv",
				Text:  "CLAUDE_CODE_SCROLL_SPEED is set and takes precedence",
				Level: ext.NoticeWarning, Source: ViewportID}))
		}
		return tea.Batch(cmds...)
	}
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
