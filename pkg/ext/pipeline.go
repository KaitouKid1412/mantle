package ext

import (
	tea "charm.land/bubbletea/v2"
)

// Draft is a prompt on its way from the editor to the engine.
type Draft struct {
	Text           string
	Attachments    []Attachment
	Mode, Priority string // Mode: "prompt" | "bash" ; Priority as in Prompt
	// Resubmit marks a draft a stage consumed earlier and now re-submits through
	// Ctx.Submit (contracts-v1.11). Stages with side effects (history) skip it.
	Resubmit bool
}

// Attachment is something attached to a draft: an image, a file reference or a
// collapsed paste.
type Attachment struct {
	Kind      string // "image" | "file" | "paste"
	Name      string // display label ("[Image #1]", "foo.go")
	Path      string // file path, when there is one
	MediaType string // "image/png", …
	Data      []byte // inline bytes (images)
	Text      string // pasted text
}

// Verdict is a prompt stage's decision.
type Verdict int

const (
	Continue Verdict = iota // pass the (possibly modified) draft to the next stage
	Consumed                // handled; stop the pipeline
	Reject                  // refuse; keep the draft in the editor
)

// PromptStage is one step of the submit pipeline. Stages run in ascending priority.
type PromptStage func(Ctx, *Draft) (Verdict, tea.Cmd)

// Interceptor is priority-ordered tea.Msg middleware, run before any other routing.
// Return the message (or a replacement) to continue; return a nil message to consume
// it.
type Interceptor func(Ctx, tea.Msg) (tea.Msg, tea.Cmd) // nil msg = consumed

// Story renders a component at a given width with fixed data: goldens, previews,
// `mantle-ui story`, and the /mantle builder's eyes.
type Story struct {
	ID     string // "chrome.footer/plan-mode"
	Widths []int  // nil = 60, 100, 160
	Render func(Ctx, Area) Rendered
}
