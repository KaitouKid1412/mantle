package turn

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/features/turn/dialogs"
	"github.com/KaitouKid1412/mantle/features/turn/gates"
	"github.com/KaitouKid1412/mantle/internal/cli"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// gateRun is one engine's pass through the startup gates.
type gateRun struct {
	msg     ext.SpawnGateMsg
	cwd     string
	flags   gates.LaunchFlags
	report  *gates.Report
	check   *engineCheck
	pending []gates.Kind
	i       int
	answers gates.Answers
	aborted bool
	// pin is set when the user chose the last good engine version; declined when they
	// exited at the version check.
	pin, declined bool
}

// engineCheck is the version gate's view of plan 02's engine.CheckEngine result.
type engineCheck struct {
	OK, Probed               bool
	Version                  string
	Failed                   []string // failed conformance checks
	LastGood, LastGoodBinary string   // last passing version, to offer pinning
}

// engineChecker checks the installed engine and pins a known-good one. Both run off the
// UI goroutine.
type engineChecker interface {
	Check(ctx context.Context) (engineCheck, error)
	Pin(binary, version string) error
}

// kindVersion is the engine version gate; it runs before the gates in package gates.
const kindVersion gates.Kind = "engineVersion"

// engineCheckTimeout bounds `claude --version` plus a conformance probe.
const engineCheckTimeout = 2 * time.Minute

// gateQueue runs one gate pass at a time (main first, builders after).
type gateQueue struct {
	waiting []*gateRun
	active  *gateRun
}

type gateEvaluatedMsg struct {
	run      *gateRun
	report   *gates.Report
	err      error
	check    *engineCheck
	checkErr error
}

type gateNextMsg struct{}

type gateRecordedMsg struct{ err error }

var gateDialogs = map[gates.Kind]string{
	gates.KindTrust:  DialogTrust,
	gates.KindBypass: DialogBypassWarning,
	gates.KindMcp:    DialogMcpApproval,
	gates.KindAPIKey: DialogAPIKey,
	kindVersion:      DialogEngineCheck,
}

func (st *state) setupGates(r ext.Registrar) {
	for _, id := range []string{DialogTrust, DialogBypassWarning, DialogMcpApproval, DialogAPIKey, DialogEngineCheck} {
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
	checker := st.checker
	if engineKey(run.msg.EngineID) != ext.MainEngine {
		checker = nil // builders run the same claude the main engine was checked with
	}
	return func() tea.Msg {
		if err != nil {
			return gateEvaluatedMsg{run: run, err: err}
		}
		m := gateEvaluatedMsg{run: run}
		if checker != nil {
			ctx, cancel := context.WithTimeout(context.Background(), engineCheckTimeout)
			c, cerr := checker.Check(ctx)
			cancel()
			m.check, m.checkErr = &c, cerr
		}
		m.report = gates.Evaluate(in)
		return m
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
	if m.checkErr != nil {
		st.gates.active = nil
		return tea.Batch(run.msg.Abort("could not check the claude engine: "+m.checkErr.Error()), st.startGates(c))
	}
	run.report = m.report
	run.check = m.check
	ms := st.mode(engineKey(run.msg.EngineID))
	ms.flags = run.flags
	ms.bypass = gates.BypassAvailable(run.flags, run.report.Settings)
	run.pending = run.report.Pending()

	var cmds []tea.Cmd
	if ck := run.check; ck != nil && !ck.OK {
		run.pending = append([]gates.Kind{kindVersion}, run.pending...)
	}
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
		case DialogEngineCheck:
			ck := run.check
			if ck == nil {
				ck = &engineCheck{}
			}
			lastGood := ck.LastGood
			if ck.LastGoodBinary == "" {
				lastGood = ""
			}
			ch := dialogs.NewEngineCheck(ck.Version, ck.Failed, lastGood)
			d.vm, d.result = ch, func() any { return ch.Chosen() }
			answer = func() {
				switch ch.Chosen() {
				case dialogs.EnginePin:
					run.pin = true
				case dialogs.EngineExit:
					run.aborted, run.declined = true, true
				}
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
		case run.declined:
			reason = "claude did not pass mantle's startup checks"
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
	spawn := run.msg.Proceed(opts)
	if run.pin && run.check != nil && st.checker != nil {
		// Pin first: the engine manager resolves the binary (honouring the pin) at spawn.
		checker, ck := st.checker, *run.check
		pin := func() tea.Msg {
			if err := checker.Pin(ck.LastGoodBinary, ck.LastGood); err != nil {
				return gateRecordedMsg{err: fmt.Errorf("pin claude %s: %w", ck.LastGood, err)}
			}
			return nil
		}
		spawn = tea.Sequence(pin, spawn)
	}
	return tea.Batch(record, spawn, next)
}

func gatePending(run *gateRun, k gates.Kind) bool {
	for _, p := range run.pending {
		if p == k {
			return true
		}
	}
	return false
}

// launchFlags reads the gate inputs from spawn options with plan 11's claude flag
// table, so a flag's value is never mistaken for a flag and ExtraArgs (which claude
// sees last) win.
func launchFlags(o ext.SpawnOpts) gates.LaunchFlags {
	g := cli.GateInputsOf(o)
	return gates.LaunchFlags{
		PermissionMode:   g.PermissionMode,
		Settings:         g.Settings,
		DangerouslySkip:  g.SkipPermissions,
		AllowDangerously: g.AllowSkipPermissions,
		StrictMcpConfig:  g.StrictMcpConfig,
	}
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
