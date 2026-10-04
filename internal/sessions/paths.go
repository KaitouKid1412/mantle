package sessions

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// MaxSlugLength is the length at which the engine cuts a project slug and appends a hash.
const MaxSlugLength = 200

// Slug returns the engine's project directory name for cwd: every UTF-16 code unit that
// is not an ASCII letter or digit becomes '-'. Slugs longer than MaxSlugLength are cut and
// suffixed with "-" plus the base-36 absolute value of a 31-multiplier hash of cwd.
//
// The engine works on JavaScript strings, so a character outside the BMP counts (and is
// replaced) twice. cwd should already be canonical (see CanonicalPath).
func Slug(cwd string) string {
	units := utf16.Encode([]rune(cwd))
	b := make([]byte, len(units))
	for i, u := range units {
		if u < 0x80 && isAlnum(byte(u)) {
			b[i] = byte(u)
		} else {
			b[i] = '-'
		}
	}
	if len(b) <= MaxSlugLength {
		return string(b)
	}
	return string(b[:MaxSlugLength]) + "-" + slugHash(units)
}

// slugHash is base36(|h|) where h = h*31 + unit over UTF-16 units in int32 arithmetic.
func slugHash(units []uint16) string {
	var h int32
	for _, u := range units {
		h = h*31 + int32(u)
	}
	v := int64(h)
	if v < 0 {
		v = -v
	}
	return strconv.FormatInt(v, 36)
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// CanonicalPath resolves symlinks the way the engine does before slugging a cwd. If the
// path cannot be resolved it is returned absolute and cleaned.
func CanonicalPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidID reports whether id is shaped like a session id (a UUID). The engine refuses to
// resume anything else.
func ValidID(id string) bool { return uuidRE.MatchString(id) }

// Layout locates Claude Code's files. The zero value resolves the config dir from the
// environment on each call; see DefaultLayout.
type Layout struct {
	// ConfigDir is the Claude Code config dir (CLAUDE_CONFIG_DIR, else ~/.claude).
	ConfigDir string
	// ProjectDirName, when set, replaces the slug as the project directory name for
	// every cwd (the engine's CLAUDE_CODE_PROJECT_DIR_NAME, honoured only together with
	// CLAUDE_CONFIG_DIR).
	ProjectDirName string
}

var (
	projectDirNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	reservedNameRE   = regexp.MustCompile(`(?i)^(?:con|prn|aux|nul|com[0-9]|lpt[0-9])$`)
)

// DefaultLayout reads CLAUDE_CONFIG_DIR and CLAUDE_CODE_PROJECT_DIR_NAME like the engine.
func DefaultLayout() Layout {
	return LayoutFromEnv(os.Getenv)
}

// LayoutFromEnv is DefaultLayout with an injectable environment.
func LayoutFromEnv(getenv func(string) string) Layout {
	var l Layout
	if cfg := strings.TrimSpace(getenv("CLAUDE_CONFIG_DIR")); cfg != "" {
		l.ConfigDir = cfg
		name := getenv("CLAUDE_CODE_PROJECT_DIR_NAME")
		if projectDirNameRE.MatchString(name) && !reservedNameRE.MatchString(name) {
			l.ProjectDirName = name
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		l.ConfigDir = filepath.Join(home, ".claude")
	}
	return l
}

func (l Layout) config() string {
	if l.ConfigDir != "" {
		return l.ConfigDir
	}
	return DefaultLayout().ConfigDir
}

// ProjectsDir is <config>/projects.
func (l Layout) ProjectsDir() string { return filepath.Join(l.config(), "projects") }

// PlansDir is <config>/plans, where plan mode writes <slug>.md.
func (l Layout) PlansDir() string { return filepath.Join(l.config(), "plans") }

// FileHistoryDir is <config>/file-history/<sessionID>, the pre-edit backups used by rewind.
func (l Layout) FileHistoryDir(sessionID string) string {
	return filepath.Join(l.config(), "file-history", sessionID)
}

// ProjectKey is the directory name under ProjectsDir for cwd.
func (l Layout) ProjectKey(cwd string) string {
	if l.ProjectDirName != "" {
		return l.ProjectDirName
	}
	return Slug(cwd)
}

// ProjectDir is the directory the engine writes cwd's sessions to.
func (l Layout) ProjectDir(cwd string) string {
	return filepath.Join(l.ProjectsDir(), l.ProjectKey(cwd))
}

// ProjectDirs returns every existing directory that may hold sessions for cwd: the exact
// project dir and, when the slug was cut at MaxSlugLength, sibling dirs sharing the cut
// prefix whose transcripts record cwd (older engines hashed long paths differently).
func (l Layout) ProjectDirs(cwd string) []string {
	var dirs []string
	if exact := l.ProjectDir(cwd); isDir(exact) {
		dirs = append(dirs, exact)
	}
	slug := Slug(cwd)
	if l.ProjectDirName != "" || len(slug) <= MaxSlugLength {
		return dirs
	}
	prefix := slug[:MaxSlugLength] + "-"
	ents, err := os.ReadDir(l.ProjectsDir())
	if err != nil {
		return dirs
	}
	for _, e := range ents {
		if !e.IsDir() || e.Name() == slug || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		dir := filepath.Join(l.ProjectsDir(), e.Name())
		if dirRecordsCwd(dir, slug[:MaxSlugLength]) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// dirRecordsCwd reports whether any transcript in dir was written for a cwd whose slug
// starts with uncut (the part of the slug before the hash).
func dirRecordsCwd(dir, uncut string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		ht, err := readHeadTail(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		cwd := lastFieldOfType(ht.tail, "relocated", "relocatedCwd")
		if cwd == "" {
			cwd = firstField(ht.head, "cwd")
		}
		if cwd != "" && strings.HasPrefix(Slug(cwd), uncut) {
			return true
		}
	}
	return false
}

// SessionFile is <projectDir>/<id>.jsonl.
func SessionFile(projectDir, id string) string { return filepath.Join(projectDir, id+".jsonl") }

// SessionDir is the per-session folder <projectDir>/<id>/ (subagents, tool results).
func SessionDir(projectDir, id string) string { return filepath.Join(projectDir, id) }

// SubagentsDir is <projectDir>/<id>/subagents.
func SubagentsDir(projectDir, id string) string {
	return filepath.Join(projectDir, id, "subagents")
}

// ToolResultsDir is <projectDir>/<id>/tool-results, where large tool outputs are spilled.
func ToolResultsDir(projectDir, id string) string {
	return filepath.Join(projectDir, id, "tool-results")
}

// ErrNotFound is returned when no transcript exists for a session id.
var ErrNotFound = errors.New("session not found")

// FindSession locates the transcript for id. It looks in cwd's project dirs first, then
// in extraCwds (for example the repo's git worktrees), then in every project. A file
// that exists but is empty does not count. cwd may be empty.
func (l Layout) FindSession(id, cwd string, extraCwds ...string) (string, error) {
	if !ValidID(id) {
		return "", ErrNotFound
	}
	seen := map[string]bool{}
	try := func(dir string) string {
		if seen[dir] {
			return ""
		}
		seen[dir] = true
		p := SessionFile(dir, id)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
			return p
		}
		return ""
	}
	for _, c := range append([]string{cwd}, extraCwds...) {
		if c == "" {
			continue
		}
		for _, d := range l.ProjectDirs(c) {
			if p := try(d); p != "" {
				return p, nil
			}
		}
	}
	ents, err := os.ReadDir(l.ProjectsDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if p := try(filepath.Join(l.ProjectsDir(), e.Name())); p != "" {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// SessionIDFromPath returns the id part of a <id>.jsonl path.
func SessionIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
