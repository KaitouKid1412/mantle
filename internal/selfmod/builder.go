package selfmod

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// BuilderRules is the builder's rules file, passed with
// --append-system-prompt-file.
//
//go:embed builder_rules.md
var BuilderRules string

// BuilderEnginePrefix starts every builder engine id ("builder-<id>").
const BuilderEnginePrefix = "builder-"

// BuilderEngineID is the engine id of the builder for request id.
func BuilderEngineID(id string) string { return BuilderEnginePrefix + id }

// DefaultMaxBudgetUSD caps one builder session's spend.
const DefaultMaxBudgetUSD = 5.0

// BuilderAllowedTools are the Bash commands the builder may run without
// asking. File edits inside its worktree are allowed by acceptEdits.
var BuilderAllowedTools = []string{
	"Bash(go build:*)",
	"Bash(go test:*)",
	"Bash(go vet:*)",
	"Bash(go list:*)",
	"Bash(go doc:*)",
	"Bash(gofmt:*)",
	"Bash(go run ./cmd/mantle-ui:*)",
	"Bash(./bin/mantle-ui story:*)",
	"Bash(./bin/mantle-ui catalog:*)",
	"Bash(git diff:*)",
	"Bash(git status)",
}

// BuilderDisallowedTools are denied outright: committing, pushing, adding
// dependencies, deleting trees, and editing the protected paths. The
// pipeline's diff check enforces the protected paths again, because a
// shell command can write any file.
func BuilderDisallowedTools() []string {
	out := []string{
		"Bash(git commit:*)",
		"Bash(git push:*)",
		"Bash(go get:*)",
		"Bash(rm -rf:*)",
	}
	for _, p := range ProtectedPaths {
		out = append(out, "Edit("+p+")", "Write("+p+")")
	}
	return out
}

// BuilderOptions configures a builder engine.
type BuilderOptions struct {
	RequestID string
	Worktree  string
	// RulesFile is the path of the rules file on disk (see WriteRules).
	RulesFile    string
	Model        string
	MaxBudgetUSD float64
}

// BuilderSpawnOpts returns the engine options of a builder: a normal
// headless claude in the worktree with acceptEdits, narrow tool rules, the
// builder rules appended to the system prompt and a budget cap.
func BuilderSpawnOpts(o BuilderOptions) ext.SpawnOpts {
	budget := o.MaxBudgetUSD
	if budget <= 0 {
		budget = DefaultMaxBudgetUSD
	}
	args := []string{"--allowedTools"}
	args = append(args, BuilderAllowedTools...)
	args = append(args, "--disallowedTools")
	args = append(args, BuilderDisallowedTools()...)
	if o.RulesFile != "" {
		args = append(args, "--append-system-prompt-file", o.RulesFile)
	}
	args = append(args, "--max-budget-usd", strconv.FormatFloat(budget, 'f', -1, 64))
	return ext.SpawnOpts{
		Cwd:            o.Worktree,
		Model:          o.Model,
		PermissionMode: "acceptEdits",
		Name:           "mantle builder " + o.RequestID,
		ExtraArgs:      args,
	}
}

// WriteRules writes BuilderRules into dir and returns the file's path.
func WriteRules(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "builder-rules.md")
	return p, os.WriteFile(p, []byte(BuilderRules), 0o644)
}

// BuildPrompt is the builder's first message for a request.
func BuildPrompt(r *Request) string {
	var b strings.Builder
	switch r.Kind {
	case RequestEdit:
		fmt.Fprintf(&b, "Change the existing mantle mod %q (package mods/%s/ and any core seam it added).\n\n", r.Target, r.Target)
		if orig := r.originalRequest(); orig != "" {
			fmt.Fprintf(&b, "Its original request was:\n%s\n\n", orig)
		}
		fmt.Fprintf(&b, "The new request is:\n%s\n", r.Request)
	case RequestUpdate:
		b.WriteString(r.Request)
		b.WriteString("\n")
		return b.String()
	default:
		fmt.Fprintf(&b, "mantle request (mod id %q):\n%s\n", r.ID, r.Request)
	}
	fmt.Fprintf(&b, "\nWork only in this worktree. Put new code in mods/%s/ unless a core seam is required. Follow the builder rules, and do not commit.\n", modDir(r))
	return b.String()
}

func modDir(r *Request) string {
	if r.Kind == RequestEdit && r.Target != "" {
		return r.Target
	}
	return r.ID
}

// originalRequest is filled for edits from the target mod's trailer.
func (r *Request) originalRequest() string { return r.Original }

// RetryPrompt feeds a failed pipeline run back to the builder.
func RetryPrompt(r *Request, failure string) string {
	return fmt.Sprintf("Round %d of %d: the mantle pipeline rejected the change.\n\n%s\nFix these problems in the worktree. Do not commit.\n",
		r.Round, MaxRounds, strings.TrimSpace(failure))
}

// ConflictPrompt asks the builder to resolve a rebase conflict for update,
// using the mod's original request as intent.
func ConflictPrompt(modID, request string, files []string) string {
	return fmt.Sprintf(`mantle is rebasing the mod %q onto a new upstream version, and the rebase stopped on conflicts in:
  %s

The mod's original request was:
%s

Resolve the conflicts so the mod keeps doing what the user asked, adapted to the new core code. Edit the conflicted files (remove every conflict marker), then run "git diff" to check. Do not run git add, git commit or git rebase: mantle continues the rebase for you.
`, modID, strings.Join(files, "\n  "), request)
}

// Config proposal markers in the builder's reply.
const (
	ProposalStart = "MANTLE_CONFIG_PROPOSAL"
	ProposalEnd   = "END_MANTLE_CONFIG_PROPOSAL"
)

// ConfigProposal is a config change the builder proposes instead of code.
type ConfigProposal struct {
	Summary string `json:"summary"`
	// Scope is "mantle" (~/.mantle/settings.json) or "claude".
	Scope string          `json:"scope"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// ParseConfigProposal finds a proposal block in the builder's reply. ok is
// false when there is none; err reports a malformed block.
func ParseConfigProposal(reply string) (p *ConfigProposal, ok bool, err error) {
	// The start marker is also the tail of the end marker: skip occurrences
	// preceded by "END_".
	start, from := -1, 0
	for {
		i := strings.Index(reply[from:], ProposalStart)
		if i < 0 {
			break
		}
		i += from
		if !strings.HasSuffix(reply[:i], "END_") {
			start = i
			break
		}
		from = i + len(ProposalStart)
	}
	if start < 0 {
		return nil, false, nil
	}
	body := reply[start+len(ProposalStart):]
	if end := strings.Index(body, ProposalEnd); end >= 0 {
		body = body[:end]
	}
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	var cp ConfigProposal
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &cp); err != nil {
		return nil, true, fmt.Errorf("malformed config proposal: %w", err)
	}
	if cp.Key == "" || len(cp.Value) == 0 {
		return nil, true, errors.New("config proposal without key or value")
	}
	switch cp.Scope {
	case "mantle", "claude":
	case "":
		cp.Scope = "claude"
	default:
		return nil, true, fmt.Errorf("config proposal with unknown scope %q", cp.Scope)
	}
	return &cp, true, nil
}

// DecodedValue returns the proposal's value as a Go value.
func (p *ConfigProposal) DecodedValue() (any, error) {
	var v any
	err := json.Unmarshal(p.Value, &v)
	return v, err
}
