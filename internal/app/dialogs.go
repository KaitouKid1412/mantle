package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Dialog placement policy:
//   - PlaceInline replaces the input slot (Claude Code's permission prompts and
//     pickers).
//   - PlaceCentered is a layer in fullscreen/alt views; inline it also replaces the
//     input slot, because an inline frame cannot overlay scrollback.
//   - PlaceAltScreen switches the frame to the alternate screen and gives the dialog the
//     whole terminal (ctrl+o viewer, /diff, big pickers). Inline scrollback is kept by
//     the terminal and prints queued meanwhile are written after it closes.
//
// Only the top dialog renders and receives keys. Every open dialog receives broadcast
// messages in its Update.

func (r *Root) topDialog() *openDialog {
	if len(r.dialogs) == 0 {
		return nil
	}
	return r.dialogs[len(r.dialogs)-1]
}

func (r *Root) openDialog(id string, args any) tea.Cmd {
	e, ok := r.dialogFacts[id]
	if !ok || r.host.Disabled(e.feature) {
		return r.addNotice(ext.Notice{Key: "dialog:" + id, Text: fmt.Sprintf("No dialog %q", id), Level: ext.NoticeError, Source: "core"})
	}
	var d ext.Dialog
	var err error
	if !r.safe(e.feature, id, func() { d, err = e.value.(ext.DialogFactory)(r.ctx, args) }) {
		return nil
	}
	if err != nil {
		return r.addNotice(ext.Notice{Key: "dialog:" + id, Text: err.Error(), Level: ext.NoticeError, Source: e.feature})
	}
	if d == nil {
		return nil
	}
	wasAlt := r.layoutMode() == ext.AltView
	od := &openDialog{id: id, feature: e.feature, d: d, c: &comp{feature: e.feature}}
	r.dialogs = append(r.dialogs, od)
	r.resolver.Reset()
	r.invalidateAll()
	var initCmd tea.Cmd
	r.safe(e.feature, d.ID()+".Init", func() { initCmd = d.Init(r.ctx) })
	cmds := []tea.Cmd{wrapCmd(e.feature, d.ID(), initCmd), ext.Msg(ext.DialogOpenedMsg{ID: id, Instance: d.ID()})}
	if wasAlt && r.layoutMode() != ext.AltView {
		cmds = append(cmds, r.resumePrinter())
	}
	return tea.Batch(cmds...)
}

// closeDialog pops the topmost dialog whose instance ID or registry ID is id.
func (r *Root) closeDialog(id string) tea.Cmd {
	for i := len(r.dialogs) - 1; i >= 0; i-- {
		d := r.dialogs[i]
		if d.d.ID() != id && d.id != id {
			continue
		}
		wasAlt := r.layoutMode() == ext.AltView
		r.dialogs = append(r.dialogs[:i], r.dialogs[i+1:]...)
		r.resolver.Reset()
		r.invalidateAll()
		var result any
		if res, ok := d.d.(ext.Resulter); ok {
			r.safe(d.feature, d.d.ID()+".Result", func() { result = res.Result() })
		}
		cmds := []tea.Cmd{ext.Msg(ext.DialogClosedMsg{ID: d.id, Instance: d.d.ID(), Result: result})}
		if wasAlt && r.layoutMode() != ext.AltView {
			cmds = append(cmds, r.resumePrinter())
		}
		return tea.Batch(cmds...)
	}
	return nil
}

// Dialogs returns the open dialog instance IDs, bottom to top (for tests).
func (r *Root) Dialogs() []string {
	out := make([]string, len(r.dialogs))
	for i, d := range r.dialogs {
		out[i] = d.d.ID()
	}
	return out
}
