package prbadge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		in   string
		want Repo
		ok   bool
	}{
		{"git@github.com:acme/widgets.git", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"git@github.com:acme/widgets", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"https://github.com/acme/widgets.git\n", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"https://user:tok@GitHub.com/acme/widgets/", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"ssh://git@github.com:22/acme/widgets.git", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"git://github.com/acme/widgets", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"org-1@github.com:acme/widgets.git", Repo{"github.com", "acme", "widgets", GitHub}, true},
		{"git@github.example.com:acme/widgets.git", Repo{"github.example.com", "acme", "widgets", GitHub}, true},
		{"git@github.com-work:acme/widgets.git", Repo{"github.com-work", "acme", "widgets", GitHub}, true},
		{"git@gitlab.com:group/sub/proj.git", Repo{"gitlab.com", "group/sub", "proj", GitLab}, true},
		{"https://gitlab.example.org/a/b", Repo{"gitlab.example.org", "a", "b", GitLab}, true},
		{"git@bitbucket.org:team/repo.git", Repo{"bitbucket.org", "team", "repo", ForgeUnknown}, true},
		{"https://github.com/acme/widgets/extra", Repo{}, false},
		{"/local/path/repo.git", Repo{}, false},
		{"./rel:path", Repo{}, false},
		{"file:///tmp/repo", Repo{}, false},
		{"https://github.com/acme", Repo{}, false},
		{"https://github.com/acme/../x", Repo{}, false},
		{"", Repo{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseRemote(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPRLabelAndLink(t *testing.T) {
	pr := PR{Number: 12, URL: "https://github.com/a/b/pull/12"}
	if pr.Label() != "PR #12" {
		t.Errorf("Label = %q", pr.Label())
	}
	if (PR{Number: 3, Kind: "mr"}).Label() != "MR !3" {
		t.Error("MR label")
	}
	if got := pr.LinkURL("https://review.example/?u={url}"); got != "https://review.example/?u=https://github.com/a/b/pull/12" {
		t.Errorf("LinkURL = %q", got)
	}
	if got := pr.LinkURL(""); got != pr.URL {
		t.Errorf("LinkURL empty = %q", got)
	}
	if got := pr.LinkURL("https://no-placeholder"); got != pr.URL {
		t.Errorf("LinkURL without {url} = %q", got)
	}
}

func TestEnabled(t *testing.T) {
	f, tr := false, true
	if !Enabled(terminal.Map{}, nil) || !Enabled(terminal.Map{}, &tr) {
		t.Error("default on")
	}
	if Enabled(terminal.Map{}, &f) {
		t.Error("setting off")
	}
	if Enabled(terminal.Map{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"}, nil) {
		t.Error("nonessential traffic off")
	}
	if !Enabled(terminal.Map{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "0"}, nil) {
		t.Error("0 is not set")
	}
}

// fakeTools puts fake gh and glab on PATH. Their behaviour is chosen by FAKE_MODE.
func fakeTools(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gh := `#!/bin/sh
[ "$GH_PROMPT_DISABLED" = 1 ] || { echo "prompts not disabled" >&2; exit 9; }
[ "$*" = "pr view --json number,url,state,isDraft,reviewDecision" ] || { echo "bad args: $*" >&2; exit 9; }
case "$FAKE_MODE" in
open) echo '{"number":42,"url":"https://github.com/acme/widgets/pull/42","state":"OPEN","isDraft":false,"reviewDecision":"APPROVED"}';;
changes) echo '{"number":42,"url":"u","state":"OPEN","isDraft":false,"reviewDecision":"CHANGES_REQUESTED"}';;
review) echo '{"number":42,"url":"u","state":"OPEN","isDraft":false,"reviewDecision":"REVIEW_REQUIRED"}';;
draft) echo '{"number":42,"url":"u","state":"OPEN","isDraft":true,"reviewDecision":"APPROVED"}';;
merged) echo '{"number":42,"url":"u","state":"MERGED","isDraft":false,"reviewDecision":"APPROVED"}';;
none) echo 'no pull requests found for branch "feat"' >&2; exit 1;;
auth) echo 'To get started with GitHub CLI, please run:  gh auth login' >&2; exit 4;;
junk) echo 'not json';;
boom) echo 'something odd' >&2; exit 2;;
esac
`
	glab := `#!/bin/sh
[ "$*" = "mr view -F json" ] || { echo "bad args: $*" >&2; exit 9; }
case "$FAKE_MODE" in
open) echo '{"iid":7,"web_url":"https://gitlab.com/g/p/-/merge_requests/7","state":"opened","draft":false,"detailed_merge_status":"mergeable"}';;
pending) echo '{"iid":7,"web_url":"u","state":"opened","draft":false,"detailed_merge_status":"not_approved"}';;
legacy) echo '{"iid":7,"web_url":"u","state":"opened","merge_status":"can_be_merged"}';;
draft) echo '{"iid":7,"web_url":"u","state":"opened","draft":true,"detailed_merge_status":"mergeable"}';;
merged) echo '{"iid":7,"web_url":"u","state":"merged"}';;
none) echo 'no open merge request available for "feat"' >&2; exit 1;;
esac
`
	for name, body := range map[string]string{"gh": gh, "glab": glab} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestFetchGitHub(t *testing.T) {
	fakeTools(t)
	repo := Repo{"github.com", "acme", "widgets", GitHub}
	tests := []struct {
		mode   string
		review ReviewState
		noPR   bool
		hint   string
		err    bool
	}{
		{mode: "open", review: Approved},
		{mode: "changes", review: ChangesRequested},
		{mode: "review", review: Pending},
		{mode: "draft", review: Draft},
		{mode: "merged", noPR: true},
		{mode: "none", noPR: true},
		{mode: "auth", noPR: true, hint: HintAuthGH},
		{mode: "junk", noPR: true, err: true},
		{mode: "boom", noPR: true, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Setenv("FAKE_MODE", tt.mode)
			f := &Fetcher{Env: terminal.Map{}}
			st := f.Fetch(context.Background(), t.TempDir(), repo, "feat")
			if (st.PR == nil) != tt.noPR {
				t.Fatalf("PR = %+v, err = %v", st.PR, st.Err)
			}
			if st.PR != nil {
				if st.PR.Review != tt.review || st.PR.Number != 42 || st.PR.Kind != "" {
					t.Errorf("PR = %+v", st.PR)
				}
			}
			if st.Hint != tt.hint {
				t.Errorf("Hint = %q, want %q", st.Hint, tt.hint)
			}
			if (st.Err != nil) != tt.err {
				t.Errorf("Err = %v", st.Err)
			}
			if st.Repo != repo || st.Branch != "feat" || st.At.IsZero() {
				t.Errorf("metadata = %+v", st)
			}
		})
	}
}

func TestFetchGitHubHints(t *testing.T) {
	noTool := func(string) (string, error) { return "", exec.ErrNotFound }
	f := &Fetcher{LookPath: noTool, Env: terminal.Map{"GH_HOST": "ghe.corp"}}
	ctx := context.Background()
	if st := f.Fetch(ctx, "", Repo{"github.com", "a", "b", GitHub}, "x"); st.Hint != HintInstallGH {
		t.Errorf("github.com hint = %q", st.Hint)
	}
	if st := f.Fetch(ctx, "", Repo{"ghe.corp", "a", "b", GitHub}, "x"); st.Hint != HintInstallGH {
		t.Errorf("GH_HOST hint = %q", st.Hint)
	}
	if st := f.Fetch(ctx, "", Repo{"github.other", "a", "b", GitHub}, "x"); st.Hint != "" {
		t.Errorf("other host should get no hint: %q", st.Hint)
	}

	fakeTools(t)
	t.Setenv("FAKE_MODE", "auth")
	f = &Fetcher{Env: terminal.Map{}}
	if st := f.Fetch(ctx, "", Repo{"github.other", "a", "b", GitHub}, "x"); st.Hint != "" || st.Err != nil {
		t.Errorf("other host auth: %+v", st)
	}
}

func TestFetchGitLab(t *testing.T) {
	fakeTools(t)
	repo := Repo{"gitlab.com", "g", "p", GitLab}
	tests := []struct {
		mode   string
		review ReviewState
		noPR   bool
	}{
		{mode: "open", review: Approved},
		{mode: "pending", review: Pending},
		{mode: "legacy", review: Approved},
		{mode: "draft", review: Draft},
		{mode: "merged", noPR: true},
		{mode: "none", noPR: true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Setenv("FAKE_MODE", tt.mode)
			st := (&Fetcher{}).Fetch(context.Background(), t.TempDir(), repo, "feat")
			if (st.PR == nil) != tt.noPR || st.Err != nil {
				t.Fatalf("st = %+v", st)
			}
			if st.PR != nil && (st.PR.Review != tt.review || st.PR.Kind != "mr" || st.PR.Label() != "MR !7") {
				t.Errorf("PR = %+v", st.PR)
			}
		})
	}
}

func TestFetchSkips(t *testing.T) {
	var calls atomic.Int32
	f := &Fetcher{Run: func(context.Context, string, []string, string, ...string) ([]byte, []byte, error) {
		calls.Add(1)
		return nil, nil, errors.New("should not run")
	}, LookPath: func(string) (string, error) { return "/bin/true", nil }}
	ctx := context.Background()
	if st := f.Fetch(ctx, "", Repo{"github.com", "a", "b", GitHub}, ""); st.PR != nil || st.Err != nil {
		t.Errorf("detached head: %+v", st)
	}
	if st := f.Fetch(ctx, "", Repo{"bitbucket.org", "a", "b", ForgeUnknown}, "main"); st.PR != nil || st.Err != nil {
		t.Errorf("unknown forge: %+v", st)
	}
	if calls.Load() != 0 {
		t.Errorf("ran %d commands", calls.Load())
	}
}

func TestCache(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	f := &Fetcher{
		Now:      func() time.Time { return now },
		LookPath: func(string) (string, error) { return "gh", nil },
		Run: func(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, []byte, error) {
			calls.Add(1)
			started <- struct{}{}
			<-block
			return []byte(`{"number":1,"url":"u","state":"OPEN"}`), nil, nil
		},
	}
	repo := Repo{"github.com", "a", "b", GitHub}
	k := KeyOf(repo, "main")
	if _, ok, due := f.Cached(k); ok || !due {
		t.Fatal("empty cache should be due")
	}
	done := make(chan Status)
	go func() { done <- f.Fetch(context.Background(), "", repo, "main") }()
	<-started
	if _, _, due := f.Cached(k); due {
		t.Error("in-flight fetch must not be due")
	}
	close(block)
	<-done
	st, ok, due := f.Cached(k)
	if !ok || due || st.PR == nil || st.PR.Number != 1 {
		t.Fatalf("after fetch: %+v ok=%v due=%v", st, ok, due)
	}
	now = now.Add(59 * time.Second)
	if _, _, due := f.Cached(k); due {
		t.Error("fresh within TTL")
	}
	now = now.Add(2 * time.Second)
	if _, _, due := f.Cached(k); !due {
		t.Error("expired after TTL")
	}
	f.Invalidate("github.com/a/b")
	if _, ok, _ := f.Cached(k); ok {
		t.Error("Invalidate should drop the entry")
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestHintTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &Fetcher{Now: func() time.Time { return now },
		LookPath: func(string) (string, error) { return "", exec.ErrNotFound }}
	repo := Repo{"github.com", "a", "b", GitHub}
	f.Fetch(context.Background(), "", repo, "main")
	now = now.Add(2 * time.Minute)
	if _, _, due := f.Cached(KeyOf(repo, "main")); due {
		t.Error("hint results use the longer TTL")
	}
	now = now.Add(4 * time.Minute)
	if _, _, due := f.Cached(KeyOf(repo, "main")); !due {
		t.Error("hint results expire")
	}
}

func TestDetect(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "feature/x")
	git("remote", "add", "origin", "git@gitlab.example.org:team/sub/app.git")
	f := &Fetcher{Env: terminal.Map{}}
	repo, branch, err := f.Detect(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if repo != (Repo{"gitlab.example.org", "team/sub", "app", GitLab}) || branch != "feature/x" {
		t.Errorf("Detect = %+v %q", repo, branch)
	}

	git("remote", "set-url", "origin", "https://code.corp/acme/app.git")
	f.Env = terminal.Map{"GH_HOST": "code.corp"}
	repo, _, err = f.Detect(context.Background(), dir)
	if err != nil || repo.Forge != GitHub {
		t.Errorf("GH_HOST forge: %+v %v", repo, err)
	}

	if _, _, err := f.Detect(context.Background(), t.TempDir()); err == nil {
		t.Error("non-repo should fail")
	}
}

func TestLinker(t *testing.T) {
	rules := []LinkRule{
		{Pattern: `#(\d+)`, URL: "https://github.com/{owner}/{repo}/issues/{number}"},
		{Pattern: `PROJ-(\d+)`, URL: "https://jira.example.com/browse/PROJ-{number}"},
		{Pattern: `REV-\d+`, URL: "https://rev.example/{url}"},
		{Pattern: `(?<=x)y`, URL: "https://bad"},
		{Pattern: ``, URL: "https://empty"},
	}
	l, errs := NewLinker(rules, Repo{"github.com", "acme", "widgets", GitHub})
	if len(errs) != 2 {
		t.Errorf("errs = %v", errs)
	}
	got := l.Find("Fixed PROJ-12 and #7, see REV-3 and again #7")
	want := []Link{
		{"PROJ-12", "https://jira.example.com/browse/PROJ-12"},
		{"#7", "https://github.com/acme/widgets/issues/7"},
		{"REV-3", "https://rev.example/REV-3"},
	}
	if len(got) != len(want) {
		t.Fatalf("Find = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	noRepo, _ := NewLinker(rules[:2], Repo{})
	if got := noRepo.Find("#7 PROJ-1"); len(got) != 1 || got[0].Text != "PROJ-1" {
		t.Errorf("no repo: %+v", got)
	}
	var nilLinker *Linker
	if nilLinker.Find("#1") != nil {
		t.Error("nil Linker")
	}
}

func TestIssueRefs(t *testing.T) {
	gh := Repo{"github.com", "me", "mine", GitHub}
	tests := []struct {
		text string
		repo Repo
		want []string // URLs
	}{
		{"see acme/widgets#12.", gh, []string{"https://github.com/acme/widgets/issues/12"}},
		{"(acme/widgets#12) and foo/bar.js#3", gh, []string{"https://github.com/acme/widgets/issues/12", "https://github.com/foo/bar.js/issues/3"}},
		{"bare #12", gh, nil},
		{"group/sub/proj#12", gh, nil},
		{"acme/widgets#12abc", gh, nil},
		{"https://x.com/acme/widgets#12", gh, nil},
		{"acme/widgets#5", Repo{Host: "ghe.corp"}, []string{"https://ghe.corp/acme/widgets/issues/5"}},
		{"acme/widgets#5", Repo{Host: "gitlab.com"}, []string{"https://gitlab.com/acme/widgets/-/issues/5"}},
		{"acme/widgets#5", Repo{Host: "bitbucket.org"}, nil},
		{"acme/widgets#5", Repo{}, []string{"https://github.com/acme/widgets/issues/5"}},
	}
	for _, tt := range tests {
		refs := IssueRefs(tt.text, tt.repo)
		var urls []string
		for _, r := range refs {
			urls = append(urls, r.URL)
			if tt.text[r.Start:r.End] != r.Text {
				t.Errorf("offsets wrong for %q", r.Text)
			}
		}
		if strings.Join(urls, ",") != strings.Join(tt.want, ",") {
			t.Errorf("IssueRefs(%q) = %v, want %v", tt.text, urls, tt.want)
		}
	}
}

func TestLinkifyIssues(t *testing.T) {
	got := LinkifyIssues("x acme/w#1 y", Repo{}, func(u, s string) string { return "[" + s + "](" + u + ")" })
	if got != "x [acme/w#1](https://github.com/acme/w/issues/1) y" {
		t.Errorf("LinkifyIssues = %q", got)
	}
	if got := LinkifyIssues("none", Repo{}, nil); got != "none" {
		t.Errorf("no refs = %q", got)
	}
}
