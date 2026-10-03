package perms

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Behavior is what a rule does when it matches.
type Behavior string

const (
	Allow Behavior = "allow"
	Ask   Behavior = "ask"
	Deny  Behavior = "deny"
)

// Behaviors lists the rule lists in panel tab order.
var Behaviors = []Behavior{Allow, Ask, Deny}

func (b Behavior) valid() bool { return b == Allow || b == Ask || b == Deny }

// AutoList names one list inside the autoMode settings object.
type AutoList string

const (
	AutoAllow       AutoList = "allow"
	AutoSoftDeny    AutoList = "soft_deny"
	AutoHardDeny    AutoList = "hard_deny"
	AutoEnvironment AutoList = "environment"
)

// AutoLists lists the autoMode lists in display order.
var AutoLists = []AutoList{AutoAllow, AutoSoftDeny, AutoHardDeny, AutoEnvironment}

func (l AutoList) valid() bool {
	for _, a := range AutoLists {
		if a == l {
			return true
		}
	}
	return false
}

// Summary is the engine's own plain-language reading of a rule, split so the rule
// fragment (Emphasis) can be styled. Only rules from list_permission_rules carry one.
type Summary struct {
	Prefix, Emphasis, Suffix string
}

// Entry is one rule in the merged view.
type Entry struct {
	Raw      string      // as written in the settings file
	Rule     Rule        // valid when Err is nil
	Err      error       // parse error; the entry is still shown and removable
	Scope    patch.Scope // where it came from (engine sources such as "session" are kept as is)
	ReadOnly bool        // managed, flag or session rules cannot be edited here

	// Filled only from list_permission_rules.
	Summary     *Summary
	NotInEffect bool
}

// Describe prefers the engine's summary and falls back to Rule.Describe.
func (e Entry) Describe() string {
	if e.Summary != nil {
		return e.Summary.Prefix + e.Summary.Emphasis + e.Summary.Suffix
	}
	if e.Err != nil {
		return "Unreadable rule: " + e.Err.Error()
	}
	return e.Rule.Describe()
}

// Dir is one additional working directory.
type Dir struct {
	Path     string
	Scope    patch.Scope
	ReadOnly bool
}

// Value is a single setting resolved across scopes.
type Value struct {
	Value string
	Scope patch.Scope // the scope that set it
	Set   bool
}

// AutoEntry is one autoMode rule or environment line.
type AutoEntry struct {
	Text     string
	Scope    patch.Scope
	ReadOnly bool
}

// View is every permission setting merged across scopes. Lists are ordered by scope
// precedence (managed first), then by position in the file.
type View struct {
	Allow, Ask, Deny []Entry
	Directories      []Dir
	DefaultMode      Value
	DisableBypass    Value // permissions.disableBypassPermissionsMode
	AutoMode         map[AutoList][]AutoEntry

	// Filled only from list_permission_rules.
	OriginalCwd string
	ManagedOnly bool
}

// Rules returns the list for b.
func (v View) Rules(b Behavior) []Entry {
	switch b {
	case Allow:
		return v.Allow
	case Ask:
		return v.Ask
	case Deny:
		return v.Deny
	}
	return nil
}

func (v *View) add(b Behavior, e Entry) {
	switch b {
	case Allow:
		v.Allow = append(v.Allow, e)
	case Ask:
		v.Ask = append(v.Ask, e)
	case Deny:
		v.Deny = append(v.Deny, e)
	}
}

func newEntry(raw string, scope patch.Scope) Entry {
	r, err := Parse(raw)
	return Entry{Raw: raw, Rule: r, Err: err, Scope: scope, ReadOnly: !scope.Writable()}
}

