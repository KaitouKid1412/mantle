package sessions

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultCachePath is ~/.mantle/cache/sessions.idx.
func DefaultCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mantle", "cache", "sessions.idx")
}

// Index lists sessions with metadata read from file heads and tails (ReadMeta). Results
// are cached on disk keyed by (path, size, mtime), so a warm listing only stats files.
// Projects are indexed lazily: only the directories asked for are read. It is safe for
// concurrent use.
type Index struct {
	layout    Layout
	cachePath string
	workers   int

	mu      sync.Mutex
	loaded  bool
	entries map[string]cacheEntry
	dirty   bool
}

type cacheEntry struct {
	Size  int64       `json:"size"`
	Mtime int64       `json:"mtime"` // UnixNano
	Meta  SessionMeta `json:"meta"`
}

type cacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]cacheEntry `json:"entries"`
}

// cacheVersion changes whenever SessionMeta or its extraction changes meaning.
const cacheVersion = 2

// NewIndex returns an index over l's projects. cachePath "" disables the disk cache.
func NewIndex(l Layout, cachePath string) *Index {
	return &Index{layout: l, cachePath: cachePath, workers: min(8, 2*runtime.GOMAXPROCS(0))}
}

func (ix *Index) load() {
	if ix.loaded {
		return
	}
	ix.loaded = true
	ix.entries = map[string]cacheEntry{}
	if ix.cachePath == "" {
		return
	}
	b, err := os.ReadFile(ix.cachePath)
	if err != nil {
		return
	}
	var cf cacheFile
	if json.Unmarshal(b, &cf) != nil || cf.Version != cacheVersion || cf.Entries == nil {
		return
	}
	ix.entries = cf.Entries
}

// Save writes the cache if it changed. The write is atomic (temp file plus rename).
func (ix *Index) Save() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.cachePath == "" || !ix.dirty {
		return nil
	}
	b, err := json.Marshal(cacheFile{Version: cacheVersion, Entries: ix.entries})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ix.cachePath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(ix.cachePath), ".sessions.idx.*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), ix.cachePath); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	ix.dirty = false
	return nil
}

// Dir lists the sessions in one project directory, newest first. Files with no
// messages are left out.
func (ix *Index) Dir(projectDir string) ([]SessionMeta, error) {
	ents, err := os.ReadDir(projectDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	type file struct {
		path  string
		size  int64
		mtime int64
	}
	var files []file
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || !ValidID(strings.TrimSuffix(name, ".jsonl")) {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		files = append(files, file{filepath.Join(projectDir, name), fi.Size(), fi.ModTime().UnixNano()})
	}

	out := make([]SessionMeta, len(files))
	var miss []int
	ix.mu.Lock()
	ix.load()
	present := make(map[string]bool, len(files))
	for i, f := range files {
		present[f.path] = true
		if c, ok := ix.entries[f.path]; ok && c.Size == f.size && c.Mtime == f.mtime {
			out[i] = c.Meta
			continue
		}
		miss = append(miss, i)
	}
	prefix := projectDir + string(filepath.Separator)
	for p := range ix.entries {
		if strings.HasPrefix(p, prefix) && !strings.ContainsRune(p[len(prefix):], filepath.Separator) && !present[p] {
			delete(ix.entries, p)
			ix.dirty = true
		}
	}
	ix.mu.Unlock()

	if len(miss) > 0 {
		var wg sync.WaitGroup
		next := make(chan int)
		for w := 0; w < min(ix.workers, len(miss)); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range next {
					ht, err := readHeadTail(files[i].path)
					if err != nil {
						continue
					}
					out[i] = metaFrom(files[i].path, ht)
				}
			}()
		}
		for _, i := range miss {
			next <- i
		}
		close(next)
		wg.Wait()

		ix.mu.Lock()
		for _, i := range miss {
			if out[i].Path == "" {
				continue
			}
			out[i].ProjectDir = projectDir
			// Key the cache by the stat taken before reading: if the file grew meanwhile,
			// the next listing sees a new size and reads it again.
			ix.entries[files[i].path] = cacheEntry{Size: files[i].size, Mtime: files[i].mtime, Meta: out[i]}
			ix.dirty = true
		}
		ix.mu.Unlock()
	}

	res := out[:0]
	for _, m := range out {
		if m.Path != "" && m.HasMessages {
			m.ProjectDir = projectDir
			res = append(res, m)
		}
	}
	SortNewest(res)
	return res, nil
}

// Project lists the sessions of cwd and of extraCwds (for example its git worktrees),
// newest first.
func (ix *Index) Project(cwd string, extraCwds ...string) ([]SessionMeta, error) {
	var dirs []string
	seen := map[string]bool{}
	for _, c := range append([]string{cwd}, extraCwds...) {
		for _, d := range ix.layout.ProjectDirs(c) {
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	return ix.dirs(dirs)
}

// ProjectDirInfo is one directory under the projects dir.
type ProjectDirInfo struct {
	Path     string
	Modified time.Time
}

// ProjectDirs lists every project directory, most recently modified first, so a UI can
// index them one at a time.
func (ix *Index) ProjectDirs() ([]ProjectDirInfo, error) {
	root := ix.layout.ProjectsDir()
	ents, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []ProjectDirInfo
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		info := ProjectDirInfo{Path: filepath.Join(root, e.Name())}
		if fi, err := e.Info(); err == nil {
			info.Modified = fi.ModTime()
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// All lists the sessions of every project, newest first.
func (ix *Index) All() ([]SessionMeta, error) {
	pds, err := ix.ProjectDirs()
	if err != nil {
		return nil, err
	}
	dirs := make([]string, len(pds))
	for i, p := range pds {
		dirs[i] = p.Path
	}
	return ix.dirs(dirs)
}

func (ix *Index) dirs(dirs []string) ([]SessionMeta, error) {
	var all []SessionMeta
	seen := map[string]bool{}
	var errs []error
	for _, d := range dirs {
		ms, err := ix.Dir(d)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, m := range ms {
			if !seen[m.ID] {
				seen[m.ID] = true
				all = append(all, m)
			}
		}
	}
	SortNewest(all)
	return all, errors.Join(errs...)
}

// Get returns one session's metadata, from the cache when the file is unchanged.
func (ix *Index) Get(path string) (SessionMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return SessionMeta{}, err
	}
	ix.mu.Lock()
	ix.load()
	c, ok := ix.entries[path]
	ix.mu.Unlock()
	if ok && c.Size == fi.Size() && c.Mtime == fi.ModTime().UnixNano() {
		return c.Meta, nil
	}
	m, err := ReadMeta(path)
	if err != nil {
		return SessionMeta{}, err
	}
	m.ProjectDir = filepath.Dir(path)
	ix.mu.Lock()
	ix.entries[path] = cacheEntry{Size: fi.Size(), Mtime: fi.ModTime().UnixNano(), Meta: m}
	ix.dirty = true
	ix.mu.Unlock()
	return m, nil
}

// SortNewest sorts sessions by file modification time, newest first (Claude Code's
// picker order), then by last activity.
func SortNewest(ms []SessionMeta) {
	sort.SliceStable(ms, func(i, j int) bool {
		a, b := ms[i].Modified, ms[j].Modified
		if a.Equal(b) {
			return ms[i].LastActive.After(ms[j].LastActive)
		}
		return a.After(b)
	})
}
