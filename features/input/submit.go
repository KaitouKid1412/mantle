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
		if s.comp.selectedIsTyped() {
			// Nothing left to complete: Enter submits what is typed.
			s.comp.close()
			return true, s.trySubmit(c, priority)
		}
		cmd := s.comp.accept(c, s, true)
		if kind != compSlash && kind != compArgs {
			return true, cmd // @ paths, emoji and ! paths only insert
		}
		// A command or argument picked from the menu runs right away.
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
	// Echo the command into the transcript first, as the engine echoes the
	// prompts it runs; panels then print their result line under it.
	return ext.Consumed, tea.Sequence(echoPrompt(c, d.Text), cmd.Run(c, args))
}

// echoPrompt prints text as a user prompt, with the transcript's own
// renderer.
func echoPrompt(c ext.Ctx, text string) tea.Cmd {
	w, _ := c.Size()
	r := c.Renderer(ext.KeyUserPrompt)
	if r == nil || w <= 0 {
		return nil
	}
	blk := r(ext.RenderCtx{Width: w, Theme: c.Theme(), Now: c.Clock().Now()},
		&ext.Item{ID: "input:echo", Key: ext.KeyUserPrompt, Data: strings.TrimSpace(text), State: ext.Done, End: c.Clock().Now()})
	if len(blk.Lines) == 0 {
		return nil
	}
	return c.Print(strings.Join(blk.Lines, "\n"))
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
	if c.Engine(ext.MainEngine) == nil && s.startErr != nil {
		// Claude failed to start: keep the prompt in the box.
		var restore tea.Cmd
		if s.ed.Empty() && s.lastSent != nil {
			s.restore(s.lastSent)
			restore = s.changed(c)
		}
		return ext.Reject, tea.Batch(restore, c.Notify(ext.Notice{Key: "input.send", Text: "Claude is not running (" + s.startErr.Error() + "); the prompt was kept", Level: ext.NoticeError, Source: FeatureID}))
	}
	p := ext.Prompt{Blocks: draftBlocks(*d), Priority: d.Priority, UUID: uuid.NewString()}
	// Paste chips are expanded in place; tell the engine which text was
	// pasted (inline_pastes, one entry per paste).
	for _, a := range d.Attachments {
		if a.Kind == "paste" && strings.TrimSpace(a.Text) != "" {
			p.InlinePastes = append(p.InlinePastes, a.Text)
		}
	}
	return ext.Consumed, s.dispatch(c, p, d.Text, s.lastSent)
}

// startPending is a prompt sent before the main engine attached.
type startPending struct {
	p     ext.Prompt
	text  string
	draft *savedDraft
}

// dispatch sends a prompt to the main engine. While the engine is still
// starting (startup gates, version check, spawn) the prompt waits, shown
// as queued, and goes out in order when the engine attaches, as Claude Code
// holds prompts typed during startup.
func (s *state) dispatch(c ext.Ctx, p ext.Prompt, text string, draft *savedDraft) tea.Cmd {
	eng := c.Engine(ext.MainEngine)
	if eng == nil {
		s.starting = append(s.starting, startPending{p: p, text: text, draft: draft})
		if draft != nil {
			text = draft.Display
		}
		s.queue = append(s.queue, queued{uuid: p.UUID, text: text, priority: p.Priority, draft: draft})
		return s.queueCmd()
	}
	if p.ShouldQuery != nil && !*p.ShouldQuery && !s.busy {
		return eng.Send(p) // recorded without a turn
	}
	return tea.Batch(s.track(c, p, text), eng.Send(p))
}

// flushStarting sends the prompts held during startup, in order: the first
// runs now, the rest queue behind it.
func (s *state) flushStarting(eng ext.Engine) tea.Cmd {
	pend := s.starting
	s.starting = nil
	var cmds []tea.Cmd
	for _, sp := range pend {
		p := sp.p
		noTurn := p.ShouldQuery != nil && !*p.ShouldQuery
		switch {
		case noTurn && !s.busy:
			s.dequeue(p.UUID)
		case !s.busy:
			p.Priority = ""
			s.busy = true
			s.dequeue(p.UUID) // it starts now
		default:
			p.Priority = proto.PriorityLater // stays in the queue until it starts
		}
		cmds = append(cmds, eng.Send(p))
	}
	if len(pend) > 0 {
		cmds = append(cmds, s.queueCmd())
	}
	// Engine.Send writes inside its Cmd, and tea.Batch runs Cmds on separate
	// goroutines: Sequence keeps the prompts in submit order.
	return tea.Sequence(cmds...)
}

// failStarting gives prompts held during startup back to the editor when
// Claude could not start.
func (s *state) failStarting(c ext.Ctx, err error) tea.Cmd {
	pend := s.starting
	s.starting = nil
	if len(pend) == 0 {
		return nil
	}
	var texts []string
	var chips []*editor.Chip
	for _, sp := range pend {
		if sp.draft != nil {
			texts = append(texts, sp.draft.Display)
			chips = append(chips, sp.draft.chips...)
		} else {
			texts = append(texts, sp.text)
		}
	}
	if !s.ed.Empty() {
		texts = append(texts, s.ed.Display())
		chips = append(chips, s.ed.Chips()...)
	}
	s.ed.SetValueWithChips(strings.Join(texts, "\n"), chips)
	return tea.Batch(s.clearQueue(), s.changed(c), c.Notify(ext.Notice{
		Key: "input.send", Text: "Claude could not start (" + err.Error() + "); your prompt was kept",
		Level: ext.NoticeError, Source: FeatureID,
	}))
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