// byPrecedence returns the scopes present in docs, highest precedence first.
func byPrecedence(docs map[patch.Scope]map[string]any) []patch.Scope {
	scopes := make([]patch.Scope, 0, len(docs))
	for s := range docs {
		scopes = append(scopes, s)
	}
	sort.SliceStable(scopes, func(i, j int) bool {
		pi, pj := scopes[i].Precedence(), scopes[j].Precedence()
		if pi != pj {
			return pi > pj
		}
		return scopes[i] < scopes[j]
	})
	return scopes
}

// Merge builds the view from per-scope settings documents (decoded settings files).
func Merge(docs map[patch.Scope]map[string]any) View {
	v := View{AutoMode: map[AutoList][]AutoEntry{}}
	for _, scope := range byPrecedence(docs) {
		doc := docs[scope]
		for _, b := range Behaviors {
			seen := map[string]bool{}
			for _, raw := range strings_(doc, "permissions", string(b)) {
				if seen[raw] {
					continue
				}
				seen[raw] = true
				v.add(b, newEntry(raw, scope))
			}
		}
		seen := map[string]bool{}
		for _, d := range strings_(doc, "permissions", "additionalDirectories") {
			if seen[d] {
				continue
			}
			seen[d] = true
			v.Directories = append(v.Directories, Dir{Path: d, Scope: scope, ReadOnly: !scope.Writable()})
		}
		if !v.DefaultMode.Set {
			if s, ok := stringAt(doc, "permissions", "defaultMode"); ok {
				v.DefaultMode = Value{Value: s, Scope: scope, Set: true}
			}
		}
		if !v.DisableBypass.Set {
			if s, ok := stringAt(doc, "permissions", "disableBypassPermissionsMode"); ok {
				v.DisableBypass = Value{Value: s, Scope: scope, Set: true}
			}
		}
		for _, l := range AutoLists {
			seen := map[string]bool{}
			for _, t := range strings_(doc, "autoMode", string(l)) {
				if seen[t] {
					continue
				}
				seen[t] = true
				v.AutoMode[l] = append(v.AutoMode[l], AutoEntry{Text: t, Scope: scope, ReadOnly: !scope.Writable()})
			}
		}
	}
	return v
}

// strings_ returns the array at path as strings. Non-string elements are rendered as
// JSON so nothing in the file is silently hidden.
func strings_(doc map[string]any, path ...string) []string {
	raw, ok := patch.Get(doc, path...)
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
			continue
		}
		b, err := json.Marshal(e)
		if err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

func stringAt(doc map[string]any, path ...string) (string, bool) {
	raw, ok := patch.Get(doc, path...)
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	return s, ok
}

// ---- list_permission_rules ----

// listState is the {state} payload of list_permission_rules as observed in Claude Code
// 2.1.288. The request is documented but its response type is not published, so this
// decoder is provisional: it accepts that shape, plus plain allow/ask/deny arrays, and
// errors on anything else so callers can fall back to reading settings files.
type listState struct {
	Rules []struct {
		Behavior    string          `json:"behavior"`
		Source      string          `json:"source"`
		Rule        string          `json:"rule"`
		RuleValue   json.RawMessage `json:"ruleValue"`
		Description *struct {
			Prefix   string `json:"prefix"`
			Emphasis string `json:"emphasis"`
			Suffix   string `json:"suffix"`
		} `json:"description"`
		Editability string `json:"editability"`
		NotInEffect bool   `json:"notInEffect"`
	} `json:"rules"`
	WorkspaceDirectories []struct {
		Path   string `json:"path"`
		Source string `json:"source"`
	} `json:"workspaceDirectories"`
	OriginalCwd string `json:"originalCwd"`
	ManagedOnly bool   `json:"managedOnly"`

	Allow []json.RawMessage `json:"allow"`
	Ask   []json.RawMessage `json:"ask"`
	Deny  []json.RawMessage `json:"deny"`
}

// ErrUnknownShape is returned when a list_permission_rules response has none of the
// fields this decoder understands.
var ErrUnknownShape = errors.New("unrecognised list_permission_rules response")

