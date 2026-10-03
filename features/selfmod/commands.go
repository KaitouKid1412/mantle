package selfmod

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/launcher"
	sm "github.com/KaitouKid1412/mantle/internal/selfmod"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// subcommand describes one /mantle subcommand.
type subcommand struct {
	name, args, help string
	// arity: 0 = no argument, 1 = exactly one mod/request id, -1 = optional
	// id, 2 = id plus free text.
	arity int
}

var subcommands = []subcommand{
	{"list", "", "mods and requests", 0},
	{"show", "<id>", "a mod's request, files and checks", 1},
	{"status", "", "current build, probation and running builds", 0},
	{"undo", "<id>", "revert a mod, check and install", 1},
	{"rollback", "", "switch to the previous build (no rebuild)", 0},
	{"retry", "[id]", "run the builder again on a failed request", -1},
	{"edit", "<id> <request>", "change a mod with a new request", 2},
	{"promote", "<id>", "install a build you kept after the preview", 1},
	{"discard", "<id>", "drop a kept or failed request and its worktree", 1},
	{"cancel", "<id>", "stop a running build (keeps the worktree)", 1},
	{"apply", "<id>", "apply a config change the builder proposed", 1},
	{"update", "", "re-apply your mods on the newest mantle", 0},
	{"upstream", "<id>", "export a mod to the dev repo as mantle/mod-<id>", 1},
	{"restart", "", "restart into the new build now (when idle)", 0},
	{"help", "", "this help", 0},
}

// parseCommand splits /mantle's arguments into a subcommand and its
// arguments. Anything that does not fit a subcommand's shape is a request:
// "/mantle list the tools in the footer" asks for a change.
func parseCommand(args string) (name string, id string, rest string) {
	args = strings.TrimSpace(args)
	word, tail, _ := strings.Cut(args, " ")
	tail = strings.TrimSpace(tail)
	i := slices.IndexFunc(subcommands, func(s subcommand) bool { return s.name == word })
	if i < 0 {
		return "", "", args
	}
	idWord, more, _ := strings.Cut(tail, " ")
	more = strings.TrimSpace(more)
	switch subcommands[i].arity {
	case 0:
		if tail == "" {
			return word, "", ""
		}
	case 1:
		if sm.ValidModID(idWord) && more == "" {
			return word, idWord, ""
		}
	case -1:
		if tail == "" || (sm.ValidModID(idWord) && more == "") {
			return word, idWord, ""
		}
	case 2:
		if sm.ValidModID(idWord) && more != "" {
			return word, idWord, more
		}
	}
	return "", "", args
}

func (c *controller) runCommand(ctx ext.Ctx, args string) tea.Cmd {
	name, id, rest := parseCommand(args)
	switch name {
	case "":
		if rest == "" {
			return c.cmdHelp(ctx)
		}
		return c.begin(ctx, "request", func(ws *sm.Workspace) (*sm.Request, error) {
			return ws.Start(rest, sm.RequestMod, "")
		})
	case "help":
		return c.cmdHelp(ctx)
	case "list":
		return c.cmdList(ctx)
	case "show":
		return c.cmdShow(ctx, id)
	case "status":
		return c.cmdStatus(ctx)
	case "undo":
		return c.begin(ctx, "undo", func(ws *sm.Workspace) (*sm.Request, error) { return ws.StartUndo(id) })
	case "edit":
		return c.begin(ctx, "edit", func(ws *sm.Workspace) (*sm.Request, error) { return ws.StartEdit(id, rest) })
	case "rollback":
		return c.cmdRollback(ctx)
	case "retry":
		return c.cmdRetry(ctx, id)
	case "promote":
		return c.cmdPromote(ctx, id)
	case "discard":
		return c.cmdDiscard(ctx, id)
	case "cancel":
		return c.cmdCancel(ctx, id)
	case "apply":
		return c.cmdApply(ctx, id)
	case "update":
		return c.cmdUpdate(ctx)
	case "upstream":
		return c.cmdUpstream(ctx, id)
	case "restart":
		return c.restart(ctx)
	}
	return nil
}

func (c *controller) complete(ctx ext.Ctx, prefix string) []ext.Completion {
	var out []ext.Completion
	word, tail, hasSpace := strings.Cut(prefix, " ")
	if !hasSpace {
		for _, s := range subcommands {
			if strings.HasPrefix(s.name, word) {
				out = append(out, ext.Completion{Value: s.name, Display: strings.TrimSpace(s.name + " " + s.args), Description: s.help})
			}
		}
		return out
	}
	i := slices.IndexFunc(subcommands, func(s subcommand) bool { return s.name == word })
	if i < 0 || subcommands[i].arity == 0 {
		return nil
	}
	for _, id := range c.order {
		if strings.HasPrefix(id, strings.TrimSpace(tail)) {
			out = append(out, ext.Completion{Value: word + " " + id, Display: id, Description: c.builds[id].req.Request})
		}
	}
	return out
}

