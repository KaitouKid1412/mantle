package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// DisabledEntry records a feature turned off after a panic.
type DisabledEntry struct {
	Feature string    `json:"feature"`
	Reason  string    `json:"reason"`
	Time    time.Time `json:"time"`
}

type disabledFile struct {
	Disabled []DisabledEntry `json:"disabled"`
}

// ReadDisabled reads ~/.mantle/state/disabled.json.
func ReadDisabled(p Paths) []DisabledEntry {
	raw, err := os.ReadFile(p.DisabledFile())
	if err != nil {
		return nil
	}
	var f disabledFile
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	return f.Disabled
}

// RecordDisabled adds (or refreshes) a feature in disabled.json.
func RecordDisabled(p Paths, feature, reason string, now time.Time) error {
	return updateDisabled(p, func(es []DisabledEntry) []DisabledEntry {
		out := es[:0]
		for _, e := range es {
			if e.Feature != feature {
				out = append(out, e)
			}
		}
		return append(out, DisabledEntry{Feature: feature, Reason: reason, Time: now})
	})
}

// ClearDisabled removes a feature from disabled.json ("" clears all).
func ClearDisabled(p Paths, feature string) error {
	return updateDisabled(p, func(es []DisabledEntry) []DisabledEntry {
		if feature == "" {
			return nil
		}
		out := es[:0]
		for _, e := range es {
			if e.Feature != feature {
				out = append(out, e)
			}
		}
		return out
	})
}

func updateDisabled(p Paths, fn func([]DisabledEntry) []DisabledEntry) error {
	path := p.DisabledFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var f disabledFile
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &f)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f.Disabled = fn(f.Disabled)
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(raw, '\n'), 0o600)
}

// DisabledIDs returns the feature IDs in entries.
func DisabledIDs(es []DisabledEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Feature
	}
	return out
}
