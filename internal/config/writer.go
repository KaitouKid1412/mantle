package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
)

// Writer changes settings files safely:
//   - an exclusive flock per file (lock files live in ~/.mantle/locks, never next to
//     the user's file);
//   - the file is re-read under the lock and fn applied to the freshest contents;
//   - the result replaces the file atomically (temp file + rename), keeping its mode;
//   - symlinked files (dotfile repos) are written through the link;
//   - key order, indentation and number spelling are kept (see Encode);
//   - an existing file that is not a JSON object is never overwritten;
//   - ~/.claude.json is refused, including through a symlink.
type Writer struct {
	Paths Paths
}

// Update locks path, decodes it (missing or empty = empty object), calls fn and writes
// the result. Nothing is written when fn fails or leaves the document unchanged.
func (w Writer) Update(path string, fn func(map[string]any) (map[string]any, error)) error {
	if w.Paths.forbidden(path) {
		return ErrForbidden
	}
	target, err := resolveLinks(path)
	if err != nil {
		return err
	}
	if w.Paths.forbidden(target) {
		return ErrForbidden
	}
	unlock, err := w.lock(target)
	if err != nil {
		return err
	}
	defer unlock()

	raw, mode, err := readFile(target)
	if err != nil {
		return err
	}
	doc, order, err := Decode(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	before := deepCopy(doc)
	next, err := fn(doc)
	if err != nil {
		return err
	}
	if next == nil {
		next = map[string]any{}
	}
	if len(raw) > 0 && reflect.DeepEqual(before, next) {
		return nil
	}
	out, err := Encode(next, order)
	if err != nil {
		return err
	}
	return writeAtomic(target, out, mode)
}

// Set writes one top-level key (nil deletes it).
func (w Writer) Set(path, key string, value any) error {
	return w.Update(path, func(doc map[string]any) (map[string]any, error) {
		if value == nil {
			delete(doc, key)
		} else {
			doc[key] = value
		}
		return doc, nil
	})
}

func resolveLinks(path string) (string, error) {
	for range 32 {
		fi, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			return path, nil
		}
		dest, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(path), dest)
		}
		path = dest
	}
	return "", fmt.Errorf("%s: too many symlinks", path)
}

func readFile(path string) ([]byte, fs.FileMode, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0o644, nil
	}
	if err != nil {
		return nil, 0, err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return b, mode, nil
}

func (w Writer) lock(target string) (func(), error) {
	dir := w.Paths.LocksDir()
	if w.Paths.MantleDir == "" {
		dir = filepath.Join(os.TempDir(), "mantle-locks")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	sum := sha256.Sum256([]byte(abs))
	f, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(sum[:8])+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func deepCopy(v map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for k, x := range v {
		out[k] = deepCopyAny(x)
	}
	return out
}

func deepCopyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCopy(t)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = deepCopyAny(x)
		}
		return out
	}
	return v
}
