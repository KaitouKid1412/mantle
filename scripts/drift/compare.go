package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

// Finding statuses.
const (
	// Unclassified: claude has it and mantle doesn't say how it's handled. Fails.
	StatusUnclassified = "unclassified"
	// Mismatch: both know it, but they disagree (arity, spellings). Fails.
	StatusMismatch = "mismatch"
	// Removed: mantle's table has it, claude no longer does. Stale, but harmless.
	StatusRemoved = "removed"
	// New: new since the accepted baseline, and handled without mantle changes
	// (subcommand flags pass through to claude). Informational.
	StatusNew = "new"
	// Gone: in the baseline, no longer in claude. Informational.
	StatusGone = "gone"
)

// Finding is one difference.
type Finding struct {
	Kind   string `json:"kind"`
	Scope  string `json:"scope,omitempty"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Fails reports whether the finding makes `make drift` fail.
func (f Finding) Fails() bool {
	return f.Status == StatusUnclassified || f.Status == StatusMismatch
}

// Known is what mantle already knows: its own tables, the PARITY.md indexes and the
// accepted baseline.
type Known struct {
	Flags       []cli.Flag
	Subcommands []cli.Subcommand
	Actions     []string // plan 01's keymap table (ext.ClaudeActions)
	Contexts    []string // plan 01's contexts (ext.Contexts)
	Parity      ParityIndex
	Baseline    Snapshot
}

// Summary counts one list's findings.
type Summary struct {
	Kind         string `json:"kind"`
	Source       string `json:"source"`
	Items        int    `json:"items"`
	Error        string `json:"error,omitempty"`
	Unclassified int    `json:"unclassified"`
	Mismatch     int    `json:"mismatch"`
	Removed      int    `json:"removed"`
	New          int    `json:"new"`
	Gone         int    `json:"gone"`
}

// Report is the comparator's output.
type Report struct {
	ClaudeVersion   string    `json:"claude_version"`
	BaselineVersion string    `json:"baseline_version"`
	Generated       string    `json:"generated"`
	Summary         []Summary `json:"summary"`
	Findings        []Finding `json:"findings"`
}

// Failed reports whether any finding fails the run.
func (r *Report) Failed() bool {
	return slices.ContainsFunc(r.Findings, Finding.Fails)
}

// Compare diffs a snapshot against what mantle knows.
func Compare(s Snapshot, k Known) Report {
	r := Report{ClaudeVersion: s.ClaudeVersion, BaselineVersion: k.Baseline.ClaudeVersion}
	for _, l := range s.Lists {
		var fs []Finding
		if l.Available() {
			switch l.Kind {
			case KindFlag:
				fs = compareFlags(l, k)
			case KindSubcommand:
				fs = compareSubcommands(l, k)
			case KindAction:
				fs = compareNames(l, k.Actions, "plan 01's keymap table (pkg/ext)")
			case KindContext:
				fs = compareNames(l, k.Contexts, "plan 01's contexts (pkg/ext)")
			case KindSetting:
				fs = compareSettings(l, k)
			default:
				fs = compareBaseline(l, k.Baseline)
			}
		}
		sum := Summary{Kind: l.Kind, Source: l.Source, Items: len(l.Items), Error: l.Error}
		for _, f := range fs {
			switch f.Status {
			case StatusUnclassified:
				sum.Unclassified++
			case StatusMismatch:
				sum.Mismatch++
			case StatusRemoved:
				sum.Removed++
			case StatusNew:
				sum.New++
			case StatusGone:
				sum.Gone++
			}
		}
		r.Summary = append(r.Summary, sum)
		r.Findings = append(r.Findings, fs...)
	}
	slices.SortStableFunc(r.Findings, func(a, b Finding) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Status, b.Status),
			strings.Compare(a.Scope, b.Scope), strings.Compare(a.Name, b.Name))
	})
	return r
}

func flagNames(names ...string) []string {
	var out []string
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

func itemFlagNames(it Item) []string {
	names := []string{it.Name, it.Attrs["short"]}
	if a := it.Attrs["aliases"]; a != "" {
		names = append(names, strings.Split(a, ",")...)
	}
	return flagNames(names...)
}

// helpArity is how claude --help shows a table arity.
func helpArity(a cli.Arity) string {
	switch a {
	case cli.ArityRequired, cli.ArityRepeatable:
		return "required"
	case cli.ArityOptional:
		return "optional"
	case cli.ArityVariadic:
		return "variadic"
	}
	return "none"
}

func compareFlags(l List, k Known) []Finding {
	var fs []Finding
	index := map[string]*cli.Flag{}
	for i := range k.Flags {
		for _, n := range k.Flags[i].Names() {
			index[n] = &k.Flags[i]
		}
	}
	rootSeen := map[string]bool{}
	var subItems []Item
	for _, it := range l.Items {
		if it.Scope != "" {
			subItems = append(subItems, it)
			continue
		}
		var f *cli.Flag
		for _, n := range itemFlagNames(it) {
			if f = index[n]; f != nil {
				break
			}
		}
		if f == nil {
			fs = append(fs, Finding{Kind: KindFlag, Name: it.Name, Status: StatusUnclassified,
				Detail: fmt.Sprintf("new %s flag; add it to internal/cli/flags.go with a class (forwarded meanwhile)", it.Attrs["arity"])})
			continue
		}
		rootSeen[f.Long] = true
		var diffs []string
		if got, want := it.Attrs["arity"], helpArity(f.Arity); got != want {
			diffs = append(diffs, fmt.Sprintf("arity is %s, table says %s", got, want))
		}
		if got, want := itemFlagNames(it), flagNames(f.Names()...); !slices.Equal(got, want) {
			diffs = append(diffs, fmt.Sprintf("spellings are %s, table says %s", strings.Join(got, " "), strings.Join(want, " ")))
		}
		if f.Hidden {
			diffs = append(diffs, "now listed by --help; clear Hidden")
		}
		if len(diffs) > 0 {
			fs = append(fs, Finding{Kind: KindFlag, Name: it.Name, Status: StatusMismatch, Detail: strings.Join(diffs, "; ")})
		}
	}
	for _, f := range k.Flags {
		if !f.Hidden && !f.Mantle && !rootSeen[f.Long] {
			fs = append(fs, Finding{Kind: KindFlag, Name: f.Long, Status: StatusRemoved, Detail: "no longer listed by claude --help"})
		}
	}
	return append(fs, compareBaseline(List{Kind: KindFlag, Items: subItems}, scoped(k.Baseline, KindFlag))...)
}

// scoped returns a baseline holding only the subcommand-scoped items of kind.
func scoped(b Snapshot, kind string) Snapshot {
	out := Snapshot{ClaudeVersion: b.ClaudeVersion}
	if l := b.List(kind); l != nil {
		nl := List{Kind: kind}
		for _, it := range l.Items {
			if it.Scope != "" {
				nl.Items = append(nl.Items, it)
			}
		}
		out.Lists = []List{nl}
	}
	return out
}

func compareSubcommands(l List, k Known) []Finding {
	var fs []Finding
	index := map[string]*cli.Subcommand{}
	for i := range k.Subcommands {
		index[k.Subcommands[i].Name] = &k.Subcommands[i]
	}
	seen := map[string]bool{}
	var nested []Item
	for _, it := range l.Items {
		if it.Scope != "" {
			nested = append(nested, it)
			continue
		}
		s := index[it.Name]
		if s == nil {
			fs = append(fs, Finding{Kind: KindSubcommand, Name: it.Name, Status: StatusUnclassified,
				Detail: "new subcommand; add it to internal/cli/subcommands.go and the launcher's copy (until then `mantle " + it.Name + "` is a prompt)"})
			continue
		}
		seen[it.Name] = true
		var aliases []string
		if a := it.Attrs["aliases"]; a != "" {
			aliases = strings.Split(a, ",")
		}
		if want := slices.Sorted(slices.Values(s.Aliases)); !slices.Equal(slices.Sorted(slices.Values(aliases)), want) && it.Attrs["hidden"] == "" {
			fs = append(fs, Finding{Kind: KindSubcommand, Name: it.Name, Status: StatusMismatch,
				Detail: fmt.Sprintf("aliases are [%s], table says [%s]", strings.Join(aliases, " "), strings.Join(s.Aliases, " "))})
		}
		if s.Hidden && it.Attrs["hidden"] == "" {
			fs = append(fs, Finding{Kind: KindSubcommand, Name: it.Name, Status: StatusMismatch, Detail: "now listed by --help; clear Hidden"})
		}
	}
	for _, s := range k.Subcommands {
		if !seen[s.Name] && !s.NoHelpProbe {
			fs = append(fs, Finding{Kind: KindSubcommand, Name: s.Name, Status: StatusRemoved, Detail: "claude no longer has it"})
		}
	}
	return append(fs, compareBaseline(List{Kind: KindSubcommand, Items: nested}, scoped(k.Baseline, KindSubcommand))...)
}

// compareNames checks a flat list against one of mantle's tables.
func compareNames(l List, table []string, tableName string) []Finding {
	var fs []Finding
	have := map[string]bool{}
	for _, it := range l.Items {
		have[it.Name] = true
		if !slices.Contains(table, it.Name) {
			fs = append(fs, Finding{Kind: l.Kind, Name: it.Name, Status: StatusUnclassified, Detail: "not in " + tableName})
		}
	}
	for _, n := range table {
		if !have[n] {
			fs = append(fs, Finding{Kind: l.Kind, Name: n, Status: StatusRemoved, Detail: "in " + tableName + " but not found in claude"})
		}
	}
	return fs
}

// compareSettings: a key is classified when PARITY.md's UI settings index lists it, or
// when it was accepted into the baseline (keys the engine handles by itself). PARITY.md
// keys missing from the schema are not reported: some are global-config keys, and the
// community schema can lag behind the binary.
func compareSettings(l List, k Known) []Finding {
	var fs []Finding
	base := map[string]bool{}
	if bl := k.Baseline.List(KindSetting); bl != nil {
		for _, it := range bl.Items {
			base[it.Name] = true
		}
	}
	have := map[string]bool{}
	for _, it := range l.Items {
		have[it.Name] = true
		top, _, _ := strings.Cut(it.Name, ".")
		if !base[it.Name] && !k.Parity.Settings[it.Name] && !k.Parity.Settings[top] {
			fs = append(fs, Finding{Kind: KindSetting, Name: it.Name, Status: StatusUnclassified,
				Detail: "new settings key; decide whether mantle's UI reads it, then accept the baseline"})
		}
	}
	for key := range base {
		if !have[key] {
			fs = append(fs, Finding{Kind: KindSetting, Name: key, Status: StatusGone, Detail: "in the baseline, no longer in the schema"})
		}
	}
	return fs
}

// compareBaseline reports items added or dropped since the accepted baseline.
func compareBaseline(l List, baseline Snapshot) []Finding {
	bl := baseline.List(l.Kind)
	if bl == nil {
		return nil
	}
	var fs []Finding
	old := map[string]bool{}
	for _, it := range bl.Items {
		old[it.Key()] = true
	}
	now := map[string]bool{}
	for _, it := range l.Items {
		now[it.Key()] = true
		if !old[it.Key()] {
			fs = append(fs, Finding{Kind: l.Kind, Scope: it.Scope, Name: it.Name, Status: StatusNew})
		}
	}
	for _, it := range bl.Items {
		if !now[it.Key()] {
			fs = append(fs, Finding{Kind: l.Kind, Scope: it.Scope, Name: it.Name, Status: StatusGone})
		}
	}
	return fs
}
