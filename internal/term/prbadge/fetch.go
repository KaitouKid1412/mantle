package prbadge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

// ReviewState is the badge colour state. The values match the status line payload's
// pr.review_state.
type ReviewState string

const (
	Approved         ReviewState = "approved"
	Pending          ReviewState = "pending"
	ChangesRequested ReviewState = "changes_requested"
	Draft            ReviewState = "draft"
)

// PR is an open pull request or merge request.
type PR struct {
	Number int
	URL    string
	Review ReviewState // may be empty
	Kind   string      // "" for a GitHub PR, "mr" for a GitLab merge request
}

// Label is the badge text: "PR #12" or "MR !12".
func (p PR) Label() string {
	if p.Kind == "mr" {
		return fmt.Sprintf("MR !%d", p.Number)
	}
	return fmt.Sprintf("PR #%d", p.Number)
}

// LinkURL applies a prUrlTemplate ("https://review.example/?url={url}") to the PR's URL.
// An empty template returns the URL unchanged.
func (p PR) LinkURL(template string) string {
	if strings.TrimSpace(template) == "" || !strings.Contains(template, "{url}") {
		return p.URL
	}
	return strings.ReplaceAll(template, "{url}", p.URL)
}

// Hint values are mantle's own wording.
const (
	HintInstallGH = "PR status needs the gh CLI"
	HintAuthGH    = "run gh auth login for PR status"
)

// Status is the result of one lookup.
type Status struct {
	Repo   Repo
	Branch string
	PR     *PR    // nil: no open PR or MR
	Hint   string // a one-line setup hint for the footer (tool missing, not logged in)
	Err    error  // anything unexpected; keep it out of the footer
	At     time.Time
}

// Key identifies a cache entry.
type Key struct {
	Repo   string // host/owner/name
	Branch string
}

// KeyOf builds the cache key for a repo and branch.
func KeyOf(r Repo, branch string) Key {
	return Key{Repo: r.Host + "/" + r.Owner + "/" + r.Name, Branch: branch}
}

// RunFunc runs a command in dir and returns its stdout and stderr.
type RunFunc func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr []byte, err error)

// Fetcher looks up PR status. The zero value works with real tools and a 60 s TTL.
// It is safe for concurrent use.
type Fetcher struct {
	Run      RunFunc
	LookPath func(string) (string, error)
	Env      terminal.Env
	Now      func() time.Time
	TTL      time.Duration // fresh results; default 60s
	HintTTL  time.Duration // results with a hint or error; default 5m
	Timeout  time.Duration // per command; default 10s

	mu       sync.Mutex
	cache    map[Key]Status
	inflight map[Key]bool
}

