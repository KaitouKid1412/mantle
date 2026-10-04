package input

import (
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
	"github.com/KaitouKid1412/mantle/pkg/ui/editor/vim"
)

// stories renders the prompt and its panels in fixed states, each from a
// fresh state so they never depend on the live session.
func stories(_ *state) []ext.Story {
	story := func(id string, setup func(c ext.Ctx, s *state)) ext.Story {
		return ext.Story{ID: id, Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
			s := newState()
			s.histPath = ""
			s.applyTheme(c.Theme())
			setup(c, s)
			a.Focused = true
			r := s.viewPrompt(c, a)
			if m := s.viewMenu(c, a); m.Text != "" {
				r.Text += "\n" + m.Text
			}
			return r
		}}
	}
	return []ext.Story{
		story("input.editor/empty", func(c ext.Ctx, s *state) {}),
		story("input.editor/multiline", func(c ext.Ctx, s *state) {
			s.ed.SetValue("Refactor the session loader so resumed sessions replay their earlier turns,\nthen add tests for sidechains and malformed lines.")
		}),
		story("input.editor/chips", func(c ext.Ctx, s *state) {
			s.ed.InsertString("Compare ")
			s.ed.InsertImage(&editor.Image{MediaType: "image/png"})
			s.ed.InsertString("with ")
			s.ed.InsertPaste("one\ntwo\nthree\nfour\nfive")
		}),
		story("input.editor/bash", func(c ext.Ctx, s *state) {
			s.mode = modeBash
			s.ed.SetValue("git status --short")
		}),
		story("input.editor/suggestion", func(c ext.Ctx, s *state) {
			s.ed.SetGhost("run the tests and fix what fails")
		}),
		story("input.editor/vim-normal", func(c ext.Ctx, s *state) {
			s.ed.SetVim(true, nil)
			s.ed.SetValue("ultrathink about the vim mode")
			s.ed.SetVimMode(vim.Normal)
		}),
		story("input.menu/slash", func(c ext.Ctx, s *state) {
			s.ed.SetValue("/co")
			s.comp.update(c, s)
		}),
		story("input.menu/help", func(c ext.Ctx, s *state) {
			s.help = true
		}),
		story("input.menu/search", func(c ext.Ctx, s *state) {
			s.cwd = "/work/demo"
			s.histAll = []history.Entry{
				{Display: "run the linter", Project: "/work/demo"},
				{Display: "explain the build pipeline", Project: "/work/demo"},
			}
			s.search = &search{scope: scopeProject, query: "build"}
			s.search.refresh(s)
		}),
	}
}
