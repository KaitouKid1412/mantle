package chrome

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/term/osc"
	"github.com/KaitouKid1412/mantle/internal/term/prbadge"
	"github.com/KaitouKid1412/mantle/internal/term/statusline"
	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// prFetcher caches PR lookups for the whole process (it is safe for concurrent use).
var prFetcher = &prbadge.Fetcher{}

// prStatusMsg is broadcast after a lookup; the footer shows the badge and the status
// line puts it (and the repo) in its payload.
type prStatusMsg struct {
	Repo   prbadge.Repo // zero outside a git repository with an origin remote
	Branch string
	Status prbadge.Status
}

// fetchPR detects the repository at dir and returns its PR status, from the cache when
// fresh. It blocks (git, gh, glab), so it runs as a Cmd.
func fetchPR(f *prbadge.Fetcher, dir string, force bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		repo, branch, err := f.Detect(ctx, dir)
		if err != nil {
			return prStatusMsg{}
		}
		k := prbadge.KeyOf(repo, branch)
		if force {
			f.Invalidate(k.Repo)
		}
		if st, ok, due := f.Cached(k); ok && !due {
			return prStatusMsg{Repo: repo, Branch: branch, Status: st}
		} else if !due {
			return nil // a lookup for this key is in flight
		}
		return prStatusMsg{Repo: repo, Branch: branch, Status: f.Fetch(ctx, dir, repo, branch)}
	}
}

// prWatch decides when the footer should look the PR up again.
type prWatch struct {
	enabled  bool
	dirty    bool // a git push or PR command ran since the last lookup
	template string
	links    bool // hyperlinks (on unless FORCE_HYPERLINK=0)

	pr   *prbadge.PR
	hint string
	repo prbadge.Repo

	linker   *prbadge.Linker
	found    []prbadge.Link // footerLinksRegexes matches, most recent last
	maxLinks int
}

func (w *prWatch) readSettings(s ext.Settings, env terminal.Env) {
	var setting *bool
	if v, ok := s.Claude("prStatusFooterEnabled"); ok {
		if b, ok := v.(bool); ok {
			setting = &b
		}
	}
	w.enabled = prbadge.Enabled(env, setting)
	w.template = ext.ClaudeString(s, "prUrlTemplate", "")
	w.links = true
	if v, ok := env.Lookup("FORCE_HYPERLINK"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "no", "off":
			w.links = false
		}
	}
	w.linker, _ = prbadge.NewLinker(linkRules(s), w.repo)
	if w.maxLinks == 0 {
		w.maxLinks = 3
	}
}

func linkRules(s ext.Settings) []prbadge.LinkRule {
	v, ok := s.Claude("footerLinksRegexes")
	if !ok {
		return nil
	}
	list, _ := v.([]any)
	var rules []prbadge.LinkRule
	for _, it := range list {
		m, _ := it.(map[string]any)
		p, _ := m["pattern"].(string)
		u, _ := m["url"].(string)
		rules = append(rules, prbadge.LinkRule{Pattern: p, URL: u})
	}
	return rules
}

// observeEngine notes PR-changing commands and scans assistant text for footer links.
// It reports whether the visible links changed and whether a lookup is due now.
func (w *prWatch) observeEngine(e proto.Event) (changed, lookup bool) {
	switch ev := e.(type) {
	case *proto.Assistant:
		for _, b := range ev.Message.Content {
			if tu, ok := b.ToolUse(); ok && tu.Name == "Bash" {
				var in struct {
					Command string `json:"command"`
				}
				_ = tu.DecodeInput(&in)
				if changesPR(in.Command) {
					w.dirty = true
				}
			}
			if b.Type == proto.BlockText && ev.ParentToolUseID == "" && w.linker != nil {
				for _, l := range w.linker.Find(b.Text) {
					w.addLink(l)
					changed = true
				}
			}
		}
	case *proto.Result:
		return changed, true
	case *proto.ConversationReset:
		w.found = nil
		return true, false
	}
	return changed, false
}

func (w *prWatch) addLink(l prbadge.Link) {
	for i, x := range w.found {
		if x.URL == l.URL {
			w.found = append(w.found[:i], w.found[i+1:]...)
			break
		}
	}
	w.found = append(w.found, l)
	if len(w.found) > w.maxLinks {
		w.found = w.found[len(w.found)-w.maxLinks:]
	}
}

// apply stores a lookup result; it reports whether the footer changed.
func (w *prWatch) apply(m prStatusMsg, s ext.Settings) bool {
	before := *w
	w.repo = m.Repo
	w.pr, w.hint = m.Status.PR, m.Status.Hint
	if before.repo != w.repo {
		w.linker, _ = prbadge.NewLinker(linkRules(s), w.repo)
	}
	return !samePR(before.pr, w.pr) || before.hint != w.hint || before.repo != w.repo
}

func samePR(a, b *prbadge.PR) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// changesPR reports whether a shell command can change the branch's PR state.
func changesPR(cmd string) bool {
	for _, s := range []string{"git push", "gh pr ", "glab mr ", "git checkout", "git switch"} {
		if strings.Contains(cmd, s) {
			return true
		}
	}
	return false
}

// segments returns the footer pieces: the badge (or setup hint) and footer links.
func (w *prWatch) segments(t *theme.Theme) []segment {
	var out []segment
	if w.pr != nil {
		label := w.pr.Label()
		styled := t.Fg(reviewToken(w.pr.Review)).Underline(true).Render(label)
		if w.links {
			styled = osc.Hyperlink(w.pr.LinkURL(w.template), styled, "")
		}
		out = append(out, segment{Plain: label, Styled: styled, Drop: 1})
	} else if w.hint != "" {
		out = append(out, seg(t, theme.Inactive, w.hint, 6))
	}
	for i := len(w.found) - 1; i >= 0; i-- {
		l := w.found[i]
		styled := t.Paint(theme.Inactive, l.Text)
		if w.links {
			styled = osc.Hyperlink(l.URL, styled, "")
		}
		out = append(out, segment{Plain: l.Text, Styled: styled, Drop: 5})
	}
	return out
}

func reviewToken(r prbadge.ReviewState) theme.Token {
	switch r {
	case prbadge.Approved:
		return theme.Success
	case prbadge.ChangesRequested:
		return theme.Error
	case prbadge.Draft:
		return theme.Inactive
	}
	return theme.Warning
}

// statusLinePR converts a lookup into the status line's pr and workspace.repo.
func statusLinePR(m prStatusMsg) (*statusline.PR, *statusline.Repo) {
	var repo *statusline.Repo
	if m.Repo.Valid() {
		repo = &statusline.Repo{Host: m.Repo.Host, Owner: m.Repo.Owner, Name: m.Repo.Name}
	}
	p := m.Status.PR
	if p == nil {
		return nil, repo
	}
	return &statusline.PR{Number: p.Number, URL: p.URL, ReviewState: string(p.Review), Kind: p.Kind}, repo
}
