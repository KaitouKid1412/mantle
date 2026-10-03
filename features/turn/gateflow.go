package turn

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// gateRun is one engine's pass through the startup gates.
type gateRun struct {
	msg     ext.SpawnGateMsg
	cwd     string
	flags   gates.LaunchFlags
	report  *gates.Report
	pending []gates.Kind
	i       int
	answers gates.Answers
	aborted bool
}

// gateQueue runs one gate pass at a time (main first, builders after).
type gateQueue struct {
	waiting []*gateRun
	active  *gateRun
}

type gateEvaluatedMsg struct {
	run    *gateRun
	report *gates.Report
	err    error
}

type gateNextMsg struct{}

type gateRecordedMsg struct{ err error }

var gateDialogs = map[gates.Kind]string{
	gates.KindTrust:  DialogTrust,
	gates.KindBypass: DialogBypassWarning,
	gates.KindMcp:    DialogMcpApproval,
	gates.KindAPIKey: DialogAPIKey,
}

func (st *state) setupGates(r ext.Registrar) {
	for _, id := range []string{DialogTrust, DialogBypassWarning, DialogMcpApproval, DialogAPIKey} {
		r.AddDialog(id, st.gateDialog(id))
	}
	ext.Subscribe(r, "turn.spawnGate", func(c ext.Ctx, m ext.SpawnGateMsg) tea.Cmd {
		st.gates.waiting = append(st.gates.waiting, &gateRun{msg: m})
		return st.startGates(c)
	})
	ext.Subscribe(r, "turn.gateEvaluated", st.onGateEvaluated)
	ext.Subscribe(r, "turn.gateNext", func(c ext.Ctx, _ gateNextMsg) tea.Cmd { return st.nextGate(c) })
	ext.Subscribe(r, "turn.gateRecorded", func(c ext.Ctx, m gateRecordedMsg) tea.Cmd {
		if m.err == nil {
			return nil
		}
		return c.Notify(ext.Notice{Key: "turn.gateRecord", Text: "Could not save your answers: " + m.err.Error(), Level: ext.NoticeWarning, Source: FeatureID})
	})
	st.setupLimits(r)
}

// startGates evaluates the next waiting engine's gates off the UI goroutine.
func (st *state) startGates(c ext.Ctx) tea.Cmd {
	if st.gates.active != nil || len(st.gates.waiting) == 0 {
		return nil
	}
	run := st.gates.waiting[0]
	st.gates.waiting = st.gates.waiting[1:]
	st.gates.active = run
	run.flags = launchFlags(run.msg.Opts)
	run.cwd = run.msg.Opts.Cwd
	if run.cwd == "" {
		run.cwd, _ = os.Getwd()
	}
	if s := c.Session().Cwd; run.cwd == "" && s != "" {
		run.cwd = s
	}
	env, err := st.env()
	in := gates.Input{
		Env:            env,
		Cwd:            run.cwd,
		Flags:          run.flags,
		AutoTrust:      strings.HasPrefix(run.msg.EngineID, "builder"),
		SessionTrusted: st.trusted[run.cwd],
	}
	return func() tea.Msg {
		if err != nil {
			return gateEvaluatedMsg{run: run, err: err}
		}
		return gateEvaluatedMsg{run: run, report: gates.Evaluate(in)}
	}
}

func (st *state) onGateEvaluated(c ext.Ctx, m gateEvaluatedMsg) tea.Cmd {
	run := m.run
	if st.gates.active != run {
		return nil
	}
	if m.err != nil {
		// Without a home directory the gates can't run, and spawning unchecked would
		// skip them: refuse.
		st.gates.active = nil
		return tea.Batch(run.msg.Abort("startup checks failed: "+m.err.Error()), st.startGates(c))
	}
	run.report = m.report
	ms := st.mode(engineKey(run.msg.EngineID))
	ms.flags = run.flags
	ms.bypass = gates.BypassAvailable(run.flags, run.report.Settings)
	run.pending = run.report.Pending()

	var cmds []tea.Cmd
	for i, n := range run.report.Notices() {
		cmds = append(cmds, c.Notify(ext.Notice{
			Key: fmt.Sprintf("turn.gateNotice.%d", i), Text: n, Level: ext.NoticeWarning,
			Timeout: 15 * time.Second, Source: FeatureID,
		}))
	}
	return tea.Batch(append(cmds, st.nextGate(c))...)
}

// nextGate opens the next pending gate dialog, or finishes the pass.
func (st *state) nextGate(c ext.Ctx) tea.Cmd {
	run := st.gates.active
	if run == nil || run.report == nil {
		return nil
	}
	if run.aborted || run.i >= len(run.pending) {
		return st.finishGates(c)
	}
	return c.OpenDialog(gateDialogs[run.pending[run.i]], run)
}

