package prbadge

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// LinkRule is one footerLinksRegexes entry: a regex over the conversation's output and
// a URL template with {number} (first capture group, or the whole match), {url} (the
// whole match), {owner} and {repo} (from the git remote).
type LinkRule struct {
	Pattern string `json:"pattern"`
	URL     string `json:"url"`
}

// Link is a clickable footer link.
type Link struct {
	Text string // the matched text, shown in the footer
	URL  string
}

type rule struct {
	re  *regexp.Regexp
	url string
}

// Linker applies footerLinksRegexes.
type Linker struct {
	rules []rule
	repo  Repo
}

// NewLinker compiles rules. Invalid patterns (Go uses RE2: no lookaround or
// backreferences) are skipped and reported.
func NewLinker(rules []LinkRule, repo Repo) (*Linker, []error) {
	l := &Linker{repo: repo}
	var errs []error
	for i, r := range rules {
		if r.Pattern == "" || r.URL == "" {
			errs = append(errs, fmt.Errorf("footerLinksRegexes[%d]: pattern and url are required", i))
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			errs = append(errs, fmt.Errorf("footerLinksRegexes[%d]: %w", i, err))
			continue
		}
		l.rules = append(l.rules, rule{re: re, url: r.URL})
	}
	return l, errs
}

// Find returns the links for every match in text, in order of first appearance and
// without duplicate URLs. Templates that need {owner}/{repo} produce nothing when the
// remote is unknown.
func (l *Linker) Find(text string) []Link {
	if l == nil {
		return nil
	}
	type hit struct {
		pos  int
		link Link
	}
	var hits []hit
	for _, r := range l.rules {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			whole := text[m[0]:m[1]]
			if whole == "" {
				continue
			}
			number := whole
			if len(m) >= 4 && m[2] >= 0 {
				number = text[m[2]:m[3]]
			}
			u, ok := l.expand(r.url, number, whole)
			if !ok {
				continue
			}
			hits = append(hits, hit{m[0], Link{Text: whole, URL: u}})
		}
	}
	// Order by position across rules.
	slices.SortStableFunc(hits, func(a, b hit) int { return a.pos - b.pos })
	var out []Link
	seen := map[string]bool{}
	for _, h := range hits {
		if !seen[h.link.URL] {
			seen[h.link.URL] = true
			out = append(out, h.link)
		}
	}
	return out
}

func (l *Linker) expand(tpl, number, whole string) (string, bool) {
	if (strings.Contains(tpl, "{owner}") || strings.Contains(tpl, "{repo}")) && !l.repo.Valid() {
		return "", false
	}
	u := strings.NewReplacer(
		"{number}", number,
		"{url}", whole,
		"{owner}", l.repo.Owner,
		"{repo}", l.repo.Name,
	).Replace(tpl)
	u = strings.Map(func(r rune) rune {
		if r <= ' ' || r == 0x7f {
			return -1
		}
		return r
	}, u)
	return u, u != ""
}

// IssueRef is an owner/repo#123 reference found in text.
type IssueRef struct {
	Start, End int // byte offsets of the reference in the text
	Text       string
	URL        string
}

// The character before the reference must not continue a path or word, so a nested
// group/subgroup/project#1 and a bare #1 stay plain.
var issueRe = regexp.MustCompile(`(^|[^A-Za-z0-9_./-])([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)/([A-Za-z0-9._-]+)#([0-9]+)\b`)

// IssueRefs finds owner/repo#N references and links them for the host of the current
// repository (github.com when unknown). Hosts without a predictable issue URL get none.
// Callers skip code spans and blocks themselves.
func IssueRefs(text string, repo Repo) []IssueRef {
	host := repo.Host
	if host == "" {
		host = "github.com"
	}
	switch host {
	case "bitbucket.org", "codeberg.org", "gitea.com":
		return nil
	}
	var out []IssueRef
	for _, m := range issueRe.FindAllStringSubmatchIndex(text, -1) {
		start := m[4]
		owner, name, num := text[m[4]:m[5]], text[m[6]:m[7]], text[m[8]:m[9]]
		if strings.HasSuffix(name, ".") {
			continue
		}
		if _, err := strconv.Atoi(num); err != nil {
			continue
		}
		var u string
		if host == "gitlab.com" {
			u = fmt.Sprintf("https://gitlab.com/%s/%s/-/issues/%s", owner, name, num)
		} else {
			u = fmt.Sprintf("https://%s/%s/%s/issues/%s", host, owner, name, num)
		}
		out = append(out, IssueRef{Start: start, End: m[1], Text: text[start:m[1]], URL: u})
	}
	return out
}

// LinkifyIssues rewrites every owner/repo#N reference in text with link(url, ref).
// Pass osc.Hyperlink-based link functions; with hyperlinks off, leave text alone.
func LinkifyIssues(text string, repo Repo, link func(url, text string) string) string {
	refs := IssueRefs(text, repo)
	if len(refs) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, r := range refs {
		b.WriteString(text[last:r.Start])
		b.WriteString(link(r.URL, r.Text))
		last = r.End
	}
	b.WriteString(text[last:])
	return b.String()
}
