package selfmod

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/KaitouKid1412/mantle/internal/archtest"
	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// StepID names a pipeline step.
type StepID string

// The pipeline steps, in order.
const (
	StepProtected StepID = "protected" // 1. protected-path diff check
	StepGofmt     StepID = "gofmt"     // 2. gofmt -l on changed Go files
	StepVet       StepID = "vet"       // 3. go vet ./...
	StepArch      StepID = "archtest"  // 4. import rules
	StepAPIDiff   StepID = "apidiff"   // 5. pkg/* API compatibility
	StepBuild     StepID = "build"     // 6. build mantle-ui
	StepTest      StepID = "test"      // 7. go test ./...
	StepSelftest  StepID = "selftest"  // 8. mantle-ui selftest
	StepSmoke     StepID = "smoke"     // 9. pty smoke boot
)

// AllSteps is the full pipeline in order.
var AllSteps = []StepID{StepProtected, StepGofmt, StepVet, StepArch, StepAPIDiff, StepBuild, StepTest, StepSelftest, StepSmoke}

// PostRebaseSteps are re-run when promotion had to rebase onto a moved user
// branch.
var PostRebaseSteps = []StepID{StepBuild, StepTest, StepSelftest, StepSmoke}

// lintSteps run together: the builder gets all their errors in one round,
// and the pipeline stops after the group if any failed.
var lintSteps = []StepID{StepGofmt, StepVet, StepArch, StepAPIDiff}

// DefaultTimeouts per step.
var DefaultTimeouts = map[StepID]time.Duration{
	StepProtected: time.Minute,
	StepGofmt:     time.Minute,
	StepVet:       5 * time.Minute,
	StepArch:      3 * time.Minute,
	StepAPIDiff:   5 * time.Minute,
	StepBuild:     10 * time.Minute,
	StepTest:      15 * time.Minute,
	StepSelftest:  2 * time.Minute,
	StepSmoke:     time.Minute,
}

// Config configures the commands the pipeline runs. Zero values mean the
// defaults, so a zero Config runs the real toolchain.
type Config struct {
	Go    string // "go"
	Gofmt string // "gofmt"
	Git   string // "git"
	// Env is added to every step's environment.
	Env []string
	// Timeouts overrides DefaultTimeouts per step.
	Timeouts map[StepID]time.Duration
	// Protected overrides ProtectedPaths.
	Protected []string
	// UIPackage is the package built in step 6 ("./cmd/mantle-ui").
	UIPackage string
	// RacePackages get an extra `go test -race` in step 7.
	RacePackages []string
	// SelftestArgs are mantle-ui's arguments in step 8 (["selftest"]).
	SelftestArgs []string
	// Smoke configures step 9.
	Smoke SmokeConfig
	// ArchCheck replaces step 4's check. The default runs this binary's own
	// copy of the import rules (internal/archtest) over the candidate, so a
	// candidate cannot vet itself by weakening its rules.
	ArchCheck func(ctx context.Context, dir string) ([]string, error)
	// MaxErrorLines caps the error lines kept per step (20).
	MaxErrorLines int
}

func (c Config) withDefaults() Config {
	if c.Go == "" {
		c.Go = "go"
	}
	if c.Gofmt == "" {
		c.Gofmt = "gofmt"
	}
	if c.Git == "" {
		c.Git = "git"
	}
	if c.Protected == nil {
		c.Protected = ProtectedPaths
	}
	if c.UIPackage == "" {
		c.UIPackage = "./cmd/mantle-ui"
	}
	if c.RacePackages == nil {
		c.RacePackages = []string{"./internal/engine/..."}
	}
	if c.SelftestArgs == nil {
		c.SelftestArgs = []string{"selftest"}
	}
	if c.ArchCheck == nil {
		c.ArchCheck = defaultArchCheck
	}
	if c.MaxErrorLines == 0 {
		c.MaxErrorLines = 20
	}
	return c
}

