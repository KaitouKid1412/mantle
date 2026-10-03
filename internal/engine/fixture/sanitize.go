package fixture

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Sanitizer removes personal data from recorded lines so fixtures can be committed:
// home paths become /home/user, the username becomes "user", emails become
// user@example.com, API keys and tokens are redacted, and session-specific ids
// (UUIDs, msg_/toolu_/req_ ids) are remapped to stable placeholders in order of
// first appearance, so references between lines still match.
//
// It works on the raw JSON text; every replacement keeps the JSON valid.
type Sanitizer struct {
	mu      sync.Mutex
	pairs   []string // old, new (longest first)
	ids     map[string]string
	counter map[string]int
}

// NewSanitizer returns a sanitiser for the current user (home dir, username,
// temp dirs), plus extra literal replacements (old, new, old, new, ...).
func NewSanitizer(extra ...string) *Sanitizer {
	s := &Sanitizer{ids: map[string]string{}, counter: map[string]int{}}
	add := func(old, new string) {
		if old != "" && old != new && len(old) > 2 {
			s.pairs = append(s.pairs, old, new)
		}
	}
	for i := 0; i+1 < len(extra); i += 2 {
		add(extra[i], extra[i+1])
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "/home/user")
		add(slug(home), slug("/home/user"))
		if real, err := filepath.EvalSymlinks(home); err == nil {
			add(real, "/home/user")
		}
	}
	for _, d := range []string{os.TempDir(), "/private" + os.TempDir()} {
		d = strings.TrimRight(d, "/")
		add(d, "/tmp")
		add(slug(d), slug("/tmp"))
	}
	if u, err := user.Current(); err == nil {
		add(u.Username, "user")
		if u.Name != "" {
			add(u.Name, "User")
		}
	}
	s.sortPairs()
	return s
}

// slug mirrors the engine's project directory naming (non-alphanumerics → "-").
func slug(p string) string {
	return nonAlnum.ReplaceAllString(p, "-")
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

func (s *Sanitizer) sortPairs() {
	type pr struct{ old, new string }
	var ps []pr
	for i := 0; i+1 < len(s.pairs); i += 2 {
		ps = append(ps, pr{s.pairs[i], s.pairs[i+1]})
	}
	sort.SliceStable(ps, func(i, j int) bool { return len(ps[i].old) > len(ps[j].old) })
	s.pairs = s.pairs[:0]
	for _, p := range ps {
		s.pairs = append(s.pairs, p.old, p.new)
	}
}

var (
	emailRe  = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	keyRe    = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}`)
	bearerRe = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]{12,}`)
	uuidRe   = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	idRe     = regexp.MustCompile(`\b(msg|toolu|srvtoolu|req|mcpsrv)_[A-Za-z0-9]{8,}\b`)
)

// Line sanitises one JSON line.
func (s *Sanitizer) Line(line []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := string(line)
	for i := 0; i+1 < len(s.pairs); i += 2 {
		t = strings.ReplaceAll(t, s.pairs[i], s.pairs[i+1])
	}
	t = emailRe.ReplaceAllString(t, "user@example.com")
	t = keyRe.ReplaceAllString(t, "sk-ant-REDACTED")
	t = bearerRe.ReplaceAllString(t, "${1}REDACTED")
	t = uuidRe.ReplaceAllStringFunc(t, func(u string) string { return s.remap("uuid", strings.ToLower(u)) })
	t = idRe.ReplaceAllStringFunc(t, func(id string) string {
		kind := id[:strings.IndexByte(id, '_')]
		return s.remap(kind, id)
	})
	return []byte(t)
}

func (s *Sanitizer) remap(kind, v string) string {
	if r, ok := s.ids[v]; ok {
		return r
	}
	if s.isPlaceholder(kind, v) {
		return v
	}
	s.counter[kind]++
	n := s.counter[kind]
	var r string
	if kind == "uuid" {
		r = fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
	} else {
		r = fmt.Sprintf("%s_%08d", kind, n)
	}
	s.ids[v] = r
	s.ids[r] = r
	return r
}

func (s *Sanitizer) isPlaceholder(kind, v string) bool {
	if kind == "uuid" {
		return strings.HasPrefix(v, "00000000-0000-4000-8000-")
	}
	return false
}
