// Package settingsfile writes Claude Code settings files safely: under an exclusive
// lock, re-reading the file just before the write, applying the change to the freshest
// contents, and replacing the file atomically. Key order and two-space indentation are
// kept so a user's hand-edited file stays recognisable, symlinked files (dotfile repos)
// are written through the link, and an unreadable file is never overwritten.
//
// It stands in for plan 01's config writer until that lands; the API is the shape the
// settings panels need (Update with a mutation function, Apply for a patch).
package settingsfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// Writer serialises writes to settings files.
type Writer struct {
	// LockDir holds the lock files (~/.mantle/locks). Locks guard mantle processes
	// against each other; the atomic rename guards readers.
	LockDir string
	// Env locates scope files and rejects forbidden targets.
	Env patch.Env
}

// ErrInvalid is returned when the existing file is not a JSON object; mantle refuses to
// overwrite it.
var ErrInvalid = errors.New("settings file is not valid JSON; fix or remove it first")

// Apply writes a patch to its scope's file.
func (w Writer) Apply(p patch.Patch) (string, error) {
	path, err := w.Env.Path(p.Scope)
	if err != nil {
		return "", err
	}
	return path, w.Update(path, p.Apply)
}

// Update locks path, reads it (a missing or empty file is an empty object), calls fn
// with the decoded document and writes the result. Nothing is written when fn fails or
// returns a document equal to the input.
func (w Writer) Update(path string, fn func(map[string]any) (map[string]any, error)) error {
	if err := w.Env.CheckWritable(path); err != nil {
		return err
	}
	target, err := resolve(path)
	if err != nil {
		return err
	}
	if err := w.Env.CheckWritable(target); err != nil {
		return err
	}
	unlock, err := w.lock(target)
	if err != nil {
		return err
	}
	defer unlock()

	raw, mode, err := read(target)
	if err != nil {
		return err
	}
	doc, order, err := Decode(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	next, err := fn(doc)
	if err != nil {
		return err
	}
	if patch.Equal(doc, next) && len(raw) > 0 {
		return nil
	}
	out, err := Encode(next, order)
	if err != nil {
		return err
	}
	return writeAtomic(target, out, mode)
}

// resolve follows symlinks so the link itself survives the rename. A missing file
// resolves to itself; a dangling link resolves to its target.
func resolve(path string) (string, error) {
	for i := 0; i < 32; i++ {
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

func read(path string) ([]byte, fs.FileMode, error) {
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
	dir := w.LockDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "mantle-locks")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(target))
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

// Decode parses a settings file into a document and its key order. Empty input is an
// empty document. Numbers are kept as json.Number so they are written back verbatim.
func Decode(raw []byte) (map[string]any, *Order, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, &Order{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, order, err := decodeValue(dec)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := dec.Token(); err == nil {
		return nil, nil, fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, fmt.Errorf("%w: top level is not an object", ErrInvalid)
	}
	return m, order, nil
}
