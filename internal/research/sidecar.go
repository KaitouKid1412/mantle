package research

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/config"
)

// SidecarVersion is the sidecar format version.
const SidecarVersion = 1

// Sidecar is research mode's UI state for one session, stored in
// <MantleDir>/research/<sid>.json. The tree itself always comes from the session JSONL.
type Sidecar struct {
	V       int    `json:"v"`
	Active  bool   `json:"active"`
	Viewing string `json:"viewing,omitempty"`
	// LastChild maps a node ID to the child last visited from it.
	LastChild map[string]string `json:"lastChild,omitempty"`
}

// ErrBadSessionID is returned for a session id that cannot name a sidecar file.
var ErrBadSessionID = errors.New("research: bad session id")

// SidecarPath is the sidecar file of session sid, or "" for an unusable id.
func SidecarPath(p config.Paths, sid string) string {
	if !validSID(sid) || p.MantleDir == "" {
		return ""
	}
	return filepath.Join(p.MantleDir, "research", sid+".json")
}

func validSID(sid string) bool {
	return sid != "" && sid != "." && sid != ".." && !strings.ContainsAny(sid, `/\`+"\x00")
}

// LoadSidecar reads session sid's sidecar. A missing, unreadable, corrupt or
// unknown-version file yields ok=false and a zero Sidecar.
func LoadSidecar(p config.Paths, sid string) (s Sidecar, ok bool) {
	path := SidecarPath(p, sid)
	if path == "" {
		return Sidecar{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Sidecar{}, false
	}
	if err := json.Unmarshal(b, &s); err != nil || s.V != SidecarVersion {
		return Sidecar{}, false
	}
	return s, true
}

// SaveSidecar writes session sid's sidecar atomically (temp file and rename).
func SaveSidecar(p config.Paths, sid string, s Sidecar) error {
	path := SidecarPath(p, sid)
	if path == "" {
		return ErrBadSessionID
	}
	s.V = SidecarVersion
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

// CopySidecar copies session from's sidecar to session to (a fork). A missing source
// is not an error.
func CopySidecar(p config.Paths, from, to string) error {
	s, ok := LoadSidecar(p, from)
	if !ok {
		if SidecarPath(p, to) == "" {
			return ErrBadSessionID
		}
		return nil
	}
	return SaveSidecar(p, to, s)
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
