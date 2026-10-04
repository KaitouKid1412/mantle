package turn

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

type queuedPrompt = ext.QueuedPrompt

// maxQueuedShown caps the queue display; the rest collapse into "+N more".
const maxQueuedShown = 3

func (st *state) setupQueue(r ext.Registrar) {
	r.AddComponent(ext.SlotAboveInput, &queueView{st: st}, ext.SlotOpts{Weight: 50, MaxHeight: maxQueuedShown + 2})
	ext.Subscribe(r, "turn.queued", func(c ext.Ctx, m ext.QueuedPromptsMsg) tea.Cmd {
		st.queued[engineKey(m.EngineID)] = m.Prompts
		c.Invalidate(QueueComponentID)
		return nil
	})
}

// onLifecycle hides prompts the engine has started or dropped, until the input feature
// sends its next list.
func (st *state) onLifecycle(c ext.Ctx, engineID string, ev *proto.CommandLifecycle) tea.Cmd {
	if ev.State == proto.LifecycleQueued {
		return nil
	}
	list := st.queued[engineID]
	kept := list[:0:0]
	for _, p := range list {
		if p.UUID != ev.CommandUUID {
			kept = append(kept, p)
		}
	}
	if len(kept) != len(list) {
		st.queued[engineID] = kept
		c.Invalidate(QueueComponentID)
	}
	return nil
}

// queueView shows prompts queued while Claude works, dimmed above the input.
type queueView struct{ st *state }

func (q *queueView) ID() string                      { return QueueComponentID }
func (q *queueView) Init(ext.Ctx) tea.Cmd            { return nil }
func (q *queueView) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (q *queueView) View(c ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: renderQueue(q.st.queued[ext.MainEngine], a.Width, c.Theme())}
}

func renderQueue(prompts []queuedPrompt, width int, t *theme.Theme) string {
	if len(prompts) == 0 || width < 8 {
		return ""
	}
	dim := func(s string) string { return s }
	if t != nil {
		dim = func(s string) string { return t.Paint(theme.Inactive, s) }
	}
	var lines []string
	for i, p := range prompts {
		if i == maxQueuedShown {
			lines = append(lines, dim("  … +"+itoa(len(prompts)-i)+" more"))
			break
		}
		text := strings.TrimSpace(dialogs.SanitizeLine(p.Text))
		lines = append(lines, dim(ansi.Truncate("  › "+text, width, "…")))
	}
	lines = append(lines, dim(ansi.Truncate("  ↑ to edit queued messages", width, "…")))
	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
