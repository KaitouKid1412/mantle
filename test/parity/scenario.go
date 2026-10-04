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
//	args: --model sonnet            # extra command-line arguments for the target
//	files: fixtures/app             # copied into the working directory before start
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
type Scenario struct {
	Name   string
	Width  int
	Height int
	Script string   // fakeapi script path (absolute after Load)
	Args   []string // extra target arguments
	Files  string   // fixture directory copied into the workspace (absolute after Load)
	Steps  []Step
	Path   string // the .scn file
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
		st, err := parseStep(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		st.Line = n
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
	case "args":
		sc.Args = strings.Fields(val)
	case "files":
		sc.Files = val
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
