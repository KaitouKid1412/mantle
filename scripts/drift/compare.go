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
	// Catalog is mantle-ui's registry (native commands, renderers); nil when unavailable.
	Catalog *Catalog
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
			case KindSlash:
				fs = compareSlash(l, k)
			case KindTool:
				fs = compareTools(l, k)
			case KindProtocol:
				fs = compareProtocol(l, k.Baseline)
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

// baselineNames returns the names of a baseline list.
func baselineNames(b Snapshot, kind string) map[string]bool {
	names := map[string]bool{}
	if l := b.List(kind); l != nil {
		for _, it := range l.Items {
			names[it.Name] = true
		}
	}
	return names
}

// goneSince reports baseline items the list no longer has.
func goneSince(l List, b Snapshot) []Finding {
	have := map[string]bool{}
	for _, it := range l.Items {
		have[it.Name] = true
	}
	var fs []Finding
	for name := range baselineNames(b, l.Kind) {
		if !have[name] {
			fs = append(fs, Finding{Kind: l.Kind, Name: name, Status: StatusGone})
		}
	}
	return fs
}

// compareSlash checks the engine's commands against mantle's routing: native commands
// (catalog), PARITY.md's slash command index (E/N/H decisions) and the baseline. An
// unknown command still works: mantle sends it to the engine as typed.
func compareSlash(l List, k Known) []Finding {
	base := baselineNames(k.Baseline, l.Kind)
	var fs []Finding
	for _, it := range l.Items {
		names := []string{it.Name}
		if a := it.Attrs["aliases"]; a != "" {
			names = append(names, strings.Split(a, ",")...)
		}
		known := base[it.Name]
		for _, n := range names {
			known = known || k.Parity.Slash[n] || (k.Catalog != nil && k.Catalog.Commands[n])
		}
		if !known {
			fs = append(fs, Finding{Kind: l.Kind, Name: it.Name, Status: StatusUnclassified,
				Detail: "new command; route it (native, engine passthrough or hand-off). It is sent to the engine meanwhile"})
		}
	}
	return append(fs, goneSince(l, k.Baseline)...)
}

// compareTools checks the engine's tools against plan 03's renderers. A tool without
// its own renderer is shown with the generic one.
func compareTools(l List, k Known) []Finding {
	base := baselineNames(k.Baseline, l.Kind)
	var fs []Finding
	for _, it := range l.Items {
		if base[it.Name] || (k.Catalog != nil && k.Catalog.HasToolRenderer(it.Name)) {
			continue
		}
		detail := "new tool without its own renderer (plan 03); shown with the generic one meanwhile"
		if k.Catalog == nil {
			detail = "new tool; check plan 03 has a renderer for it (mantle-ui catalog unavailable)"
		}
		fs = append(fs, Finding{Kind: l.Kind, Name: it.Name, Status: StatusUnclassified, Detail: detail})
	}
	return append(fs, goneSince(l, k.Baseline)...)
}

// compareProtocol reads plan 02's sdk-diff output. Scope "sdk-only" means the Agent SDK
// declares a message type or control subtype that pkg/proto doesn't decode (it arrives
// as raw JSON meanwhile): drift. "proto-only" items are mantle's internal or optional
// subtypes the SDK unions leave out; they are stable, so only changes to that set are
// reported.
func compareProtocol(l List, baseline Snapshot) []Finding {
	var fs []Finding
	var protoOnly []Item
	for _, it := range l.Items {
		if it.Scope == "sdk-only" {
			fs = append(fs, Finding{Kind: l.Kind, Scope: it.Scope, Name: it.Name, Status: StatusUnclassified,
				Detail: "the SDK declares it and pkg/proto (plan 02) doesn't decode it; it is kept as raw JSON meanwhile"})
			continue
		}
		protoOnly = append(protoOnly, it)
	}
	return append(fs, compareBaseline(List{Kind: l.Kind, Items: protoOnly}, scopedTo(baseline, l.Kind, "proto-only"))...)
}

// scopedTo returns a baseline holding only the items of kind with the given scope.
func scopedTo(b Snapshot, kind, scope string) Snapshot {
	out := Snapshot{ClaudeVersion: b.ClaudeVersion}
	if l := b.List(kind); l != nil {
		nl := List{Kind: kind}
		for _, it := range l.Items {
			if it.Scope == scope {
				nl.Items = append(nl.Items, it)
			}
		}
		out.Lists = []List{nl}
	}
	return out
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