func (c *controller) cmdHelp(ctx ext.Ctx) tea.Cmd {
	var b bytes.Buffer
	b.WriteString("/mantle <request>  change mantle itself: a builder edits mantle's source, the checks vet it, and the new build runs next launch\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, s := range subcommands {
		fmt.Fprintf(tw, "  /mantle %s\t%s\n", strings.TrimSpace(s.name+" "+s.args), s.help)
	}
	tw.Flush()
	return ctx.Print(strings.TrimRight(b.String(), "\n"))
}

// async runs fn in a Cmd and prints its text.
func async(fn func() (string, error), errPrefix string) tea.Cmd {
	return func() tea.Msg {
		text, err := fn()
		if err != nil {
			return printMsg{notice: notice(ext.NoticeError, "cmd", errPrefix+": "+err.Error())}
		}
		return printMsg{text: text}
	}
}

func (c *controller) cmdList(ctx ext.Ctx) tea.Cmd {
	ws := c.workspace(ctx)
	live := c.liveSummary()
	return async(func() (string, error) {
		mods, err := ws.Mods()
		if err != nil {
			return "", err
		}
		reqs, _ := ws.Requests()
		return formatList(ws, mods, reqs, live), nil
	}, "/mantle list")
}

func formatList(ws *sm.Workspace, mods []sm.Mod, reqs []*sm.Request, live map[string]string) string {
	var b bytes.Buffer
	where := ws.Source
	if ws.Branch != "" {
		where += ", branch " + ws.Branch
	} else {
		where += " (dev mode)"
	}
	if len(mods) == 0 {
		fmt.Fprintf(&b, "No mods yet in %s. Try /mantle <request>.\n", where)
	} else {
		fmt.Fprintf(&b, "Mods in %s:\n", where)
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  ID\tKIND\tSTATE\tDATE\tREQUEST")
		for _, m := range mods {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", m.ID, m.Kind(), m.State, m.Date.Local().Format("2006-01-02 15:04"), truncate(m.Request, 60))
		}
		tw.Flush()
	}
	var open []*sm.Request
	for _, r := range reqs {
		if !r.Done() {
			open = append(open, r)
		}
	}
	if len(open) > 0 {
		b.WriteString("Requests not installed:\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, r := range open {
			state := r.State
			if phase, ok := live[r.ID]; ok {
				state = phase
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", r.ID, state, truncate(r.Request, 60))
		}
		tw.Flush()
	}
	return strings.TrimRight(b.String(), "\n")
}

func truncate(s string, n int) string {
	s = sm.OneLine(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// liveSummary is the phase of each build this session knows about.
func (c *controller) liveSummary() map[string]string {
	out := map[string]string{}
	for id, b := range c.builds {
		out[id] = phaseText(b.view())
	}
	return out
}

func (c *controller) cmdShow(ctx ext.Ctx, id string) tea.Cmd {
	ws := c.workspace(ctx)
	return async(func() (string, error) {
		d, err := ws.Show(id)
		if err != nil {
			return "", err
		}
		return formatShow(d), nil
	}, "/mantle show")
}

func formatShow(d *sm.ModDetail) string {
	var b strings.Builder
	m := d.Mod
	fmt.Fprintf(&b, "%s (%s, %s, %s)\n", m.ID, m.Kind(), m.State, m.Date.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "  request: %s\n", m.Request)
	for _, st := range d.Stats {
		for _, ln := range strings.Split(st, "\n") {
			if strings.TrimSpace(ln) != "" {
				b.WriteString("  " + ln + "\n")
			}
		}
	}
	if d.Report != nil {
		var steps []string
		for _, r := range d.Report.Results {
			mark := "ok"
			switch {
			case r.Skipped:
				mark = "skipped"
			case !r.OK:
				mark = "failed"
			}
			steps = append(steps, fmt.Sprintf("%s %s", r.Step, mark))
		}
		fmt.Fprintf(&b, "  checks: %s\n", strings.Join(steps, ", "))
	}
	if d.Request != nil && d.Request.BuildID != "" {
		fmt.Fprintf(&b, "  build: %s\n", d.Request.BuildID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *controller) cmdStatus(ctx ext.Ctx) tea.Cmd {
	ws := c.workspace(ctx)
	live := c.liveSummary()
	running := c.run.buildID
	return async(func() (string, error) {
		st, err := ws.Status()
		if err != nil {
			return "", err
		}
		return formatStatus(st, live, running), nil
	}, "/mantle status")
}

func formatStatus(st *sm.Status, live map[string]string, running string) string {
	var b strings.Builder
	switch {
	case st.Current == "":
		b.WriteString("No build installed (run `make install` in a mantle checkout).\n")
	case st.Probation != nil:
		fmt.Fprintf(&b, "Current build %s, on probation (%d/%d failed launches).\n", st.Current, st.Probation.Failures, launcher.MaxProbationFailures)
	default:
		fmt.Fprintf(&b, "Current build %s (healthy).\n", st.Current)
	}
	if running != "" && running != st.Current {
		fmt.Fprintf(&b, "This session runs build %s; the current build starts next launch.\n", running)
	}
	if st.LastGood != "" && st.LastGood != st.Current {
		fmt.Fprintf(&b, "Last good build %s.\n", st.LastGood)
	}
	if len(st.Active) == 0 {
		b.WriteString("No requests in progress.")
	} else {
		b.WriteString("Requests in progress:\n")
		for _, r := range st.Active {
			state := r.State
			if p, ok := live[r.ID]; ok {
				state = p
			}
			fmt.Fprintf(&b, "  %s: %s (round %d/%d) — %s\n", r.ID, state, r.Round, sm.MaxRounds, truncate(r.Request, 60))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (c *controller) cmdRollback(ctx ext.Ctx) tea.Cmd {
	ws := c.workspace(ctx)
	underLauncher := c.run.enabled
	return async(func() (string, error) {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		from, to, err := ws.Rollback(cctx)
		if err != nil {
			return "", err
		}
		text := fmt.Sprintf("current build: %s → %s (active next launch", from, to)
		if underLauncher {
			text += "; /mantle restart to switch now"
		}
		return text + ")", nil
	}, "/mantle rollback")
}

// cmdRetry runs the builder again on a failed request's worktree, with the
// last failure as context and a fresh set of rounds.
func (c *controller) cmdRetry(ctx ext.Ctx, id string) tea.Cmd {
	if b := c.builds[id]; id != "" && b != nil && activePhase(b.phase) {
		return c.notify(ctx, ext.NoticeWarning, "retry", id+" is still running")
	}
	ws := c.workspace(ctx)
	budget, model := c.budget(ctx), c.model(ctx)
	return func() tea.Msg {
		var r *sm.Request
		var err error
		if id != "" {
			r, err = ws.Load(id)
		} else {
			reqs, _ := ws.Requests()
			for _, q := range reqs {
				if q.State == sm.StateFailed {
					r = q
					break
				}
			}
			if r == nil {
				err = fmt.Errorf("no failed request to retry")
			}
		}
		if err == nil && r.State != sm.StateFailed && r.State != sm.StateBuilding && r.State != sm.StateVetting {
			err = fmt.Errorf("%s is %s, not failed", r.ID, r.State)
		}
		if err != nil {
			return startedMsg{err: err, action: "retry"}
		}
		failure := r.LastFailure
		r.Round, r.State = 0, sm.StateBuilding
		ws.Save(r)
		b := &build{ws: ws, req: r, budget: budget, model: model}
		if r.Kind != sm.RequestUndo {
			if b.rules, err = sm.WriteRules(ws.Layout.Build(r.ID)); err != nil {
				return startedMsg{err: err, action: "retry"}
			}
		}
		return startedMsg{b: b, retry: failure}
	}
}

func (c *controller) cmdPromote(ctx ext.Ctx, id string) tea.Cmd {
	b := c.builds[id]
	if b == nil || b.phase != PhaseReady || b.report == nil || !b.report.OK {
		return c.notify(ctx, ext.NoticeWarning, "promote", id+" has no vetted build waiting; see /mantle status")
	}
	return c.promote(ctx, b)
}

func (c *controller) cmdDiscard(ctx ext.Ctx, id string) tea.Cmd {
	if b := c.builds[id]; b != nil && activePhase(b.phase) {
		return c.notify(ctx, ext.NoticeWarning, "discard", id+" is running; /mantle cancel "+id+" first")
	}
	ws := c.workspace(ctx)
	if b := c.builds[id]; b != nil {
		delete(c.builds, id)
		c.order = slices.DeleteFunc(c.order, func(s string) bool { return s == id })
		ctx.Invalidate(ComponentID)
	}
	return async(func() (string, error) {
		r, err := ws.Load(id)
		if err != nil {
			return "", err
		}
		if r.State == sm.StatePromoted {
			return "", fmt.Errorf("%s is installed; use /mantle undo %s", id, id)
		}
		if err := ws.Abandon(r); err != nil {
			return "", err
		}
		return "discarded " + id + " and its worktree", nil
	}, "/mantle discard")
}

func (c *controller) cmdCancel(ctx ext.Ctx, id string) tea.Cmd {
	b := c.builds[id]
	if b == nil || !activePhase(b.phase) {
		return c.notify(ctx, ext.NoticeWarning, "cancel", id+" is not running")
	}
	return c.fail(ctx, b, "cancelled; the worktree is kept (/mantle retry "+id+" or /mantle discard "+id+")", nil)
}
