package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Resolution is what a resume argument names.
type Resolution struct {
	// Session is set when the argument named exactly one session.
	Session *SessionMeta
	// Otherwise the picker opens with Query; Matches are the sessions it matches,
	// current project first (all projects only if the current one has none).
	Query   string
	Matches []SessionMeta
}

// Continue returns the most recent session for cwd (or its extra cwds), including
// headless (sdk-cli) ones, the way `-c` picks it. ErrNotFound if there is none.
func (ix *Index) Continue(cwd string, extraCwds ...string) (SessionMeta, error) {
	ms, err := ix.Project(cwd, extraCwds...)
	if err != nil && len(ms) == 0 {
		return SessionMeta{}, err
	}
	if len(ms) == 0 {
		return SessionMeta{}, ErrNotFound
	}
	return ms[0], nil
}

// Resolve interprets a `-r`/`/resume` argument:
//   - "" opens the picker;
//   - a session id, or the path of a transcript file, names that session (ErrNotFound
//     if it does not exist);
//   - a name that exactly one session carries (its custom title or agent name, any
//     case) names that session;
//   - anything else is a search query for the picker.
func (ix *Index) Resolve(cwd, arg string, extraCwds ...string) (Resolution, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Resolution{}, nil
	}
	if ValidID(arg) {
		p, err := ix.layout.FindSession(strings.ToLower(arg), cwd, extraCwds...)
		if err != nil {
			return Resolution{}, err
		}
		m, err := ix.Get(p)
		if err != nil {
			return Resolution{}, err
		}
		return Resolution{Session: &m}, nil
	}
	if strings.HasSuffix(arg, ".jsonl") && filepath.IsAbs(arg) {
		if fi, err := os.Stat(arg); err == nil && fi.Mode().IsRegular() {
			m, err := ix.Get(arg)
			if err != nil {
				return Resolution{}, err
			}
			return Resolution{Session: &m}, nil
		}
		return Resolution{}, ErrNotFound
	}

	local, _ := ix.Project(cwd, extraCwds...)
	if m, ok := uniqueName(local, arg); ok {
		return Resolution{Session: &m}, nil
	}
	res := Resolution{Query: arg, Matches: Filter(local, arg)}
	if len(res.Matches) == 0 {
		all, _ := ix.All()
		if m, ok := uniqueName(all, arg); ok {
			return Resolution{Session: &m}, nil
		}
		res.Matches = Filter(all, arg)
	}
	return res, nil
}

func uniqueName(ms []SessionMeta, name string) (SessionMeta, bool) {
	var found []SessionMeta
	for _, m := range ms {
		if strings.EqualFold(m.CustomTitle, name) || strings.EqualFold(m.AgentName, name) {
			found = append(found, m)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return SessionMeta{}, false
}

// SearchText is the text a picker search runs over: title, prompts, branch, tag, PR
// and id.
func (m SessionMeta) SearchText() string {
	parts := []string{m.Title(), m.FirstPrompt, m.LastPrompt, m.GitBranch, m.Tag, m.PRURL, m.ID}
	if m.PRNumber != 0 {
		parts = append(parts, "#"+strconv.Itoa(m.PRNumber))
	}
	return strings.Join(parts, "\n")
}

// Matches reports whether every whitespace-separated term of query occurs in
// m.SearchText(), ignoring case.
func (m SessionMeta) Matches(query string) bool {
	text := strings.ToLower(m.SearchText())
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

// Filter returns the sessions matching query, keeping their order.
func Filter(ms []SessionMeta, query string) []SessionMeta {
	var out []SessionMeta
	for _, m := range ms {
		if m.Matches(query) {
			out = append(out, m)
		}
	}
	return out
}