func (st *state) gateDialog(id string) ext.DialogFactory {
	return func(c ext.Ctx, args any) (ext.Dialog, error) {
		run, ok := args.(*gateRun)
		if !ok || run.report == nil {
			return nil, fmt.Errorf("%s: opened outside the startup checks", id)
		}
		rep := run.report
		d := &vmDialog{id: id, contexts: []string{ext.ContextConfirmation}, place: ext.PlaceCentered}
		var answer func()
		switch id {
		case DialogTrust:
			ch := dialogs.NewTrust(rep.TrustReport, rep.Trust.HomeDir)
			d.vm, d.result = ch, func() any { return ch.Chosen() }
			answer = func() {
				run.answers.TrustAccepted = ch.Chosen() == dialogs.ChoiceYes
				run.aborted = !run.answers.TrustAccepted
			}
		case DialogBypassWarning:
			ch := dialogs.NewBypassWarning()
			d.vm, d.result = ch, func() any { return ch.Chosen() }
			answer = func() {
				run.answers.BypassAccepted = ch.Chosen() == dialogs.ChoiceYes
				run.aborted = !run.answers.BypassAccepted
			}
		case DialogMcpApproval:
			m := dialogs.NewMcpApproval(rep.Mcp.Pending())
			d.vm, d.result = m, func() any { return m.Approved() }
			answer = func() {
				run.answers.McpApproved = m.Approved()
				run.answers.McpEnableAll = m.EnableAll()
			}
		case DialogAPIKey:
			ch := dialogs.NewAPIKeyPrompt(rep.APIKey.Suffix)
			d.vm, d.result = ch, func() any { return ch.Chosen() }
			answer = func() {
				yes := ch.Chosen() == dialogs.ChoiceYes
				run.answers.APIKeyApproved = &yes
			}
		default:
			return nil, fmt.Errorf("unknown gate dialog %q", id)
		}
		d.onDone = func(c ext.Ctx) tea.Cmd {
			answer()
			run.i++
			return tea.Sequence(c.CloseDialog(id), ext.Msg(gateNextMsg{}))
		}
		// ctrl+c during the startup checks leaves without starting Claude.
		d.onInterrupt = func(c ext.Ctx) tea.Cmd {
			run.aborted = true
			return tea.Sequence(c.CloseDialog(id), ext.Msg(gateNextMsg{}))
		}
		return d, nil
	}
}

// finishGates records the answers and spawns the engine with the amended options, or
// aborts the launch.
func (st *state) finishGates(c ext.Ctx) tea.Cmd {
	run := st.gates.active
	st.gates.active = nil
	next := st.startGates(c)
	rep := run.report
	answers := run.answers
	record := func() tea.Msg { return gateRecordedMsg{err: rep.Record(answers)} }
	if answers.TrustAccepted && rep.Trust.HomeDir {
		st.trusted[run.cwd] = true
	}
	if run.aborted {
		reason := "startup cancelled"
		switch {
		case gatePending(run, gates.KindTrust) && !answers.TrustAccepted:
			reason = "workspace not trusted"
		case gatePending(run, gates.KindBypass) && !answers.BypassAccepted:
			reason = "bypass permissions mode declined"
		}
		return tea.Batch(record, run.msg.Abort(reason), next)
	}
	out, err := rep.Resolve(answers)
	if err != nil {
		return tea.Batch(record, run.msg.Abort(err.Error()), next)
	}
	if !out.Proceed {
		return tea.Batch(record, run.msg.Abort(out.Reason), next)
	}
	opts := run.msg.Opts
	opts.Settings = out.Settings
	opts.ExtraArgs = stripFlag(opts.ExtraArgs, "--settings")
	opts.UnsetEnv = append(append([]string(nil), opts.UnsetEnv...), out.UnsetEnv...)
	return tea.Batch(record, run.msg.Proceed(opts), next)
}

func gatePending(run *gateRun, k gates.Kind) bool {
	for _, p := range run.pending {
		if p == k {
			return true
		}
	}
	return false
}

// valueFlags are claude flags that take a value as the next argument. Their values are
// skipped when scanning ExtraArgs, so `--append-system-prompt --dangerously-skip-permissions`
// (a prompt text) is not mistaken for the flag. Plan 11's cli.GateInputsOf replaces this
// scan once internal/cli reaches this branch.
var valueFlags = map[string]bool{
	"--add-dir": true, "--agent": true, "--agents": true, "--allowedTools": true, "--allowed-tools": true,
	"--append-system-prompt": true, "--append-system-prompt-file": true, "--betas": true,
	"--debug-file": true, "--disallowedTools": true, "--disallowed-tools": true, "--effort": true,
	"--fallback-model": true, "--input-format": true, "--json-schema": true, "--max-budget-usd": true,
	"--max-thinking-tokens": true, "--max-turns": true, "--mcp-config": true, "--model": true,
	"--name": true, "-n": true, "--output-format": true, "--permission-mode": true,
	"--permission-prompt-tool": true, "--plugin-dir": true, "--resume": true, "-r": true,
	"--session-id": true, "--setting-sources": true, "--settings": true, "--system-prompt": true,
	"--system-prompt-file": true, "--task-budget": true, "--thinking": true, "--thinking-display": true,
	"--tools": true, "--worktree": true, "-w": true, "--remote-control-session-name-prefix": true,
}

// launchFlags reads the gate inputs from spawn options: the explicit fields, then the
// user's claude flags forwarded in ExtraArgs (which win, as claude sees them last).
func launchFlags(o ext.SpawnOpts) gates.LaunchFlags {
	f := gates.LaunchFlags{PermissionMode: o.PermissionMode, Settings: o.Settings}
	args := o.ExtraArgs
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		name, val, hasVal := strings.Cut(args[i], "=")
		takeVal := func() string {
			if hasVal {
				return val
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch name {
		case "--dangerously-skip-permissions":
			f.DangerouslySkip = true
		case "--allow-dangerously-skip-permissions":
			f.AllowDangerously = true
		case "--strict-mcp-config":
			f.StrictMcpConfig = true
		case "--permission-mode":
			f.PermissionMode = takeVal()
		case "--settings":
			f.Settings = takeVal()
		default:
			if valueFlags[name] {
				takeVal()
			}
		}
	}
	return f
}

// stripFlag removes a value-taking flag ("--x v" or "--x=v") from args.
func stripFlag(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == flag {
			i++
			continue
		}
		if strings.HasPrefix(args[i], flag+"=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}
