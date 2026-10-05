package parity

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// AllowEntry accepts a known, intentional difference (mantle's own wording, extra
// mantle features). Scenario and Checkpoint "" or "*" match any.
type AllowEntry struct {
	Scenario   string
	Checkpoint string
	Pattern    string // regular expression matched against a differing line, either side
	Reason     string
	re         *regexp.Regexp
}

// Allowlist is the set of accepted differences.
type Allowlist struct {
	Entries []AllowEntry
}

// ParseAllowlist reads lines "scenario | checkpoint | pattern | reason"; '#' starts a
// comment line.
func ParseAllowlist(text string) (*Allowlist, error) {
	a := &Allowlist{}
	sc := bufio.NewScanner(strings.NewReader(text))
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " | ", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("allowlist line %d: want \"scenario | checkpoint | pattern | reason\"", n)
		}
		e := AllowEntry{
			Scenario: strings.TrimSpace(parts[0]), Checkpoint: strings.TrimSpace(parts[1]),
			Pattern: strings.TrimSpace(parts[2]), Reason: strings.TrimSpace(parts[3]),
		}
		if e.Reason == "" {
			return nil, fmt.Errorf("allowlist line %d: a reason is required", n)
		}
		re, err := regexp.Compile(e.Pattern)
		if err != nil {
			return nil, fmt.Errorf("allowlist line %d: %w", n, err)
		}
		e.re = re
		a.Entries = append(a.Entries, e)
	}
	return a, sc.Err()
}

// LoadAllowlist reads an allowlist file; a missing file is an empty list.
func LoadAllowlist(path string) (*Allowlist, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Allowlist{}, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseAllowlist(string(data))
}

func matchAny(field, v string) bool { return field == "" || field == "*" || field == v }

// Covers reports whether every differing line between left and right (the lines a line
// diff marks as added or removed) is explained by an entry for this scenario and
// checkpoint, and returns the reasons used.
func (a *Allowlist) Covers(scenario, checkpoint string, left, right []string) ([]string, bool) {
	if a == nil || len(a.Entries) == 0 {
		return nil, false
	}
	var entries []AllowEntry
	for _, e := range a.Entries {
		if matchAny(e.Scenario, scenario) && matchAny(e.Checkpoint, checkpoint) {
			entries = append(entries, e)
		}
	}
	var reasons []string
	onlyL, onlyR := diffLines(left, right)
	for _, line := range append(onlyL, onlyR...) {
		if strings.TrimSpace(line) == "" {
			continue // blank lines move with the text around them
		}
		covered := false
		for _, e := range entries {
			if e.re.MatchString(line) {
				covered = true
				if !slices.Contains(reasons, e.Reason) {
					reasons = append(reasons, e.Reason)
				}
				break
			}
		}
		if !covered {
			return nil, false
		}
	}
	return reasons, true
}