// Enabled reports whether PR lookups should run: prStatusFooterEnabled (nil = default
// on) and not CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC.
func Enabled(env terminal.Env, setting *bool) bool {
	if setting != nil && !*setting {
		return false
	}
	return !truthy(terminal.Get(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"))
}

// Detect reads the origin remote and current branch of the repository at dir. A
// detached HEAD returns an empty branch.
func (f *Fetcher) Detect(ctx context.Context, dir string) (Repo, string, error) {
	out, _, err := f.run(ctx, dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return Repo{}, "", fmt.Errorf("prbadge: no origin remote: %w", err)
	}
	repo, ok := ParseRemote(string(out))
	if !ok {
		return Repo{}, "", fmt.Errorf("prbadge: unrecognised remote %q", strings.TrimSpace(string(out)))
	}
	if repo.Forge == ForgeUnknown {
		repo.Forge = f.forgeFromEnv(repo.Host)
	}
	out, _, err = f.run(ctx, dir, "git", "branch", "--show-current")
	if err != nil {
		return repo, "", fmt.Errorf("prbadge: branch: %w", err)
	}
	return repo, strings.TrimSpace(string(out)), nil
}

// Cached returns the cached status for k, whether one exists, and whether a refresh is
// due (missing or expired, and none in flight).
func (f *Fetcher) Cached(k Key) (st Status, ok, due bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok = f.cache[k]
	if f.inflight[k] {
		return st, ok, false
	}
	if !ok {
		return st, false, true
	}
	ttl := f.ttl()
	if st.Hint != "" || st.Err != nil {
		ttl = f.hintTTL()
	}
	return st, true, f.now().Sub(st.At) >= ttl
}

// Invalidate drops cached entries for every branch of repo (empty repo: everything),
// e.g. after `git push` or `gh pr create`.
func (f *Fetcher) Invalidate(repo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.cache {
		if repo == "" || k.Repo == repo {
			delete(f.cache, k)
		}
	}
}

// Fetch queries the forge for the branch checked out at dir and caches the result. It
// blocks; call it from a Cmd. Concurrent fetches for the same key are not deduplicated
// here; use Cached's due flag (which is false while one is in flight) to avoid them.
func (f *Fetcher) Fetch(ctx context.Context, dir string, repo Repo, branch string) Status {
	k := KeyOf(repo, branch)
	f.mu.Lock()
	if f.inflight == nil {
		f.inflight = map[Key]bool{}
	}
	f.inflight[k] = true
	f.mu.Unlock()

	st := f.fetch(ctx, dir, repo, branch)
	st.Repo, st.Branch, st.At = repo, branch, f.now()

	f.mu.Lock()
	delete(f.inflight, k)
	if f.cache == nil {
		f.cache = map[Key]Status{}
	}
	f.cache[k] = st
	f.mu.Unlock()
	return st
}

func (f *Fetcher) fetch(ctx context.Context, dir string, repo Repo, branch string) Status {
	if branch == "" {
		return Status{}
	}
	switch repo.Forge {
	case GitHub:
		return f.fetchGitHub(ctx, dir, repo)
	case GitLab:
		return f.fetchGitLab(ctx, dir)
	}
	return Status{}
}

type ghPR struct {
	Number         int    `json:"number"`
	URL            string `json:"url"`
	State          string `json:"state"`
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"`
}

func (f *Fetcher) fetchGitHub(ctx context.Context, dir string, repo Repo) Status {
	// Hints only where a login would help: github.com and the GH_HOST enterprise host.
	hinted := repo.Host == "github.com" || strings.EqualFold(repo.Host, terminal.Get(f.env(), "GH_HOST"))
	if _, err := f.lookPath("gh"); err != nil {
		if hinted {
			return Status{Hint: HintInstallGH}
		}
		return Status{}
	}
	out, errOut, err := f.run(ctx, dir, "gh", "pr", "view", "--json", "number,url,state,isDraft,reviewDecision")
	if err != nil {
		msg := strings.ToLower(string(errOut))
		switch {
		case strings.Contains(msg, "no pull requests found"), strings.Contains(msg, "no open pull requests"):
			return Status{}
		case isAuthError(msg):
			if hinted {
				return Status{Hint: HintAuthGH}
			}
			return Status{}
		}
		return Status{Err: commandError("gh", err, errOut)}
	}
	var p ghPR
	if err := json.Unmarshal(out, &p); err != nil {
		return Status{Err: fmt.Errorf("prbadge: gh output: %w", err)}
	}
	return Status{PR: githubPR(p)}
}

func githubPR(p ghPR) *PR {
	if !strings.EqualFold(p.State, "OPEN") || p.Number <= 0 {
		return nil
	}
	pr := &PR{Number: p.Number, URL: p.URL}
	switch {
	case p.IsDraft:
		pr.Review = Draft
	case p.ReviewDecision == "APPROVED":
		pr.Review = Approved
	case p.ReviewDecision == "CHANGES_REQUESTED":
		pr.Review = ChangesRequested
	default:
		pr.Review = Pending
	}
	return pr
}

type glMR struct {
	IID                 int    `json:"iid"`
	WebURL              string `json:"web_url"`
	State               string `json:"state"`
	Draft               bool   `json:"draft"`
	WorkInProgress      bool   `json:"work_in_progress"`
	DetailedMergeStatus string `json:"detailed_merge_status"`
	MergeStatus         string `json:"merge_status"`
}

func (f *Fetcher) fetchGitLab(ctx context.Context, dir string) Status {
	if _, err := f.lookPath("glab"); err != nil {
		return Status{}
	}
	out, errOut, err := f.run(ctx, dir, "glab", "mr", "view", "-F", "json")
	if err != nil {
		msg := strings.ToLower(string(errOut))
		switch {
		case strings.Contains(msg, "no open merge request"), strings.Contains(msg, "not found"):
			return Status{}
		case isAuthError(msg):
			return Status{}
		}
		return Status{Err: commandError("glab", err, errOut)}
	}
	var m glMR
	if err := json.Unmarshal(out, &m); err != nil {
		return Status{Err: fmt.Errorf("prbadge: glab output: %w", err)}
	}
	return Status{PR: gitlabMR(m)}
}

func gitlabMR(m glMR) *PR {
	if m.State != "opened" || m.IID <= 0 {
		return nil
	}
	pr := &PR{Number: m.IID, URL: m.WebURL, Kind: "mr"}
	mergeable := m.DetailedMergeStatus == "mergeable" ||
		(m.DetailedMergeStatus == "" && m.MergeStatus == "can_be_merged")
	switch {
	case m.Draft || m.WorkInProgress:
		pr.Review = Draft
	case mergeable:
		pr.Review = Approved
	default:
		pr.Review = Pending
	}
	return pr
}

func isAuthError(msg string) bool {
	for _, s := range []string{"auth login", "not logged", "authentication", "401", "bad credentials", "gh_token"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func commandError(name string, err error, stderr []byte) error {
	line := strings.TrimSpace(string(stderr))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return fmt.Errorf("prbadge: %s: %w", name, err)
	}
	return fmt.Errorf("prbadge: %s: %w: %s", name, err, line)
}

func (f *Fetcher) forgeFromEnv(host string) Forge {
	env := f.env()
	switch {
	case strings.EqualFold(host, terminal.Get(env, "GH_HOST")):
		return GitHub
	case strings.EqualFold(host, terminal.Get(env, "GITLAB_HOST")):
		return GitLab
	}
	return ForgeUnknown
}

func (f *Fetcher) run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	env := []string{"GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "GLAB_NO_PROMPT=1",
		"NO_COLOR=1", "GIT_TERMINAL_PROMPT=0", "CLICOLOR=0"}
	if f.Run != nil {
		return f.Run(ctx, dir, env, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

func (f *Fetcher) lookPath(name string) (string, error) {
	if f.LookPath != nil {
		return f.LookPath(name)
	}
	return exec.LookPath(name)
}

func (f *Fetcher) env() terminal.Env {
	if f.Env != nil {
		return f.Env
	}
	return terminal.OS()
}

func (f *Fetcher) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) ttl() time.Duration {
	if f.TTL > 0 {
		return f.TTL
	}
	return time.Minute
}

func (f *Fetcher) hintTTL() time.Duration {
	if f.HintTTL > 0 {
		return f.HintTTL
	}
	return 5 * time.Minute
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