// FromListResponse decodes a list_permission_rules response (either the whole
// {"state": …} payload or the state object) into a View. Default mode and auto-mode
// rules are not part of that response; take them from Merge.
func FromListResponse(raw json.RawMessage) (View, error) {
	var wrapper struct {
		State json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return View{}, fmt.Errorf("list_permission_rules: %w", err)
	}
	body := raw
	if len(wrapper.State) > 0 && string(wrapper.State) != "null" {
		body = wrapper.State
	}
	var st listState
	if err := json.Unmarshal(body, &st); err != nil {
		return View{}, fmt.Errorf("list_permission_rules: %w", err)
	}
	if st.Rules == nil && st.Allow == nil && st.Ask == nil && st.Deny == nil && st.WorkspaceDirectories == nil {
		return View{}, ErrUnknownShape
	}
	v := View{AutoMode: map[AutoList][]AutoEntry{}, OriginalCwd: st.OriginalCwd, ManagedOnly: st.ManagedOnly}
	for _, r := range st.Rules {
		b := Behavior(r.Behavior)
		if !b.valid() {
			continue
		}
		text := r.Rule
		if text == "" {
			text = ruleValueString(r.RuleValue)
		}
		e := newEntry(text, patch.Scope(r.Source))
		// The engine says whether a rule can be edited; trust a read-only verdict.
		if r.Editability != "" && r.Editability != "persistent" {
			e.ReadOnly = true
		}
		e.NotInEffect = r.NotInEffect
		if d := r.Description; d != nil {
			e.Summary = &Summary{Prefix: d.Prefix, Emphasis: d.Emphasis, Suffix: d.Suffix}
		}
		v.add(b, e)
	}
	for b, list := range map[Behavior][]json.RawMessage{Allow: st.Allow, Ask: st.Ask, Deny: st.Deny} {
		for _, item := range list {
			if e, ok := plainEntry(item); ok {
				v.add(b, e)
			}
		}
	}
	for _, d := range st.WorkspaceDirectories {
		s := patch.Scope(d.Source)
		v.Directories = append(v.Directories, Dir{Path: d.Path, Scope: s, ReadOnly: !s.Writable()})
	}
	sortEntries(v.Allow)
	sortEntries(v.Ask)
	sortEntries(v.Deny)
	return v, nil
}

// plainEntry decodes "Rule" or {"rule"|"ruleValue": …, "source": …}.
func plainEntry(item json.RawMessage) (Entry, bool) {
	var s string
	if json.Unmarshal(item, &s) == nil {
		return newEntry(s, ""), s != ""
	}
	var o struct {
		Rule      string          `json:"rule"`
		RuleValue json.RawMessage `json:"ruleValue"`
		Source    string          `json:"source"`
	}
	if json.Unmarshal(item, &o) != nil {
		return Entry{}, false
	}
	text := o.Rule
	if text == "" {
		text = ruleValueString(o.RuleValue)
	}
	return newEntry(text, patch.Scope(o.Source)), text != ""
}

// ruleValueString accepts a rule string or {toolName, ruleContent}.
func ruleValueString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		ToolName    string  `json:"toolName"`
		RuleContent *string `json:"ruleContent"`
	}
	if json.Unmarshal(raw, &o) != nil || o.ToolName == "" {
		return ""
	}
	if o.RuleContent == nil {
		return o.ToolName
	}
	return o.ToolName + "(" + *o.RuleContent + ")"
}

// sortEntries orders by scope precedence (highest first), keeping response order within
// a scope. Unknown engine sources (session, cliArg, …) sort after the settings files.
func sortEntries(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		return es[i].Scope.Precedence() > es[j].Scope.Precedence()
	})
}

// String renders a one-line debug form, e.g. "Bash(git *) [User]".
func (e Entry) String() string {
	label := e.Scope.Label()
	if label == "" {
		label = "?"
	}
	return strings.TrimSpace(fmt.Sprintf("%s [%s]", e.Raw, label))
}
