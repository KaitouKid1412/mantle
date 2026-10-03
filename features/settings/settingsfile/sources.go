package settingsfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// ManagedPath is the managed (policy) settings file for an OS.
func ManagedPath(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	}
	return "/etc/claude-code/managed-settings.json"
}

// Source is one settings file as found on disk.
type Source struct {
	Scope  patch.Scope
	Path   string
	Exists bool
	Doc    map[string]any // nil when missing or invalid
	Err    error          // read or parse problem
}

// ReadScope reads one scope's file. A missing file is not an error.
func ReadScope(env patch.Env, scope patch.Scope) Source {
	var path string
	switch scope {
	case patch.Policy:
		path = ManagedPath(runtime.GOOS)
	case patch.User:
		path = filepath.Join(env.ClaudeDir(), "settings.json")
	case patch.Project, patch.Local:
		if env.ProjectRoot == "" {
			return Source{Scope: scope}
		}
		name := "settings.json"
		if scope == patch.Local {
			name = "settings.local.json"
		}
		path = filepath.Join(env.ProjectRoot, ".claude", name)
	default:
		return Source{Scope: scope}
	}
	return readSource(scope, path)
}

func readSource(scope patch.Scope, path string) Source {
	s := Source{Scope: scope, Path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s
	}
	s.Exists = true
	if err != nil {
		s.Err = err
		return s
	}
	s.Doc, _, s.Err = Decode(b)
	return s
}

// ReadAll reads the user, project, local and managed files, lowest precedence first.
// Flag settings come from the command line and are not files.
func ReadAll(env patch.Env) []Source {
	var out []Source
	for _, sc := range []patch.Scope{patch.User, patch.Project, patch.Local, patch.Policy} {
		if s := ReadScope(env, sc); s.Path != "" {
			out = append(out, s)
		}
	}
	return out
}

// Docs maps each readable scope to its document.
func Docs(srcs []Source) map[patch.Scope]map[string]any {
	out := map[patch.Scope]map[string]any{}
	for _, s := range srcs {
		if s.Doc != nil {
			out[s.Scope] = s.Doc
		}
	}
	return out
}
