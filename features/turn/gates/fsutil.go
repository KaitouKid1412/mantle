package gates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// writeJSONAtomic writes v to path through a temp file in the same directory, fsync and
// rename, so readers never see a partial file.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// updateJSON runs a locked read-modify-write of a JSON file owned by mantle. A missing
// file decodes as the zero value; a corrupt one is replaced (it is only mantle's cache
// of answers, and the user is asked again).
func updateJSON[T any](path string, fn func(*T) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	var v T
	if err := readJSON(path, &v); err != nil && !errors.Is(err, fs.ErrNotExist) {
		var zero T
		v = zero
	}
	if err := fn(&v); err != nil {
		return err
	}
	return writeJSONAtomic(path, &v)
}

// readJSON decodes path into v. Missing files return an error wrapping fs.ErrNotExist.
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
