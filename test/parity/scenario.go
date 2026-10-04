package parity

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Scenario is a scripted terminal session run identically against every target.
//
// File format (.scn): a header of "key: value" lines, a line "---", then one step per
// line. Blank lines and lines starting with '#' are ignored.
//
//	name: plain-qa
//	size: 100x30
//	script: scripts/plain-qa.json   # fakeapi script, relative to the scenario file
//	args: --model sonnet            # extra command-line arguments ('…' and "…" quote)
//	prompt: Explain this project    # positional first prompt (first start only, not restarts)
//	files: ../../../testdata/fixtures/12/app  # copied into the working directory first
//	env: CLAUDE_CODE_ENABLE_TODO_TOOLS=1   # extra environment (repeatable)
//	settings: {"statusLine": {...}}  # written to the isolated CLAUDE_CONFIG_DIR/settings.json;
//	                                 # "tui" defaults to "default" (the inline renderer)
//	parity: TR-01, PD-03            # PARITY.md rows the scenario exercises
//	---
//	ready                       # wait until the target shows its prompt
//	type What is 2+2?           # literal text ("…" quotes allow \n, \t escapes)
//	keys enter                  # keys in keybindings.json syntax, space separated
//	paste "line 1\nline 2"      # bracketed paste
//	wait_for 4                  # wait until the screen contains the text (10 s)
//	wait_for 30s Done           # … with an explicit timeout
//	wait_gone Thinking          # wait until the text is gone from the screen
//	settle                      # wait until the screen stops changing
//	checkpoint answered         # capture screen and scrollback
//	resize 120x40
//	sleep 300ms
//	restart -c                  # quit and start the target again with these arguments
//	@mantle keys enter          # a step for one target only (not checkpoints); the
//	                            # report lists these, since they are real differences
//
// fakeapi scripts may use {{work}}, {{config}} and {{home}}; the runner replaces them
// with the run's directories (tools need absolute paths).
type Scenario struct {
	Name     string
	Width    int
	Height   int
	Script   string   // fakeapi script path (absolute after Load)
	Args     []string // extra target arguments
	Prompt   string   // positional first prompt, for the first start only
	Files    string   // fixture directory copied into the workspace (absolute after Load)
	Env      []string // extra KEY=VALUE environment
	Settings string   // JSON written to CLAUDE_CONFIG_DIR/settings.json
	Parity   []string // PARITY.md row IDs this scenario covers
	Steps    []Step
	Path     string // the .scn file
}

// StepKind names a step.
type StepKind string

const (
	StepReady      StepKind = "ready"
	StepType       StepKind = "type"
	StepKeys       StepKind = "keys"
	StepPaste      StepKind = "paste"
	StepWaitFor    StepKind = "wait_for"
	StepWaitGone   StepKind = "wait_gone"
	StepSettle     StepKind = "settle"
	StepCheckpoint StepKind = "checkpoint"
	StepResize     StepKind = "resize"
	StepSleep      StepKind = "sleep"
	StepRestart    StepKind = "restart"
)

// Step is one scenario step.
type Step struct {
	Kind    StepKind
	Text    string        // type, paste, wait_for, wait_gone, checkpoint
	Keys    []string      // keys
	Timeout time.Duration // wait_for, wait_gone, ready (0 = default)
	Width   int           // resize
	Height  int           // resize
	Dur     time.Duration // sleep
	Args    []string      // restart
	Only    string        // run on this target only ("" = every target)
	Src     string        // the step as written (without @target), for reports
	Line    int           // line in the file, for errors
}

// DefaultWait is the timeout for wait steps without an explicit one.
const DefaultWait = 10 * time.Second

// LoadScenario reads and parses a .scn file; relative paths resolve against its dir.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc, err := ParseScenario(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sc.Path = path
	dir := filepath.Dir(path)
	if sc.Script != "" && !filepath.IsAbs(sc.Script) {
		sc.Script = filepath.Join(dir, sc.Script)
	}
	if sc.Files != "" && !filepath.IsAbs(sc.Files) {
		sc.Files = filepath.Join(dir, sc.Files)
	}
	if sc.Name == "" {
		sc.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return sc, nil
}

