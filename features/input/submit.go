package input

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
)

// pendingSubmit is a submit waiting for dropped image files to load.
type pendingSubmit struct {
	priority string
}

// imagesLoadedMsg carries images loaded from dropped paths.
type imagesLoadedMsg struct {
	images map[*editor.Chip]*editor.Image
	errs   []error
}

// queued is a prompt sent while a turn runs that the engine has not started.
type queued struct {
	uuid, text, priority string
	draft                *savedDraft
}

// submitAction handles chat:submit, chat:sendNow and chat:queueSubmit.
func (s *state) submitAction(c ext.Ctx, priority string) (bool, tea.Cmd) {
	if s.pending != nil {
		return true, nil
	}
	if s.comp.open() && priority == "" {
		kind := s.comp.kind
		cmd := s.comp.accept(c, s, true)
		if kind != compSlash {
			return true, cmd
		}
		// A command picked from the menu runs right away.
		return true, tea.Batch(cmd, s.trySubmit(c, priority))
	}
	return true, s.trySubmit(c, priority)
}

func (s *state) trySubmit(c ext.Ctx, priority string) tea.Cmd {
	if s.ed.BackslashNewline() {
		return s.changed(c)
	}
	if strings.TrimSpace(s.ed.Text()) == "" && len(s.ed.Chips()) == 0 {
		return nil
	}
	if n := s.ed.StripInvisible(); n > 0 {
		return tea.Batch(s.changed(c), c.Notify(ext.Notice{
			Key:    "input.invisible",
			Text:   fmt.Sprintf("Removed %d invisible %s from the prompt. Press Enter again to send.", n, plural(n, "character", "characters")),
			Level:  ext.NoticeWarning,
			Source: FeatureID,
		}))
	}
	var load []*editor.Chip
	for _, ch := range s.ed.Chips() {
		if ch.Kind == editor.ChipImage && !ch.Image.Loaded() && ch.Image != nil && ch.Image.Path != "" {
			load = append(load, ch)
		}
	}
	if len(load) > 0 {
		s.pending = &pendingSubmit{priority: priority}
		return loadImages(load)
	}
	return s.finishSubmit(c, priority)
}

func loadImages(chips []*editor.Chip) tea.Cmd {
	paths := make(map[*editor.Chip]string, len(chips))
	for _, ch := range chips {
		paths[ch] = ch.Image.Path
	}
	return func() tea.Msg {
		out := imagesLoadedMsg{images: map[*editor.Chip]*editor.Image{}}
		for ch, p := range paths {
			im, err := editor.LoadImageFile(p)
			if err != nil {
				out.errs = append(out.errs, err)
				continue
			}
			out.images[ch] = im
		}
		return out
	}
}

func (s *state) imagesLoaded(c ext.Ctx, m imagesLoadedMsg) tea.Cmd {
	p := s.pending
	s.pending = nil
	if p == nil {
		return nil
	}
	if len(m.errs) > 0 {
		return c.Notify(ext.Notice{Key: "input.image", Text: "Could not attach image: " + m.errs[0].Error(), Level: ext.NoticeError, Source: FeatureID})
	}
	for ch, im := range m.images {
		ch.Image = im // chips are only read on the UI goroutine
	}
	return s.finishSubmit(c, p.priority)
}

// finishSubmit hands the prompt to the pipeline and resets the editor.
func (s *state) finishSubmit(c ext.Ctx, priority string) tea.Cmd {
	d := s.buildDraft(priority)
	entry := s.historyEntry()
	s.lastSent = s.save()
	s.pendingHist = &entry
	s.clearEditor()
	s.hist.Reset()
	s.draft = nil
	s.suggestion = ""
	cmd := c.Submit(d)
	var restore tea.Cmd
	if s.stash != nil && s.ed.Empty() {
		restore = s.popStash(c)
	}
	return tea.Batch(cmd, restore, s.changed(c))
}

