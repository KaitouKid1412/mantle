package selfmod

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// KeyBuild is the content key of a /mantle build block.
const KeyBuild ext.ContentKey = "mantle.build"

// ComponentID is the live progress component above the input.
const ComponentID = "selfmod.progress"

// Build phases.
const (
	PhaseStarting   = "starting"
	PhaseBuilding   = "building"
	PhaseResolving  = "resolving" // the builder resolves rebase conflicts
	PhaseUpdating   = "updating"  // /mantle update re-applies commits
	PhaseVetting    = "vetting"
	PhasePreviewing = "previewing"
	PhaseReady      = "ready" // vetted, waiting for the user to promote
	PhasePromoting  = "promoting"
	PhaseDone       = "done"
	PhaseFailed     = "failed"
	PhaseConfig     = "config"
)

// activePhase reports whether a build is still running.
func activePhase(p string) bool {
	switch p {
	case PhaseDone, PhaseFailed, PhaseConfig, PhaseReady:
		return false
	}
	return true
}

// BuildView is the data of a mantle.build item: a snapshot of one request.
type BuildView struct {
	ID       string
	Kind     string
	Request  string
	Phase    string
	Round    int
	Rounds   int
	LastTool string
	CostUSD  float64
	Budget   float64
	Start    time.Time
	End      time.Time
	Err      string
	Detail   []string // failure lines or the config proposal
	BuildID  string
}

// RenderBuild is the mantle.build renderer: one summary line, and when
// expanded (or finished with details) the request and the detail lines.
func RenderBuild(rc ext.RenderCtx, it *ext.Item) ext.Block {
	v, ok := it.Data.(*BuildView)
	if !ok || v == nil {
		return ext.Block{Lines: []string{"/mantle"}}
	}
	th := rc.Theme
	if th == nil {
		d := theme.Default()
		th = &d
	}
	paint := func(tok theme.Token, s string) string { return th.Paint(tok, s) }
	dot, tok := "●", theme.Accent
	switch v.Phase {
	case PhaseDone:
		tok = theme.Success
	case PhaseFailed:
		tok = theme.Error
	case PhaseConfig, PhaseReady, PhasePreviewing:
		tok = theme.Warning
	}
	label := "/mantle " + v.ID
	if v.Kind == "undo" || v.Kind == "update" {
		label = "/mantle " + v.Kind
		if v.Kind == "undo" {
			label = "/mantle " + strings.TrimPrefix(v.ID, "undo-") + " (undo)"
		}
	}
	parts := []string{paint(tok, dot) + " " + paint(theme.Text, label), phaseText(v)}
	if v.LastTool != "" && activePhase(v.Phase) {
		parts = append(parts, v.LastTool)
	}
	if v.CostUSD > 0 {
		cost := fmt.Sprintf("$%.2f", v.CostUSD)
		if v.Budget > 0 {
			cost += fmt.Sprintf("/$%.2f", v.Budget)
		}
		parts = append(parts, cost)
	}
	if !v.Start.IsZero() {
		end := v.End
		if end.IsZero() {
			end = rc.Now
		}
		if d := end.Sub(v.Start); d > 0 {
			parts = append(parts, formatDuration(d))
		}
	}
	head := parts[0] + paint(theme.Inactive, " · "+strings.Join(parts[1:], " · "))
	lines := []string{ansi.Truncate(head, max(rc.Width, 1), "…")}
	showDetail := rc.Expanded || !activePhase(v.Phase)
	if showDetail {
		indent := "  "
		if rc.Expanded && v.Request != "" {
			lines = append(lines, wrapLines(paint(theme.Inactive, "request: ")+v.Request, rc.Width, indent)...)
		}
		if v.Err != "" {
			lines = append(lines, wrapLines(paint(theme.Error, v.Err), rc.Width, indent)...)
		}
		max := len(v.Detail)
		if !rc.Expanded && max > 6 {
			max = 6
		}
		for _, d := range v.Detail[:max] {
			lines = append(lines, wrapLines(paint(theme.Inactive, d), rc.Width, indent)...)
		}
		if max < len(v.Detail) {
			lines = append(lines, indent+paint(theme.Subtle, fmt.Sprintf("… %d more (ctrl+o)", len(v.Detail)-max)))
		}
	}
	return ext.Block{Lines: lines, Collapsible: v.Request != "" || len(v.Detail) > 0}
}

func phaseText(v *BuildView) string {
	switch v.Phase {
	case PhaseStarting:
		return "starting the builder"
	case PhaseBuilding:
		if v.Round > 0 {
			return fmt.Sprintf("fixing (round %d/%d)", v.Round+1, v.Rounds)
		}
		return "building"
	case PhaseResolving:
		return "resolving conflicts"
	case PhaseUpdating:
		return "re-applying mods"
	case PhaseVetting:
		return fmt.Sprintf("checking (round %d/%d)", max(v.Round, 1), v.Rounds)
	case PhasePreviewing:
		return "waiting for your review"
	case PhaseReady:
		return "ready to promote"
	case PhasePromoting:
		return "installing"
	case PhaseDone:
		if v.BuildID != "" {
			return "ready · active next launch"
		}
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseConfig:
		return "config change proposed"
	}
	return v.Phase
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// wrapLines hard-wraps s to width with an indent on every line.
func wrapLines(s string, width int, indent string) []string {
	w := max(width-ansi.StringWidth(indent), 10)
	var out []string
	for _, ln := range strings.Split(ansi.Hardwrap(s, w, true), "\n") {
		out = append(out, indent+ln)
	}
	return out
}

// progressComp shows the running builds above the input.
type progressComp struct {
	c *controller
}

func (p *progressComp) ID() string                      { return ComponentID }
func (p *progressComp) Init(ext.Ctx) tea.Cmd            { return nil }
func (p *progressComp) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }

func (p *progressComp) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	var lines []string
	render := ctx.Renderer(KeyBuild)
	for _, b := range p.c.activeBuilds() {
		it := b.item(ctx.Clock().Now())
		blk := render(ext.RenderCtx{Width: a.Width, Theme: ctx.Theme(), Now: ctx.Clock().Now()}, it)
		lines = append(lines, blk.Lines...)
	}
	if a.MaxHeight > 0 && len(lines) > a.MaxHeight {
		more := len(lines) - a.MaxHeight + 1
		lines = append(lines[:a.MaxHeight-1], ctx.Theme().Paint(theme.Subtle, fmt.Sprintf("  +%d more", more)))
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
