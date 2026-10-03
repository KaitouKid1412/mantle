package termsetup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner runs an external command (the macOS `defaults` tool). stdin may be nil.
type Runner interface {
	Run(name string, args []string, stdin []byte) ([]byte, error)
}

// ExecRunner runs commands with os/exec.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Applier loads terminal configs and writes confirmed proposals.
type Applier struct {
	Runner    Runner           // nil means ExecRunner
	Now       func() time.Time // nil means time.Now
	BackupDir string           // where preferences-domain backups go; file backups sit next to the file
}

// ErrStale means the config changed between planning and applying.
var ErrStale = errors.New("the terminal config changed since the proposal was made; run /terminal-setup again")

// ErrForbidden is returned for targets mantle must never write.
var ErrForbidden = errors.New("refusing to write Claude Code's global config (.claude.json)")

func (a Applier) runner() Runner {
	if a.Runner != nil {
		return a.Runner
	}
	return ExecRunner{}
}

func (a Applier) stamp() string {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	return now().Format("20060102-150405")
}

// Load reads the current config for the detected terminal and builds the planner input.
func (a Applier) Load(d Detected, e Env, kittyKeyboard bool) (Input, error) {
	loc := Locate(d.Terminal, e, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
	in := Input{Terminal: d.Terminal, Path: loc.Path, KittyKeyboard: kittyKeyboard, InTmux: d.InTmux, LegacyPath: loc.LegacyPath}
	switch loc.Kind {
	case TargetFile:
		b, err := os.ReadFile(loc.Path)
		switch {
		case err == nil:
			in.Exists, in.Content = true, b
		case !errors.Is(err, fs.ErrNotExist):
			return in, err
		}
	case TargetDefaults:
		if out, err := a.runner().Run("defaults", []string{"export", loc.Domain, "-"}, nil); err == nil {
			in.Exists, in.Content = true, out
		}
	}
	return in, nil
}

// Apply writes an Edit proposal after backing up the current config. It returns the
// backup's path ("" when the file didn't exist before).
func (a Applier) Apply(p Proposal) (backup string, err error) {
	if p.Status != Edit {
		return "", fmt.Errorf("nothing to apply (%s)", p.Status)
	}
	if filepath.Base(p.Path) == ".claude.json" {
		return "", ErrForbidden
	}
	switch p.Kind {
	case TargetFile:
		return a.applyFile(p)
	case TargetDefaults:
		return a.applyDefaults(p)
	}
	return "", fmt.Errorf("unknown target kind %d", int(p.Kind))
}

func (a Applier) applyFile(p Proposal) (string, error) {
	mode := fs.FileMode(0o644)
	cur, err := os.ReadFile(p.Path)
	existed := err == nil
	switch {
	case existed:
		if st, err := os.Stat(p.Path); err == nil {
			mode = st.Mode().Perm()
		}
	case errors.Is(err, fs.ErrNotExist):
		cur = nil
	default:
		return "", err
	}
	if !bytes.Equal(cur, p.Old) {
		return "", ErrStale
	}
	backup := ""
	if existed {
		if backup, err = writeBackup(p.Path+".mantle-backup-"+a.stamp(), cur, mode); err != nil {
			return "", err
		}
	} else if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return "", err
	}
	if err := writeAtomic(p.Path, p.New, mode); err != nil {
		return backup, err
	}
	return backup, nil
}

func (a Applier) applyDefaults(p Proposal) (string, error) {
	if p.Domain == "" {
		return "", errors.New("missing preferences domain")
	}
	if a.BackupDir == "" {
		return "", errors.New("no backup directory configured")
	}
	r := a.runner()
	cur, err := r.Run("defaults", []string{"export", p.Domain, "-"}, nil)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(cur, p.Old) {
		return "", ErrStale
	}
	if err := os.MkdirAll(a.BackupDir, 0o700); err != nil {
		return "", err
	}
	backup, err := writeBackup(filepath.Join(a.BackupDir, p.Domain+"-"+a.stamp()+".plist"), cur, 0o600)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(a.BackupDir, p.Domain+"-new-*.plist")
	if err != nil {
		return backup, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(p.New); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Close(); err != nil {
		return backup, err
	}
	if _, err := r.Run("defaults", []string{"import", p.Domain, tmp.Name()}, nil); err != nil {
		return backup, err
	}
	return backup, nil
}

// writeBackup creates path (or path-N if taken) without overwriting anything.
func writeBackup(path string, data []byte, mode fs.FileMode) (string, error) {
	for i := 0; i < 100; i++ {
		p := path
		if i > 0 {
			p = fmt.Sprintf("%s-%d", path, i)
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", err
		}
		return p, f.Close()
	}
	return "", fmt.Errorf("too many backups of %s", path)
}

// writeAtomic replaces path with data via a temp file and rename.
func writeAtomic(path string, data []byte, mode fs.FileMode) error {
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
