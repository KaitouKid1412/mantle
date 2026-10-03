package launcher

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Manifest describes one installed build. It is written once, next to the
// binary, and never changed.
type Manifest struct {
	BuildID string `json:"build_id"`
	// GitSHA is the commit of ~/.mantle/src the build was made from.
	GitSHA string `json:"git_sha,omitempty"`
	// UpstreamSHA is the upstream (origin/main) commit the mods sit on.
	UpstreamSHA string `json:"upstream_sha,omitempty"`
	// Mods lists the Mantle-Mod ids contained in the build.
	Mods      []string `json:"mods,omitempty"`
	GoVersion string   `json:"go_version,omitempty"`
	// ClaudeVersion is the engine version the build was tested with.
	ClaudeVersion string    `json:"claude_version,omitempty"`
	Created       time.Time `json:"created"`
	// Source says what produced the build: "install", "promote", "undo", "update", ...
	Source   string           `json:"source,omitempty"`
	Pipeline *PipelineSummary `json:"pipeline,omitempty"`
}

// PipelineSummary is the short form of a selfmod pipeline report.
type PipelineSummary struct {
	OK       bool          `json:"ok"`
	BuildDir string        `json:"build_dir,omitempty"`
	Steps    []StepSummary `json:"steps,omitempty"`
}

