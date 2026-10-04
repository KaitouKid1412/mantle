package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// Probe markers, planted in the probe's temp project.
const (
	probeSkill   = "mantle-probe"
	probeHookCmd = "echo mantle-probe"
)

// Check is one conformance check.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// ProbeResult is the outcome of a conformance probe.
type ProbeResult struct {
	Version  string        `json:"version"`
	Binary   string        `json:"binary"`
	OK       bool          `json:"ok"`
	Checks   []Check       `json:"checks"`
	Duration time.Duration `json:"duration"`
}

// Failed returns the names of failed checks.
func (r ProbeResult) Failed() []string {
	var out []string
	for _, c := range r.Checks {
		if !c.OK {
			out = append(out, c.Name)
		}
	}
	return out
}

// ProbeOptions configure Probe.
type ProbeOptions struct {
	Binary  string  // "" = FindBinary
	Spawner Spawner // nil = ExecSpawner (tests use enginetest)
	Dir     string  // project dir to plant markers in; "" = a temp dir
	Env     map[string]string
	Unset   []string
	Timeout time.Duration // whole probe; 0 = 90 s
}

// Probe runs the zero-token conformance probe (spike S6): it starts the engine in a
// project with a CLAUDE.md, a project hook and a project skill, then checks through
// control requests that the engine sees them (a --bare engine would not), that it
// lists commands and models, and that it answers end_session. No prompt is sent, so
// no API call is made.
func Probe(ctx context.Context, o ProbeOptions) (ProbeResult, error) {
	start := time.Now()
	if o.Timeout == 0 {
		o.Timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	res := ProbeResult{Binary: o.Binary}
	if res.Binary == "" {
		bin, err := FindBinary()
		if err != nil {
			return res, err
		}
		res.Binary = bin
	}
	dir := o.Dir
	if dir == "" {
		d, err := os.MkdirTemp("", "mantle-probe-")
		if err != nil {
			return res, err
		}
		defer os.RemoveAll(d)
		dir = d
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if err := plantProbeProject(dir); err != nil {
		return res, err
	}

	initDone := make(chan ext.ControlResultMsg, 1)
	m := NewManager(func(msg tea.Msg) {
		if r, ok := msg.(ext.ControlResultMsg); ok && r.Subtype == proto.SubInitialize {
			select {
			case initDone <- r:
			default:
			}
		}
	})
	m.Binary, m.Spawner, m.RunDir = res.Binary, o.Spawner, "-"
	e, err := m.Start("probe", ext.SpawnOpts{Cwd: dir, Env: o.Env, UnsetEnv: o.Unset,
		ExtraArgs: []string{"--no-session-persistence"}})
	if err != nil {
		return res, err
	}
	defer m.Close(context.Background())

	add := func(name string, ok bool, detail string, args ...any) {
		res.Checks = append(res.Checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(detail, args...)})
	}
	var ir proto.InitializeResponse
	select {
	case r := <-initDone:
		if r.Err != nil {
			add("initialize", false, "%v", r.Err)
		} else if err := json.Unmarshal(r.Resp, &ir); err != nil {
			add("initialize", false, "decode: %v", err)
		} else {
			add("initialize", true, "%d commands, %d models", len(ir.Commands), len(ir.Models))
		}
	case <-ctx.Done():
		add("initialize", false, "no reply: %v (stderr: %s)", ctx.Err(), e.StderrTail(5))
		res.Duration = time.Since(start)
		return res, nil
	}

	req := func(r proto.Request, v any) error {
		raw, err := e.Request(ctx, r)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, v)
	}

	var ver proto.BinaryVersion
	if err := req(proto.GetBinaryVersionRequest{}, &ver); err != nil {
		add("version", false, "%v", err)
	} else {
		res.Version = ver.Version
		add("version", ver.Version != "", "%s", ver.Version)
	}

	add("commands", len(ir.Commands) > 0, "%d commands", len(ir.Commands))
	skill := false
	for _, c := range ir.Commands {
		if c.Name == probeSkill {
			skill = true
		}
	}
	add("skills", skill, "project skill %q %s", probeSkill, foundWord(skill))

	var models proto.ModelsResponse
	if err := req(proto.ListModelsRequest{}, &models); err != nil {
		add("models", false, "%v", err)
	} else {
		add("models", len(models.Models) > 0, "%d models", len(models.Models))
	}

	var hooks proto.HooksListing
	if err := req(proto.GetHooksListingRequest{}, &hooks); err != nil {
		add("hooks", false, "%v", err)
	} else {
		hook := false
		for _, h := range hooks.Hooks {
			if strings.Contains(h.DisplayText, probeHookCmd) {
				hook = true
			}
		}
		add("hooks", hook, "project hook %s", foundWord(hook))
	}

	var cu proto.ContextUsage
	if err := req(proto.GetContextUsageRequest{}, &cu); err != nil {
		add("memory", false, "%v", err)
	} else {
		var files []struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(cu.MemoryFiles, &files)
		mem := false
		for _, f := range files {
			if f.Path == filepath.Join(dir, "CLAUDE.md") {
				mem = true
			}
		}
		add("memory", mem, "project CLAUDE.md %s", foundWord(mem))
	}

	_, err = e.Request(ctx, proto.EndSessionRequest{Reason: "mantle conformance probe"})
	add("end_session", err == nil || errors.Is(err, ErrExited), "%v", errOrOK(err))

	res.OK = len(res.Failed()) == 0
	res.Duration = time.Since(start)
	return res, nil
}

func foundWord(ok bool) string {
	if ok {
		return "seen"
	}
	return "missing (is the engine running in --bare mode?)"
}

func errOrOK(err error) any {
	if err == nil {
		return "ok"
	}
	return err
}

// plantProbeProject writes the markers the probe looks for.
func plantProbeProject(dir string) error {
	files := map[string]string{
		"CLAUDE.md":             "# mantle conformance probe\n\nThis directory only exists for a few seconds.\n",
		".claude/settings.json": `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"` + probeHookCmd + `"}]}]}}` + "\n",
		".claude/skills/" + probeSkill + "/SKILL.md": "---\nname: " + probeSkill + "\n" +
			"description: Marker skill for mantle's engine conformance probe; never invoked.\n---\n\nDo nothing.\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}