func (c Config) timeout(s StepID) time.Duration {
	if d, ok := c.Timeouts[s]; ok && d > 0 {
		return d
	}
	return DefaultTimeouts[s]
}

// Run is the input of one pipeline run.
type Run struct {
	// BuildID names the run (builds/<id>/).
	BuildID string
	// Dir is the candidate worktree.
	Dir string
	// Base is the commit the candidate started from; the protected-path,
	// gofmt and apidiff steps compare against it.
	Base string
	// LogDir receives one log per step and report.json.
	LogDir string
	// OutDir receives the built mantle-ui ("" = LogDir/out).
	OutDir string
	// Steps limits the run to these steps (nil = AllSteps).
	Steps []StepID
}

// Result is one step's outcome.
type Result struct {
	Step     StepID        `json:"step"`
	OK       bool          `json:"ok"`
	Skipped  bool          `json:"skipped,omitempty"`
	TimedOut bool          `json:"timed_out,omitempty"`
	Duration time.Duration `json:"duration"`
	LogPath  string        `json:"log,omitempty"`
	// Summary is one line: why the step failed or was skipped.
	Summary string `json:"summary,omitempty"`
	// Errors are the extracted error lines (paths relative to the worktree).
	Errors []string `json:"errors,omitempty"`
	// More counts error lines dropped by MaxErrorLines.
	More int `json:"more,omitempty"`
}

