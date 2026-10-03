package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/prbadge"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// fakeForge answers git and gh like a repo on branch feat with an open PR.
type fakeForge struct {
	mu     sync.Mutex
	calls  []string
	review string
}

func (f *fakeForge) fetcher() *prbadge.Fetcher {
	return &prbadge.Fetcher{
		LookPath: func(string) (string, error) { return "/bin/x", nil },
		Run: func(_ context.Context, dir string, _ []string, name string, args ...string) ([]byte, []byte, error) {
			f.mu.Lock()
			f.calls = append(f.calls, name+" "+strings.Join(args, " "))
			f.mu.Unlock()
			switch {
			case name == "git" && args[0] == "remote":
				return []byte("git@github.com:acme/widgets.git\n"), nil, nil
			case name == "git" && args[0] == "branch":
				return []byte("feat\n"), nil, nil
			case name == "gh":
				return []byte(`{"number":42,"url":"https://github.com/acme/widgets/pull/42","state":"OPEN","reviewDecision":"` + f.review + `"}`), nil, nil
			}
			return nil, nil, errors.New("unexpected " + name)
		},
	}
}

func (f *fakeForge) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// deliver runs cmd and feeds every prStatusMsg back into the components.
func deliver(t *testing.T, ctx ext.Ctx, cmd tea.Cmd, comps ...ext.Component) {
	t.Helper()
	for _, m := range exttest.Exec(cmd) {
		if pm, ok := m.(prStatusMsg); ok {
			for _, c := range comps {
				c.Update(ctx, pm)
			}
		}
	}
}

func TestFooterPRBadge(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["prUrlTemplate"] = "https://review.example/?u={url}"
	ff := &fakeForge{review: "APPROVED"}
	f := newFooter()
	f.env, f.fetcher = terminal.Map{}, ff.fetcher()
	f.Init(ctx)

	cmd := f.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Cwd: "/w/widgets"}})
	if cmd == nil {
		t.Fatal("session start should look up the PR")
	}
	deliver(t, ctx, cmd, f)
	out := f.View(ctx, ext.Area{Width: 120}).Text
	if !strings.Contains(ansi.Strip(out), "PR #42") {
		t.Fatalf("footer = %q", ansi.Strip(out))
	}
	if !strings.Contains(out, "\x1b]8;;https://review.example/?u=https://github.com/acme/widgets/pull/42\x1b\\") {
		t.Errorf("badge link missing: %q", out)
	}

	// A later turn end within the TTL uses the cache.
	deliver(t, ctx, f.Update(ctx, ev(&proto.Result{})), f)
	if n := ff.count("gh"); n != 1 {
		t.Errorf("gh ran %d times", n)
	}
	// git push marks the PR stale: the next turn end looks it up again.
	f.Update(ctx, ev(&proto.Assistant{Message: proto.Message{Content: []proto.ContentBlock{{
		Type: proto.BlockToolUse, ID: "b", Name: "Bash", Input: json.RawMessage(`{"command":"git push -u origin feat"}`)}}}}))
	ff.review = "CHANGES_REQUESTED"
	deliver(t, ctx, f.Update(ctx, ev(&proto.Result{})), f)
	if n := ff.count("gh"); n != 2 || f.pr.pr.Review != prbadge.ChangesRequested {
		t.Errorf("after push: gh=%d review=%v", n, f.pr.pr)
	}
}

func TestFooterPRDisabled(t *testing.T) {
	for name, set := range map[string]func(*exttest.Ctx, *footer){
		"setting": func(c *exttest.Ctx, f *footer) { c.SettingsV.ClaudeM["prStatusFooterEnabled"] = false },
		"traffic": func(c *exttest.Ctx, f *footer) {
			f.env = terminal.Map{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1"}
		},
	} {
		ctx := exttest.NewCtx()
		f := newFooter()
		f.env, f.fetcher = terminal.Map{}, (&fakeForge{}).fetcher()
		set(ctx, f)
		f.Init(ctx)
		if cmd := f.Update(ctx, ext.SessionChangedMsg{Info: ext.SessionInfo{Cwd: "/w"}}); cmd != nil {
			t.Errorf("%s: lookup should be off", name)
		}
	}
}

func TestFooterLinksAndForceHyperlink(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["footerLinksRegexes"] = []any{
		map[string]any{"pattern": `PROJ-(\d+)`, "url": "https://jira.example/browse/PROJ-{number}"},
	}
	f := newFooter()
	f.env, f.fetcher = terminal.Map{"FORCE_HYPERLINK": "0"}, nil
	f.Init(ctx)
	f.Update(ctx, ev(&proto.Assistant{Message: proto.Message{Content: []proto.ContentBlock{
		{Type: proto.BlockText, Text: "Fixed PROJ-12, see also PROJ-7."}}}}))
	out := f.View(ctx, ext.Area{Width: 120}).Text
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "PROJ-7") || !strings.Contains(plain, "PROJ-12") {
		t.Errorf("links = %q", plain)
	}
	if strings.Contains(out, "\x1b]8;") {
		t.Error("FORCE_HYPERLINK=0 must render plain text")
	}
	f.Update(ctx, ev(&proto.ConversationReset{NewConversationID: "n"}))
	if strings.Contains(ansi.Strip(f.View(ctx, ext.Area{Width: 120}).Text), "PROJ") {
		t.Error("reset clears links")
	}
}

func TestStatusLineGetsPR(t *testing.T) {
	ctx := exttest.NewCtx()
	ctx.SettingsV.ClaudeM["statusLine"] = map[string]any{"command": "sl"}
	fx := &fakeSL{out: "ok"}
	c := newTestSL(fx)
	c.Init(ctx)
	feed(t, ctx, c, ext.SessionChangedMsg{Info: ext.SessionInfo{SessionID: "s", Cwd: "/w"}})
	defer c.stop()
	c.Update(ctx, prStatusMsg{
		Repo:   prbadge.Repo{Host: "github.com", Owner: "acme", Name: "widgets", Forge: prbadge.GitHub},
		Status: prbadge.Status{PR: &prbadge.PR{Number: 42, URL: "u", Review: prbadge.Approved}},
	})
	settle(t, ctx, c, ev(&proto.Assistant{}))
	p := fx.last()
	pr, _ := p["pr"].(map[string]any)
	repo, _ := p["workspace"].(map[string]any)["repo"].(map[string]any)
	if pr["number"] != float64(42) || pr["review_state"] != "approved" || repo["owner"] != "acme" {
		t.Errorf("pr = %v repo = %v", pr, repo)
	}
}

func TestChangesPR(t *testing.T) {
	for cmd, want := range map[string]bool{
		"git push": true, "gh pr create --fill": true, "glab mr merge 3": true,
		"git switch main": true, "git status": false, "ls": false,
	} {
		if changesPR(cmd) != want {
			t.Errorf("changesPR(%q) != %v", cmd, want)
		}
	}
}