func (s *state) buildDraft(priority string) ext.Draft {
	d := ext.Draft{Text: s.ed.Text(), Mode: s.mode, Priority: priority}
	for _, ch := range s.ed.Chips() {
		switch ch.Kind {
		case editor.ChipImage:
			a := ext.Attachment{Kind: "image", Name: ch.Label()}
			if ch.Image != nil {
				a.Path, a.MediaType, a.Data = ch.Image.Path, ch.Image.MediaType, ch.Image.Data
			}
			d.Attachments = append(d.Attachments, a)
		case editor.ChipPaste:
			d.Attachments = append(d.Attachments, ext.Attachment{Kind: "paste", Name: ch.Label(), Text: ch.Text})
		}
	}
	return d
}

// ---- prompt stages ----

// stageHistory records the prompt in history.jsonl.
func (s *state) stageHistory(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	if s.pendingHist == nil {
		return ext.Continue, nil
	}
	e := *s.pendingHist
	s.pendingHist = nil
	return ext.Continue, s.appendHistory(e)
}

// stageSlash runs native and mod commands. Engine commands, and names no
// command claims, continue to the engine as prompt text (the engine runs
// them or hands unknown names to the model).
func (s *state) stageSlash(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	if d.Mode != modePrompt || !strings.HasPrefix(d.Text, "/") {
		return ext.Continue, nil
	}
	name, args := splitCommand(d.Text)
	if name == "" {
		return ext.Continue, nil
	}
	cmd, ok := c.Command(name)
	if !ok || cmd.Run == nil || cmd.Source == ext.SourceEngine {
		return ext.Continue, nil
	}
	return ext.Consumed, cmd.Run(c, args)
}

// splitCommand splits "/name args" into its parts.
func splitCommand(text string) (name, args string) {
	t := strings.TrimPrefix(text, "/")
	i := strings.IndexAny(t, " \t\n")
	if i < 0 {
		return t, ""
	}
	return t[:i], strings.TrimSpace(t[i+1:])
}

// stageBash runs "!" prompts as shell commands.
func (s *state) stageBash(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	if d.Mode != modeBash {
		return ext.Continue, nil
	}
	return ext.Consumed, s.runBash(c, d.Text)
}

// stageAttachments drops images that have no data (they failed to load).
func (s *state) stageAttachments(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	var keep []ext.Attachment
	var cmd tea.Cmd
	for _, a := range d.Attachments {
		if a.Kind == "image" && len(a.Data) == 0 {
			cmd = c.Notify(ext.Notice{Key: "input.image", Text: a.Name + " has no image data and was left out", Level: ext.NoticeWarning, Source: FeatureID})
			continue
		}
		keep = append(keep, a)
	}
	d.Attachments = keep
	return ext.Continue, cmd
}

// stagePriority queues prompts submitted while a turn runs.
func (s *state) stagePriority(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	switch {
	case !s.busy:
		d.Priority = ""
	case d.Priority == "":
		d.Priority = proto.PriorityLater
	}
	return ext.Continue, nil
}

// stageSend writes the prompt to the main engine.
func (s *state) stageSend(c ext.Ctx, d *ext.Draft) (ext.Verdict, tea.Cmd) {
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		var restore tea.Cmd
		if s.ed.Empty() && s.lastSent != nil {
			s.restore(s.lastSent)
			restore = s.changed(c)
		}
		return ext.Reject, tea.Batch(restore, c.Notify(ext.Notice{Key: "input.send", Text: "Claude is not running yet; the prompt was kept", Level: ext.NoticeWarning, Source: FeatureID}))
	}
	p := ext.Prompt{Blocks: draftBlocks(*d), Priority: d.Priority, UUID: uuid.NewString()}
	// Paste chips are expanded in place; tell the engine which text was
	// pasted (inline_pastes, one entry per paste).
	for _, a := range d.Attachments {
		if a.Kind == "paste" && strings.TrimSpace(a.Text) != "" {
			p.InlinePastes = append(p.InlinePastes, a.Text)
		}
	}
	return ext.Consumed, tea.Batch(s.track(c, p, d.Text), eng.Send(p))
}