// StepSummary is one pipeline step in a PipelineSummary.
type StepSummary struct {
	Step       string `json:"step"`
	OK         bool   `json:"ok"`
	Skipped    bool   `json:"skipped,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Version is an installed build in versions/<id>/.
type Version struct {
	ID       string
	Dir      string
	Manifest Manifest
	// ManifestErr is set when manifest.json is missing or unreadable.
	ManifestErr error
}

// Binary is the path of the version's mantle-ui.
func (v Version) Binary() string { return filepath.Join(v.Dir, UIBinaryName) }

// Created is the manifest's creation time, or the folder's mtime if the
// manifest is unreadable.
func (v Version) Created() time.Time {
	if !v.Manifest.Created.IsZero() {
		return v.Manifest.Created
	}
	if fi, err := os.Stat(v.Dir); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// Errors returned by the store.
var (
	ErrVersionExists  = errors.New("version already exists")
	ErrNoVersion      = errors.New("no such version")
	ErrNotInstalled   = errors.New("no current build is installed")
	ErrInvalidBuildID = errors.New("invalid build id")
	errUnresolvedLink = errors.New("link does not point into versions/")
)

var buildIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidBuildID reports whether id can name a version folder.
func ValidBuildID(id string) bool { return buildIDRe.MatchString(id) }

// NewBuildID returns a sortable build id: UTC time plus a short git sha.
func NewBuildID(t time.Time, sha string) string {
	id := t.UTC().Format("20060102-150405")
	if len(sha) > 7 {
		sha = sha[:7]
	}
	if sha != "" && ValidBuildID("x"+sha) {
		id += "-" + sha
	}
	return id
}

// Store manages versions/ and the current and last-good links.
type Store struct {
	L Layout
}

// UniqueID returns base, or base with a numeric suffix if a version with that
// id already exists.
func (s Store) UniqueID(base string) string {
	id := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(s.L.Version(id)); errors.Is(err, fs.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, n)
	}
}

// Install copies uiBin into a new immutable version folder versions/<id>/
// together with the manifest. The folder is assembled under a temporary name
// and renamed into place, so a version folder is always complete.
func (s Store) Install(id, uiBin string, m Manifest) (Version, error) {
	if !ValidBuildID(id) {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalidBuildID, id)
	}
	if err := os.MkdirAll(s.L.Versions(), 0o755); err != nil {
		return Version{}, err
	}
	final := s.L.Version(id)
	if _, err := os.Lstat(final); err == nil {
		return Version{}, fmt.Errorf("%w: %s", ErrVersionExists, id)
	}
	tmp, err := os.MkdirTemp(s.L.Versions(), ".tmp-"+id+"-")
	if err != nil {
		return Version{}, err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(tmp)
		}
	}()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return Version{}, err
	}
	if err := copyFile(uiBin, filepath.Join(tmp, UIBinaryName), 0o555); err != nil {
		return Version{}, fmt.Errorf("copy %s: %w", uiBin, err)
	}
	m.BuildID = id
	if m.Created.IsZero() {
		m.Created = time.Now().UTC()
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Version{}, err
	}
	if err := writeFileSync(filepath.Join(tmp, ManifestName), append(data, '\n'), 0o444); err != nil {
		return Version{}, err
	}
	syncDir(tmp)
	if err := os.Rename(tmp, final); err != nil {
		if _, serr := os.Lstat(final); serr == nil {
			return Version{}, fmt.Errorf("%w: %s", ErrVersionExists, id)
		}
		return Version{}, err
	}
	syncDir(s.L.Versions())
	ok = true
	return s.Get(id)
}

// Get loads versions/<id>.
func (s Store) Get(id string) (Version, error) {
	if !ValidBuildID(id) {
		return Version{}, fmt.Errorf("%w: %q", ErrInvalidBuildID, id)
	}
	dir := s.L.Version(id)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, id)
	}
	v := Version{ID: id, Dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err == nil {
		err = json.Unmarshal(data, &v.Manifest)
	}
	v.ManifestErr = err
	return v, nil
}

// List returns every installed version, newest first.
func (s Store) List() ([]Version, error) {
	ents, err := os.ReadDir(s.L.Versions())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Version
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || !ValidBuildID(e.Name()) {
			continue
		}
		v, err := s.Get(e.Name())
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b Version) int {
		if c := b.Created().Compare(a.Created()); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// Current resolves the current link.
func (s Store) Current() (Version, error) {
	v, err := s.resolve(s.L.Current())
	if errors.Is(err, fs.ErrNotExist) {
		return Version{}, ErrNotInstalled
	}
	return v, err
}

// LastGood resolves the last-good link.
func (s Store) LastGood() (Version, error) { return s.resolve(s.L.LastGood()) }

// CurrentID returns the id current points to, or "" if it is unset.
func (s Store) CurrentID() string { id, _ := s.linkID(s.L.Current()); return id }

// LastGoodID returns the id last-good points to, or "" if it is unset.
func (s Store) LastGoodID() string { id, _ := s.linkID(s.L.LastGood()); return id }

func (s Store) linkID(link string) (string, error) {
	target, err := readlinkRetry(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	target = filepath.Clean(target)
	if filepath.Dir(target) != filepath.Clean(s.L.Versions()) {
		return "", fmt.Errorf("%s: %w", link, errUnresolvedLink)
	}
	return filepath.Base(target), nil
}

// readlinkRetry is os.Readlink, retried on EINVAL: on macOS, readlink(2) on a
// symlink that rename(2) is replacing can fail with EINVAL although the path
// is a symlink before and after.
func readlinkRetry(link string) (string, error) {
	for i := 0; ; i++ {
		target, err := os.Readlink(link)
		if err == nil || i == 100 || !errors.Is(err, syscall.EINVAL) {
			return target, err
		}
		if fi, lerr := os.Lstat(link); lerr != nil || fi.Mode()&fs.ModeSymlink == 0 {
			return target, err
		}
		runtime.Gosched()
	}
}

func (s Store) resolve(link string) (Version, error) {
	id, err := s.linkID(link)
	if err != nil {
		return Version{}, err
	}
	return s.Get(id)
}

// SetCurrent atomically points current at versions/<id>.
func (s Store) SetCurrent(id string) error { return s.flip(s.L.Current(), id) }

// SetLastGood atomically points last-good at versions/<id>.
func (s Store) SetLastGood(id string) error { return s.flip(s.L.LastGood(), id) }

// flip replaces link with a relative symlink to versions/<id> using rename(2),
// so readers see either the old or the new target, never a missing link.
func (s Store) flip(link, id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	target := filepath.Join("versions", id)
	if rel, err := filepath.Rel(filepath.Dir(link), s.L.Version(id)); err == nil {
		target = rel
	}
	tmp := fmt.Sprintf("%s.tmp-%d-%s", link, os.Getpid(), randHex(4))
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, link); err != nil {
		os.Remove(tmp)
		return err
	}
	syncDir(filepath.Dir(link))
	return nil
}

// GC removes old versions. It keeps the newest keep versions, current,
// last-good, and every version referenced by a live run file. It also removes
// leftover temporary folders older than an hour. It returns the removed ids.
func (s Store) GC(keep int, alive func(pid int) bool) ([]string, error) {
	if alive == nil {
		alive = ProcessAlive
	}
	vs, err := s.List()
	if err != nil {
		return nil, err
	}
	protect := map[string]bool{s.CurrentID(): true, s.LastGoodID(): true}
	runs, _ := s.L.RunFiles()
	for _, rf := range runs {
		if rf.Version != "" && alive(rf.PID) {
			protect[rf.Version] = true
		}
	}
	var removed []string
	var errs []error
	for i, v := range vs {
		if i < keep || protect[v.ID] {
			continue
		}
		if err := os.RemoveAll(v.Dir); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, v.ID)
	}
	if ents, err := os.ReadDir(s.L.Versions()); err == nil {
		for _, e := range ents {
			if !strings.HasPrefix(e.Name(), ".tmp-") {
				continue
			}
			if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > time.Hour {
				os.RemoveAll(filepath.Join(s.L.Versions(), e.Name()))
			}
		}
	}
	return removed, errors.Join(errs...)
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

func writeFileSync(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, perm)
}

// writeFileAtomic writes data to a temporary file next to path and renames it
// over path.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp-%d-%s", path, os.Getpid(), randHex(4))
	if err := writeFileSync(tmp, data, perm); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
