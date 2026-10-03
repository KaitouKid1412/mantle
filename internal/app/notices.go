package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// DefaultNoticeTimeout is how long a notice stays when it doesn't set Timeout.
const DefaultNoticeTimeout = 6 * time.Second

// noticesID is the host's notice component, mounted at the bottom of aboveInput.
const noticesID = "core.notices"

type notice struct {
	n   ext.Notice
	seq int
}

type noticeExpireMsg struct {
	key string
	seq int
}

// addNotice shows a notice, replacing one with the same key.
func (r *Root) addNotice(n ext.Notice) tea.Cmd {
	if n.Text == "" {
		return nil
	}
	r.noticeID++
	seq := r.noticeID
	if n.Key == "" {
		n.Key = "notice:" + itoa(seq)
	}
	replaced := false
	for _, x := range r.notices {
		if x.n.Key == n.Key {
			x.n, x.seq = n, seq
			replaced = true
		}
	}
	if !replaced {
		r.notices = append(r.notices, &notice{n: n, seq: seq})
		if len(r.notices) > 5 {
			r.notices = r.notices[len(r.notices)-5:]
		}
	}
	r.invalidate(noticesID)
	timeout := n.Timeout
	if timeout == 0 {
		timeout = DefaultNoticeTimeout
	}
	if timeout < 0 {
		return nil
	}
	key := n.Key
	return r.opts.Clock.Tick(timeout, func(time.Time) tea.Msg { return noticeExpireMsg{key: key, seq: seq} })
}

func (r *Root) expireNotice(m noticeExpireMsg) {
	for i, x := range r.notices {
		if x.n.Key == m.key && x.seq == m.seq {
			r.notices = append(r.notices[:i], r.notices[i+1:]...)
			r.invalidate(noticesID)
			return
		}
	}
}

// Notices returns the visible notices (for tests).
func (r *Root) Notices() []ext.Notice {
	out := make([]ext.Notice, len(r.notices))
	for i, x := range r.notices {
		out[i] = x.n
	}
	return out
}

func noticeFor(feature, reason string) ext.Notice {
	return ext.Notice{
		Key:     "disabled:" + feature,
		Text:    feature + " was turned off after an error: " + reason,
		Level:   ext.NoticeError,
		Timeout: 15 * time.Second,
		Source:  "core",
	}
}

// noticesComp renders the notice stack.
type noticesComp struct{}

func (c *noticesComp) ID() string                      { return noticesID }
func (c *noticesComp) Init(ext.Ctx) tea.Cmd            { return nil }
func (c *noticesComp) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (c *noticesComp) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	r := rootOf(ctx)
	if r == nil || len(r.notices) == 0 {
		return ext.Rendered{}
	}
	t := ctx.Theme()
	var lines []string
	for _, x := range r.notices {
		tok, mark := theme.Inactive, "·"
		switch x.n.Level {
		case ext.NoticeSuccess:
			tok, mark = theme.Success, "✓"
		case ext.NoticeWarning:
			tok, mark = theme.Warning, "!"
		case ext.NoticeError:
			tok, mark = theme.Error, "✗"
		}
		text := strings.ReplaceAll(x.n.Text, "\n", " ")
		lines = append(lines, clip(t.Paint(tok, mark+" "+text), a.Width))
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