// LoadScenarios loads every .scn file in dir, sorted by name.
func LoadScenarios(dir string) ([]*Scenario, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.scn"))
	if err != nil {
		return nil, err
	}
	var out []*Scenario
	for _, m := range matches {
		sc, err := LoadScenario(m)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, nil
}

// ParseScenario parses scenario text.
func ParseScenario(text string) (*Scenario, error) {
	sc := &Scenario{Width: 100, Height: 30}
	inSteps := false
	sawHeaderEnd := false
	sc2 := bufio.NewScanner(strings.NewReader(text))
	n := 0
	for sc2.Scan() {
		n++
		line := strings.TrimSpace(stripComment(sc2.Text()))
		if line == "" {
			continue
		}
		if line == "---" {
			if sawHeaderEnd {
				return nil, fmt.Errorf("line %d: second ---", n)
			}
			inSteps, sawHeaderEnd = true, true
			continue
		}
		if !inSteps {
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				return nil, fmt.Errorf("line %d: header lines are \"key: value\" (missing --- before steps?)", n)
			}
			if err := sc.setHeader(strings.TrimSpace(key), strings.TrimSpace(val)); err != nil {
				return nil, fmt.Errorf("line %d: %w", n, err)
			}
			continue
		}
		only := ""
		if strings.HasPrefix(line, "@") {
			tgt, rest, _ := strings.Cut(line[1:], " ")
			if tgt == "" || strings.TrimSpace(rest) == "" {
				return nil, fmt.Errorf("line %d: @target needs a target and a step", n)
			}
			only, line = tgt, strings.TrimSpace(rest)
		}
		st, err := parseStep(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if only != "" && st.Kind == StepCheckpoint {
			return nil, fmt.Errorf("line %d: checkpoints run on every target", n)
		}
		st.Line, st.Only, st.Src = n, only, line
		sc.Steps = append(sc.Steps, st)
	}
	if !sawHeaderEnd {
		return nil, fmt.Errorf("missing --- between header and steps")
	}
	if len(sc.Steps) == 0 {
		return nil, fmt.Errorf("no steps")
	}
	seen := map[string]bool{}
	for _, st := range sc.Steps {
		if st.Kind == StepCheckpoint {
			if seen[st.Text] {
				return nil, fmt.Errorf("line %d: duplicate checkpoint %q", st.Line, st.Text)
			}
			seen[st.Text] = true
		}
	}
	return sc, nil
}

func (sc *Scenario) setHeader(key, val string) error {
	switch key {
	case "name":
		sc.Name = val
	case "size":
		w, h, err := parseSize(val)
		if err != nil {
			return err
		}
		sc.Width, sc.Height = w, h
	case "script":
		sc.Script = val
	case "prompt":
		sc.Prompt = val
	case "args":
		args, err := splitArgs(val)
		if err != nil {
			return err
		}
		sc.Args = args
	case "files":
		sc.Files = val
	case "env":
		if !strings.Contains(val, "=") {
			return fmt.Errorf("env wants KEY=VALUE")
		}
		sc.Env = append(sc.Env, val)
	case "settings":
		sc.Settings = val
	case "parity":
		for _, id := range strings.FieldsFunc(val, func(r rune) bool { return r == ',' || r == ' ' }) {
			sc.Parity = append(sc.Parity, id)
		}
	default:
		return fmt.Errorf("unknown header %q", key)
	}
	return nil
}

// stripComment removes a " # comment" tail (and whole-line comments). A '#' inside a
// quoted argument or not preceded by a space is kept.
func stripComment(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote && i > 0 && line[i-1] == ' ' {
				return line[:i]
			}
		}
	}
	return line
}

func parseStep(line string) (Step, error) {
	verb, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	st := Step{Kind: StepKind(verb)}
	var err error
	switch st.Kind {
	case StepReady, StepSettle:
		if rest != "" {
			st.Timeout, err = time.ParseDuration(rest)
		}
	case StepType, StepPaste:
		st.Text, err = unquote(rest)
		if err == nil && st.Text == "" {
			err = fmt.Errorf("%s needs text", verb)
		}
	case StepKeys:
		st.Keys = strings.Fields(rest)
		if len(st.Keys) == 0 {
			err = fmt.Errorf("keys needs at least one key")
		}
	case StepWaitFor, StepWaitGone:
		if first, tail, ok := strings.Cut(rest, " "); ok {
			if d, derr := time.ParseDuration(first); derr == nil {
				st.Timeout, rest = d, strings.TrimSpace(tail)
			}
		}
		st.Text, err = unquote(rest)
		if err == nil && st.Text == "" {
			err = fmt.Errorf("%s needs text", verb)
		}
	case StepCheckpoint:
		st.Text = rest
		if rest == "" || strings.ContainsAny(rest, "/\\ ") {
			err = fmt.Errorf("checkpoint needs a name without spaces or slashes")
		}
	case StepResize:
		st.Width, st.Height, err = parseSize(rest)
	case StepSleep:
		st.Dur, err = time.ParseDuration(rest)
	case StepRestart:
		st.Args, err = splitArgs(rest)
	default:
		err = fmt.Errorf("unknown step %q", verb)
	}
	return st, err
}

func unquote(s string) (string, error) {
	if strings.HasPrefix(s, `"`) {
		return strconv.Unquote(s)
	}
	return s, nil
}

func parseSize(s string) (int, int, error) {
	ws, hs, ok := strings.Cut(strings.ToLower(s), "x")
	w, err1 := strconv.Atoi(strings.TrimSpace(ws))
	h, err2 := strconv.Atoi(strings.TrimSpace(hs))
	if !ok || err1 != nil || err2 != nil || w < 20 || h < 5 || w > 500 || h > 300 {
		return 0, 0, fmt.Errorf("bad size %q (want WxH, at least 20x5)", s)
	}
	return w, h, nil
}

// splitArgs splits a command line into words like a shell: whitespace separates,
// '…' quotes literally, "…" quotes with \" and \\ escapes.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			if inWord {
				args, inWord = append(args, cur.String()), false
				cur.Reset()
			}
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, fmt.Errorf("unterminated ' in %q", s)
			}
			cur.WriteString(s[i+1 : i+1+end])
			i, inWord = i+1+end, true
		case c == '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated \" in %q", s)
			}
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
