package cli

import "github.com/KaitouKid1412/mantle/internal/sessions"

// IndexResolver resolves -c and -r through plan 06's session index (headless sdk-cli
// sessions included, which claude's own -c skips).
type IndexResolver struct{ Index *sessions.Index }

// DefaultResolver uses the session index at Claude Code's default locations.
func DefaultResolver() IndexResolver {
	return IndexResolver{Index: sessions.NewIndex(sessions.DefaultLayout(), sessions.DefaultCachePath())}
}

// Continue returns the most recent session for cwd.
func (r IndexResolver) Continue(cwd string) (string, error) {
	m, err := r.Index.Continue(cwd)
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// Resolve returns the session a -r value names, or a search query for the picker.
func (r IndexResolver) Resolve(cwd, arg string) (string, string, error) {
	res, err := r.Index.Resolve(cwd, arg)
	if err != nil {
		return "", "", err
	}
	if res.Session != nil {
		return res.Session.ID, "", nil
	}
	return "", res.Query, nil
}
