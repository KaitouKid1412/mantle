// Package prbadge finds the open pull request (GitHub) or merge request (GitLab) for the
// current branch, for the footer badge and the status line's `pr` field, and turns issue
// references and footerLinksRegexes matches into links.
//
// It runs `git`, `gh` and `glab` as subprocesses. Fetches block, so the UI calls them
// from a Cmd; the Fetcher caches results per (repo, branch) with a TTL.
package prbadge

import (
	"net/url"
	"strings"
)

// Forge is a code-hosting product.
type Forge string

const (
	ForgeUnknown Forge = ""
	GitHub       Forge = "github"
	GitLab       Forge = "gitlab"
)

// Repo identifies a repository from its git remote.
type Repo struct {
	Host  string // "github.com"
	Owner string // "acme", or "group/subgroup" on GitLab
	Name  string // "widgets"
	Forge Forge
}

// Valid reports whether the remote parsed into host, owner and name.
func (r Repo) Valid() bool { return r.Host != "" && r.Owner != "" && r.Name != "" }

// Slug is "owner/name".
func (r Repo) Slug() string { return r.Owner + "/" + r.Name }

// ParseRemote parses a git remote URL: scp-like (git@host:owner/repo.git), ssh://,
// https://, http:// and git://. Credentials and ports are dropped and a trailing .git
// is removed. GitLab subgroup paths keep the full namespace in Owner.
func ParseRemote(raw string) (Repo, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Repo{}, false
	}
	var host, path string
	if i := strings.Index(raw, "://"); i >= 0 {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return Repo{}, false
		}
		switch u.Scheme {
		case "ssh", "git+ssh", "https", "http", "git":
		default:
			return Repo{}, false
		}
		host, path = u.Hostname(), u.Path
	} else {
		// scp-like: [user@]host:path. A path with a slash before the colon is a local path.
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 || strings.ContainsRune(raw[:colon], '/') {
			return Repo{}, false
		}
		host, path = raw[:colon], raw[colon+1:]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
	}
	host = strings.ToLower(host)
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	segs := strings.Split(path, "/")
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return Repo{}, false
		}
	}
	if host == "" || len(segs) < 2 {
		return Repo{}, false
	}
	forge := ForgeOf(host)
	if forge == GitHub && len(segs) != 2 {
		return Repo{}, false
	}
	return Repo{
		Host:  host,
		Owner: strings.Join(segs[:len(segs)-1], "/"),
		Name:  segs[len(segs)-1],
		Forge: forge,
	}, true
}

// ForgeOf guesses the forge from a host name. Self-managed hosts are recognised when
// their name contains "github" or "gitlab" (ssh host aliases such as github.com-work
// too).
func ForgeOf(host string) Forge {
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "github"):
		return GitHub
	case strings.Contains(h, "gitlab"):
		return GitLab
	}
	return ForgeUnknown
}
