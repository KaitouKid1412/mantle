// Package importcfg implements /import: preview what `claude import` would
// bring in from another coding agent (codex, gemini, cursor), confirm, then
// apply with the preview's scan digest so nothing changes between the preview
// and the import.
//
// Primary owner: plan 09 (docs/plans/09-ecosystem-panels.md).
package importcfg

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/ecosystem/internal/eco"
	"github.com/KaitouKid1412/mantle/internal/claudecli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// FeatureID is this feature's ID.
const FeatureID = "ecosystem.import"

// DialogID is the /import dialog.
const DialogID = "dialog.ecosystem.import"

// Sources are the agents claude import knows.
var Sources = []string{"codex", "gemini", "cursor"}

func init() {
	ext.Register(ext.Feature{ID: FeatureID, Order: 330, Parity: []string{"EC-21"}, Setup: Setup})
}

// Setup registers /import and its dialog.
func Setup(r ext.Registrar) error {
	r.AddCommand(ext.Command{
		Name: "import", ArgHint: "[codex|gemini|cursor]", Source: ext.SourceBuiltin,
		Description: "Import settings from another coding agent",
		Run: func(ctx ext.Ctx, args string) tea.Cmd {
			src := strings.TrimSpace(args)
			if src != "" && !valid(src) {
				return ctx.Notify(ext.Notice{Key: "import.usage", Level: ext.NoticeWarning,
					Text: fmt.Sprintf("Unknown source %q; use one of %s.", src, strings.Join(Sources, ", "))})
			}
			return ctx.OpenDialog(DialogID, src)
		},
		Complete: func(ctx ext.Ctx, prefix string) []ext.Completion {
			var out []ext.Completion
			for _, s := range Sources {
				if strings.HasPrefix(s, prefix) {
					out = append(out, ext.Completion{Value: s})
				}
			}
			return out
		},
	})
	r.AddDialog(DialogID, eco.Factory(New))
	r.AddStory(ext.Story{ID: "ecosystem.import/preview", Render: func(ctx ext.Ctx, a ext.Area) ext.Rendered {
		d, _ := New(ctx, "codex")
		v := d.Root().(*view)
		v.setPreview(claudecli.ImportPreview{Text: storyPreview, Digest: "d1g3st"})
		return d.View(ctx, a)
	}})
	return nil
}

const storyPreview = "Found 3 importable items from Example Agent (scan digest: d1g3st).\n" +
	"  [user] instructions file -> CLAUDE.md\n  [user] 2 MCP servers"

func valid(s string) bool {
	for _, x := range Sources {
		if x == s {
			return true
		}
	}
	return false
}

// New builds the dialog; args is the source ("" = every detected agent).
func New(ctx ext.Ctx, args any) (*eco.Dialog, error) {
	src, _ := args.(string)
	return eco.NewDialog(DialogID, "Import", &view{source: src}), nil
}

type phase int

const (
	scanning phase = iota
	previewing
	importing
	finished
)

type view struct {
	eco.Base
	source  string
	phase   phase
	preview claudecli.ImportPreview
	result  string
}

func (v *view) Subtitle() string {
	if v.source == "" {
		return "all agents"
	}
	return v.source
}

func (v *view) Init(ctx ext.Ctx, d *eco.Dialog) tea.Cmd {
	cli, src := eco.CLI(ctx), v.source
	eco.Busy(ctx, d, "Looking for importable settings")
	return eco.Async("import.preview", func() (any, error) {
		return cli.ImportDryRun(eco.Background(), src)
	})
}

func (v *view) setPreview(p claudecli.ImportPreview) {
	v.preview, v.phase = p, previewing
}

func (v *view) Update(ctx ext.Ctx, d *eco.Dialog, msg tea.Msg) tea.Cmd {
	m, ok := msg.(eco.ResultMsg)
	if !ok {
		return nil
	}
	switch m.Key {
	case "import.preview":
		d.SetStatus(ctx, "", "")
		if m.Err != nil {
			v.phase = finished
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		v.setPreview(m.Value.(claudecli.ImportPreview))
	case "import.apply":
		v.phase = finished
		if m.Err != nil {
			eco.Fail(ctx, d, m.Err)
			return nil
		}
		v.result, _ = m.Value.(string)
		d.CloseLine = "Import finished"
		eco.Done(ctx, d, "Imported. Restart Claude to load the new settings.")
	}
	ctx.Invalidate(d.ID())
	return nil
}

func (v *view) Action(ctx ext.Ctx, d *eco.Dialog, a ext.ActionID) (bool, tea.Cmd) {
	if a != ext.ActSelectAccept {
		return false, nil
	}
	switch {
	case v.phase == previewing && v.preview.Digest != "":
		v.phase = importing
		eco.Busy(ctx, d, "Importing")
		cli, src, digest := eco.CLI(ctx), v.source, v.preview.Digest
		return true, eco.Async("import.apply", func() (any, error) {
			return cli.ImportApply(eco.Background(), src, digest)
		})
	case v.phase == finished && v.result != "":
		return true, tea.Batch(d.Close(ctx), eco.RestartEngine(ctx, "to load the imported settings"))
	case v.phase == finished || v.phase == previewing:
		return true, d.Close(ctx)
	}
	return true, nil
}

func (v *view) Render(ctx ext.Ctx, th *theme.Theme, width, height int) []string {
	switch v.phase {
	case scanning, importing:
		return nil
	case previewing:
		out := eco.Wrap(v.preview.Text, width)
		if v.preview.Digest == "" {
			return out
		}
		return append(out, "", th.Paint(theme.Text, "Import these into Claude Code?"))
	}
	if v.result != "" {
		return eco.Wrap(v.result, width)
	}
	return nil
}

func (v *view) Hints(ctx ext.Ctx) string {
	switch {
	case v.phase == previewing && v.preview.Digest != "":
		return eco.Hint("enter", "import", "esc", "cancel")
	case v.phase == finished && v.result != "":
		return eco.Hint("enter", "restart Claude", "esc", "later")
	case v.phase == scanning || v.phase == importing:
		return eco.Hint("esc", "close")
	}
	return eco.Hint("enter", "close")
}
