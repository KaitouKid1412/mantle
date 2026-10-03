package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func (r *Root) store(featureID string) ext.KV {
	if kv, ok := r.stores[featureID]; ok {
		return kv
	}
	var kv ext.KV
	if r.opts.StateDir == "" {
		kv = &memKV{m: map[string]json.RawMessage{}}
	} else {
		safe := strings.Map(func(c rune) rune {
			if c == '/' || c == '\\' || c == 0 {
				return '_'
			}
			return c
		}, featureID)
		kv = &fileKV{path: filepath.Join(r.opts.StateDir, safe+".json")}
	}
	r.stores[featureID] = kv
	return kv
}

type memKV struct{ m map[string]json.RawMessage }

func (k *memKV) Get(key string, out any) (bool, error) {
	raw, ok := k.m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (k *memKV) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	k.m[key] = raw
	return nil
}

func (k *memKV) Delete(key string) error { delete(k.m, key); return nil }
func (k *memKV) Keys() []string          { return sortedKeys(k.m) }

// fileKV is a JSON object file, read on every Get and rewritten atomically on Set. It
// is small by design (feature state, not data).
type fileKV struct {
	mu   sync.Mutex
	path string
}

func (k *fileKV) load() (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	data, err := os.ReadFile(k.path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return m, nil
	}
	return m, json.Unmarshal(data, &m)
}

func (k *fileKV) save(m map[string]json.RawMessage) error {
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(k.path), ".kv-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), k.path)
}

func (k *fileKV) Get(key string, out any) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	m, err := k.load()
	if err != nil {
		return false, err
	}
	raw, ok := m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (k *fileKV) Set(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	m, err := k.load()
	if err != nil {
		return err
	}
	m[key] = raw
	return k.save(m)
}

func (k *fileKV) Delete(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	m, err := k.load()
	if err != nil {
		return err
	}
	delete(m, key)
	return k.save(m)
}

func (k *fileKV) Keys() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	m, _ := k.load()
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