// Report is a pipeline run's outcome.
type Report struct {
	BuildID  string    `json:"build_id"`
	Dir      string    `json:"dir"`
	Base     string    `json:"base"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	OK       bool      `json:"ok"`
	Results  []Result  `json:"results"`
	// Binary is the mantle-ui built by step 6, if it ran and passed.
	Binary string `json:"binary,omitempty"`
	// Changed lists the files that differ from Base.
	Changed []string `json:"changed,omitempty"`
}

// Failed returns the results of failed steps.
func (r *Report) Failed() []Result {
	var out []Result
	for _, res := range r.Results {
		if !res.OK && !res.Skipped {
			out = append(out, res)
		}
	}
	return out
}

// Result returns the result of step s, if it ran.
func (r *Report) Result(s StepID) (Result, bool) {
	for _, res := range r.Results {
		if res.Step == s {
			return res, true
		}
	}
	return Result{}, false
}

// Summary converts the report for a version manifest.
func (r *Report) Summary(buildDir string) *launcher.PipelineSummary {
	s := &launcher.PipelineSummary{OK: r.OK, BuildDir: buildDir}
	for _, res := range r.Results {
		s.Steps = append(s.Steps, launcher.StepSummary{Step: string(res.Step), OK: res.OK, Skipped: res.Skipped, DurationMS: res.Duration.Milliseconds()})
	}
	return s
}

// Pipeline runs the deterministic build checks. The model never decides
// whether a build is good: only these steps do.
type Pipeline struct {
	cfg Config
}

// NewPipeline returns a pipeline with cfg (zero fields take defaults).
func NewPipeline(cfg Config) *Pipeline { return &Pipeline{cfg: cfg.withDefaults()} }

// errSkip marks a step that had nothing to check.
type errSkip struct{ reason string }

func (e errSkip) Error() string { return "skipped: " + e.reason }

// stepFailure is a failed check with extracted error lines.
type stepFailure struct {
	summary string
	lines   []string
}

func (e *stepFailure) Error() string { return e.summary }

// Run runs the pipeline. It returns an error only if the run could not be
// set up; step failures are in the report.
func (p *Pipeline) Run(ctx context.Context, r Run) (*Report, error) {
	if r.Dir == "" || r.LogDir == "" {
		return nil, errors.New("pipeline: Dir and LogDir are required")
	}
	if err := os.MkdirAll(r.LogDir, 0o755); err != nil {
		return nil, err
	}
	if r.OutDir == "" {
		r.OutDir = filepath.Join(r.LogDir, "out")
	}
	if err := os.MkdirAll(r.OutDir, 0o755); err != nil {
		return nil, err
	}
	steps := r.Steps
	if steps == nil {
		steps = AllSteps
	}
	rep := &Report{BuildID: r.BuildID, Dir: r.Dir, Base: r.Base, Started: time.Now().UTC(), OK: true}
	if r.Base != "" {
		rep.Changed, _ = p.git(r.Dir).ChangedFiles(r.Base)
	}

	lintFailed := false
	for i, s := range AllSteps {
		if !slices.Contains(steps, s) {
			continue
		}
		inLint := slices.Contains(lintSteps, s)
		if lintFailed && !inLint {
			break // the lint group failed: stop before building
		}
		res := p.runStep(ctx, i+1, s, r, rep)
		rep.Results = append(rep.Results, res)
		if res.OK || res.Skipped {
			continue
		}
		rep.OK = false
		if inLint {
			lintFailed = true
			continue
		}
		break
	}
	rep.Finished = time.Now().UTC()
	if data, err := json.MarshalIndent(rep, "", "  "); err == nil {
		os.WriteFile(filepath.Join(r.LogDir, "report.json"), append(data, '\n'), 0o644)
	}
	return rep, nil
}

func (p *Pipeline) git(dir string) Git { return Git{Dir: dir, Bin: p.cfg.Git} }

func (p *Pipeline) runStep(ctx context.Context, n int, s StepID, r Run, rep *Report) Result {
	res := Result{Step: s, LogPath: filepath.Join(r.LogDir, fmt.Sprintf("%02d-%s.log", n, s))}
	logf, err := os.Create(res.LogPath)
	if err != nil {
		res.Summary = err.Error()
		return res
	}
	defer logf.Close()
	sctx, cancel := context.WithTimeout(ctx, p.cfg.timeout(s))
	defer cancel()
	sc := &stepCtx{p: p, r: r, rep: rep, log: logf}
	start := time.Now()
	var stepErr error
	switch s {
	case StepProtected:
		stepErr = sc.protected()
	case StepGofmt:
		stepErr = sc.gofmt(sctx)
	case StepVet:
		stepErr = sc.goCmd(sctx, nil, "vet", "./...")
	case StepArch:
		stepErr = sc.arch(sctx)
	case StepAPIDiff:
		stepErr = sc.apidiff(sctx)
	case StepBuild:
		stepErr = sc.build(sctx)
	case StepTest:
		stepErr = sc.test(sctx)
	case StepSelftest:
		stepErr = sc.selftest(sctx)
	case StepSmoke:
		stepErr = sc.smoke(sctx)
	default:
		stepErr = fmt.Errorf("unknown step %q", s)
	}
	res.Duration = time.Since(start)
	var skip errSkip
	var fail *stepFailure
	switch {
	case stepErr == nil:
		res.OK = true
	case errors.As(stepErr, &skip):
		res.Skipped = true
		res.Summary = skip.reason
		fmt.Fprintf(logf, "skipped: %s\n", skip.reason)
	case errors.As(stepErr, &fail):
		res.Summary = fail.summary
		res.Errors, res.More = capLines(relativize(fail.lines, r.Dir), p.cfg.MaxErrorLines)
	default:
		res.Summary = stepErr.Error()
	}
	if sctx.Err() == context.DeadlineExceeded && !res.OK {
		res.TimedOut = true
		res.Summary = fmt.Sprintf("timed out after %s", p.cfg.timeout(s))
	}
	if !res.OK {
		fmt.Fprintf(logf, "\nFAILED: %s\n", res.Summary)
	}
	return res
}

// stepCtx carries one step's state.
type stepCtx struct {
	p   *Pipeline
	r   Run
	rep *Report
	log io.Writer
}

// safetyEnv keeps tests and smoke boots away from the real API and the
// user's state: an isolated mantle home and claude config, and an API base
// URL that refuses connections.
func (sc *stepCtx) safetyEnv() []string {
	return []string{
		launcher.EnvHome + "=" + filepath.Join(sc.r.LogDir, "home"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(sc.r.LogDir, "claude-config"),
		"ANTHROPIC_BASE_URL=http://127.0.0.1:9",
		"ANTHROPIC_API_KEY=",
		"CLAUDE_CODE_OAUTH_TOKEN=",
	}
}

func (sc *stepCtx) env(extra []string) []string {
	return mergeEnv(os.Environ(), append(append([]string{"GOTOOLCHAIN=local", "GOFLAGS="}, sc.p.cfg.Env...), extra...))
}

// mergeEnv returns base with every KEY=VALUE of overrides replacing (or
// adding) its key.
func mergeEnv(base, overrides []string) []string {
	keys := map[string]bool{}
	for _, kv := range overrides {
		k, _, _ := strings.Cut(kv, "=")
		keys[k] = true
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !keys[k] {
			out = append(out, kv)
		}
	}
	return append(out, overrides...)
}

// exec runs a command in dir, logging the command line and its combined
// output. It returns the output (capped) and an error for a non-zero exit.
func (sc *stepCtx) exec(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	fmt.Fprintf(sc.log, "$ %s %s\n", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	buf := &cappedBuffer{max: 4 << 20}
	w := io.MultiWriter(sc.log, buf)
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(sc.log, "(%v)\n", err)
	}
	return buf.String(), err
}

func (sc *stepCtx) goCmd(ctx context.Context, extraEnv []string, args ...string) error {
	out, err := sc.exec(ctx, sc.r.Dir, sc.env(extraEnv), sc.p.cfg.Go, args...)
	if err != nil {
		return &stepFailure{summary: fmt.Sprintf("go %s: %v", args[0], err), lines: extractErrors(out)}
	}
	return nil
}

// protected is step 1.
func (sc *stepCtx) protected() error {
	if sc.r.Base == "" {
		return errors.New("no base commit to compare against")
	}
	g := sc.p.git(sc.r.Dir)
	files, err := g.ChangedFiles(sc.r.Base)
	if err != nil {
		return err
	}
	var bad []string
	for _, f := range files {
		fmt.Fprintf(sc.log, "changed: %s\n", f)
		if IsProtected(f, sc.p.cfg.Protected) {
			bad = append(bad, "protected path changed: "+f)
		}
	}
	if slices.Contains(files, "go.mod") {
		old, err := g.ShowFile(sc.r.Base, "go.mod")
		cur, err2 := os.ReadFile(filepath.Join(sc.r.Dir, "go.mod"))
		switch {
		case err != nil || err2 != nil:
			bad = append(bad, "go.mod: cannot compare the toolchain line")
		case ToolchainLine(old) != ToolchainLine(cur):
			bad = append(bad, fmt.Sprintf("go.mod: toolchain line changed from %q to %q", ToolchainLine(old), ToolchainLine(cur)))
		}
	}
	if len(bad) > 0 {
		return &stepFailure{summary: fmt.Sprintf("%d protected change(s); revert them", len(bad)), lines: bad}
	}
	return nil
}

// gofmt is step 2.
func (sc *stepCtx) gofmt(ctx context.Context) error {
	var files []string
	for _, f := range sc.rep.Changed {
		if strings.HasSuffix(f, ".go") && !strings.Contains("/"+f, "/testdata/") {
			if _, err := os.Stat(filepath.Join(sc.r.Dir, f)); err == nil {
				files = append(files, f)
			}
		}
	}
	if sc.r.Base == "" {
		return errSkip{"no base commit"}
	}
	if len(files) == 0 {
		return errSkip{"no changed Go files"}
	}
	out, err := sc.exec(ctx, sc.r.Dir, sc.env(nil), sc.p.cfg.Gofmt, append([]string{"-l"}, files...)...)
	if err != nil {
		return &stepFailure{summary: fmt.Sprintf("gofmt: %v", err), lines: extractErrors(out)}
	}
	var unformatted []string
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		if ln != "" {
			unformatted = append(unformatted, "needs gofmt: "+ln)
		}
	}
	if len(unformatted) > 0 {
		return &stepFailure{summary: fmt.Sprintf("%d file(s) need gofmt -w", len(unformatted)), lines: unformatted}
	}
	return nil
}

// arch is step 4.
func (sc *stepCtx) arch(ctx context.Context) error {
	violations, err := sc.p.cfg.ArchCheck(ctx, sc.r.Dir)
	for _, v := range violations {
		fmt.Fprintln(sc.log, v)
	}
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		return &stepFailure{summary: fmt.Sprintf("%d import rule violation(s)", len(violations)), lines: violations}
	}
	return nil
}

func defaultArchCheck(ctx context.Context, dir string) ([]string, error) {
	pkgs, err := archtest.List(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range archtest.Check(pkgs) {
		out = append(out, v.String())
	}
	return out, nil
}

// apidiff is step 5: every pkg/* package of the base must stay
// API-compatible in the candidate.
func (sc *stepCtx) apidiff(ctx context.Context) error {
	if sc.r.Base == "" {
		return errSkip{"no base commit"}
	}
	if !slices.ContainsFunc(sc.rep.Changed, func(f string) bool { return strings.HasPrefix(f, "pkg/") }) {
		return errSkip{"no changes under pkg/"}
	}
	work := filepath.Join(sc.r.LogDir, "apidiff")
	os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	tool := filepath.Join(work, "apidiff")
	if out, err := sc.exec(ctx, sc.r.Dir, sc.env(nil), sc.p.cfg.Go, "build", "-o", tool, "golang.org/x/exp/cmd/apidiff"); err != nil {
		return &stepFailure{summary: "cannot build apidiff: " + err.Error(), lines: extractErrors(out)}
	}
	baseDir := filepath.Join(work, "base")
	g := sc.p.git(sc.r.Dir)
	if err := g.WorktreeAddDetached(baseDir, sc.r.Base); err != nil {
		return err
	}
	defer g.WorktreeRemove(baseDir)

	out, err := sc.exec(ctx, baseDir, sc.env(nil), sc.p.cfg.Go, "list", "./pkg/...")
	if err != nil {
		return &stepFailure{summary: "go list ./pkg/... in the base failed", lines: extractErrors(out)}
	}
	basePkgs := strings.Fields(out)
	out, err = sc.exec(ctx, sc.r.Dir, sc.env(nil), sc.p.cfg.Go, "list", "./pkg/...")
	if err != nil {
		return &stepFailure{summary: "go list ./pkg/... failed", lines: extractErrors(out)}
	}
	candPkgs := strings.Fields(out)

	var bad []string
	for _, pkg := range basePkgs {
		if !slices.Contains(candPkgs, pkg) {
			bad = append(bad, pkg+": package removed")
			continue
		}
		export := filepath.Join(work, strings.ReplaceAll(pkg, "/", "_")+".export")
		if out, err := sc.exec(ctx, baseDir, sc.env(nil), tool, "-w", export, pkg); err != nil {
			bad = append(bad, fmt.Sprintf("%s: cannot export the base API: %s", pkg, firstLine(out)))
			continue
		}
		out, err := sc.exec(ctx, sc.r.Dir, sc.env(nil), tool, "-incompatible", export, pkg)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: apidiff failed: %s", pkg, firstLine(out)))
			continue
		}
		for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" && !strings.HasPrefix(ln, "Incompatible changes:") {
				bad = append(bad, pkg+": "+strings.TrimPrefix(ln, "- "))
			}
		}
	}
	if len(bad) > 0 {
		return &stepFailure{summary: "incompatible pkg/ API change (pkg/ is additive-only)", lines: bad}
	}
	return nil
}

// build is step 6.
func (sc *stepCtx) build(ctx context.Context) error {
	bin := filepath.Join(sc.r.OutDir, launcher.UIBinaryName)
	os.Remove(bin)
	if err := sc.goCmd(ctx, nil, "build", "-trimpath", "-o", bin, sc.p.cfg.UIPackage); err != nil {
		return err
	}
	sc.rep.Binary = bin
	return nil
}

// test is step 7.
func (sc *stepCtx) test(ctx context.Context) error {
	if err := sc.goCmd(ctx, sc.safetyEnv(), "test", "./..."); err != nil {
		return err
	}
	for _, pkg := range sc.p.cfg.RacePackages {
		dir := filepath.Join(sc.r.Dir, strings.TrimSuffix(strings.TrimPrefix(pkg, "./"), "/..."))
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		if err := sc.goCmd(ctx, sc.safetyEnv(), "test", "-race", pkg); err != nil {
			return err
		}
	}
	return nil
}

func (sc *stepCtx) binary() (string, error) {
	if sc.rep.Binary != "" {
		return sc.rep.Binary, nil
	}
	bin := filepath.Join(sc.r.OutDir, launcher.UIBinaryName)
	if _, err := os.Stat(bin); err != nil {
		return "", errors.New("no mantle-ui binary: the build step did not run")
	}
	return bin, nil
}

// selftest is step 8.
func (sc *stepCtx) selftest(ctx context.Context) error {
	bin, err := sc.binary()
	if err != nil {
		return err
	}
	out, err := sc.exec(ctx, sc.r.Dir, sc.env(sc.safetyEnv()), bin, sc.p.cfg.SelftestArgs...)
	if err != nil {
		return &stepFailure{summary: "mantle-ui selftest: " + err.Error(), lines: extractErrors(out)}
	}
	return nil
}

// errorLineRe matches compiler, vet and test failure lines.
var errorLineRe = regexp.MustCompile(`^(\S+\.go:\d+(:\d+)?: |--- FAIL|FAIL\s|panic: |fatal error: |# |\s+\S+_test\.go:\d+: |[a-z]+: .*(error|cannot|undefined))`)

// extractErrors picks the error lines out of a command's output; without any
// recognised line it falls back to the last lines.
func extractErrors(out string) []string {
	var lines, all []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		ln := strings.TrimRight(sc.Text(), " \r")
		if ln == "" {
			continue
		}
		all = append(all, ln)
		if errorLineRe.MatchString(ln) && !strings.HasPrefix(ln, "FAIL\t") || strings.HasPrefix(ln, "--- FAIL") {
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return all[max(0, len(all)-15):]
	}
	return lines
}

func relativize(lines []string, dir string) []string {
	if dir == "" {
		return lines
	}
	prefixes := []string{dir + string(filepath.Separator)}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		prefixes = append(prefixes, real+string(filepath.Separator))
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		for _, p := range prefixes {
			ln = strings.ReplaceAll(ln, p, "")
		}
		out[i] = ln
	}
	return out
}

func capLines(lines []string, n int) ([]string, int) {
	if len(lines) <= n {
		return lines, 0
	}
	return lines[:n], len(lines) - n
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// TrimForBuilder renders the failed steps of a report as compact text to
// feed back to the builder: each failed step with its summary and first
// error lines, and the log path for the rest.
func TrimForBuilder(rep *Report) string {
	if rep == nil || rep.OK {
		return ""
	}
	var b strings.Builder
	b.WriteString("The mantle pipeline rejected the change. Fix these problems:\n")
	for _, res := range rep.Failed() {
		fmt.Fprintf(&b, "\n## step %s failed: %s\n", res.Step, res.Summary)
		for _, ln := range res.Errors {
			b.WriteString("  " + ln + "\n")
		}
		if res.More > 0 {
			fmt.Fprintf(&b, "  (+%d more lines)\n", res.More)
		}
		if res.LogPath != "" {
			fmt.Fprintf(&b, "  full log: %s\n", res.LogPath)
		}
		if b.Len() > 12000 {
			b.WriteString("\n(further failures omitted)\n")
			break
		}
	}
	return b.String()
}

// cappedBuffer keeps the first max bytes written to it.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return c.buf.String() }