// draftBlocks turns a draft into content blocks: images first, then text.
func draftBlocks(d ext.Draft) []proto.ContentBlock {
	var blocks []proto.ContentBlock
	for _, a := range d.Attachments {
		if a.Kind == "image" && len(a.Data) > 0 {
			im := &editor.Image{MediaType: a.MediaType, Data: a.Data}
			blocks = append(blocks, proto.Image(a.MediaType, im.Base64()))
		}
	}
	if strings.TrimSpace(d.Text) != "" {
		blocks = append(blocks, proto.Text(d.Text))
	}
	return blocks
}

// track records a sent prompt: queued when a turn runs, else the engine is
// now busy.
func (s *state) track(c ext.Ctx, p ext.Prompt, text string) tea.Cmd {
	if !s.busy || p.Priority == "" || p.Priority == proto.PriorityNow {
		s.busy = true
		return nil
	}
	q := queued{uuid: p.UUID, text: text, priority: p.Priority, draft: s.lastSent}
	if s.lastSent != nil {
		q.text = s.lastSent.Display
	}
	s.queue = append(s.queue, q)
	return s.queueCmd()
}

func (s *state) queueCmd() tea.Cmd {
	ps := make([]ext.QueuedPrompt, len(s.queue))
	for i, q := range s.queue {
		ps[i] = ext.QueuedPrompt{UUID: q.uuid, Text: q.text, Priority: q.priority}
	}
	return ext.Msg(ext.QueuedPromptsMsg{EngineID: ext.MainEngine, Prompts: ps})
}

// dequeue drops queued prompts the engine started or dropped.
func (s *state) dequeue(uuids ...string) tea.Cmd {
	n := len(s.queue)
	s.queue = slicesDeleteFunc(s.queue, func(q queued) bool {
		for _, u := range uuids {
			if q.uuid == u {
				return true
			}
		}
		return false
	})
	if len(s.queue) == n {
		return nil
	}
	return s.queueCmd()
}

func (s *state) clearQueue() tea.Cmd {
	if len(s.queue) == 0 {
		return nil
	}
	s.queue = nil
	return s.queueCmd()
}

// takeBackQueue pulls queued prompts back into the editor (up arrow on an
// empty prompt) and asks the engine to drop them.
func (s *state) takeBackQueue(c ext.Ctx) tea.Cmd {
	eng := c.Engine(ext.MainEngine)
	var cmds []tea.Cmd
	var texts []string
	var chips []*editor.Chip
	mode := modePrompt
	for _, q := range s.queue {
		if eng != nil {
			cmds = append(cmds, eng.Control(proto.SubCancelAsyncMessage, proto.CancelAsyncMessageRequest{MessageUUID: q.uuid}))
		}
		if q.draft != nil {
			texts = append(texts, q.draft.Display)
			chips = append(chips, q.draft.chips...)
			if q.draft.Mode == modeBash {
				mode = modeBash
			}
		} else {
			texts = append(texts, q.text)
		}
	}
	s.queue = nil
	s.ed.SetValueWithChips(strings.Join(texts, "\n"), chips)
	s.mode = mode
	cmds = append(cmds, s.queueCmd(), s.changed(c))
	return tea.Batch(cmds...)
}

func (s *state) cancelResult(c ext.Ctx, m ext.ControlResultMsg) tea.Cmd {
	var r proto.CancelAsyncMessageResponse
	if m.Err == nil && json.Unmarshal(m.Resp, &r) == nil && r.Cancelled {
		return nil
	}
	return c.Notify(ext.Notice{Key: "input.takeback", Text: "A queued message had already been sent", Level: ext.NoticeWarning, Source: FeatureID})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func slicesDeleteFunc[T any](s []T, del func(T) bool) []T {
	out := s[:0]
	for _, v := range s {
		if !del(v) {
			out = append(out, v)
		}
	}
	return out
}
