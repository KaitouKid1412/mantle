package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"

	"github.com/KaitouKid1412/mantle/internal/keymap"
)

// KeybindingsMsg carries reloaded keybinding files (~/.claude and ~/.mantle).
type KeybindingsMsg struct{ Sources []keymap.Source }

// ThemesMsg carries reloaded custom themes from ~/.claude/themes.
type ThemesMsg struct {
	Themes map[string]CustomTheme
	Errs   []error
}

// LoadKeymapSources reads ~/.claude/keybindings.json (Claude Code actions) and
// ~/.mantle/keybindings.json (mantle:* actions). Parse errors become issues on an empty
// source so the rest of the keymap still loads.
func LoadKeymapSources(p Paths) []keymap.Source {
	var out []keymap.Source
	for _, f := range []struct {
		path   string
		mantle bool
	}{{p.ClaudeKeybindings(), false}, {p.MantleKeybindings(), true}} {
		src, err := keymap.LoadFile(f.path, f.mantle)
		if err != nil {
			src = keymap.Source{Name: f.path, Mantle: f.mantle, User: true, Err: err}
		}
		out = append(out, src)
	}
	return out
}

// Watcher watches settings, keybindings and theme files and sends reload messages.
// Changes are debounced; every message carries fully re-read data built off the UI
// goroutine.
type Watcher struct {
	p     Paths
	flag  string
	send  func(tea.Msg)
	w     *fsnotify.Watcher
	done  chan struct{}
	once  sync.Once
	delay time.Duration
}

// Watch starts watching. send is typically (*tea.Program).Send.
func Watch(p Paths, flagSettings string, send func(tea.Msg)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{p: p, flag: flagSettings, send: send, w: fw, done: make(chan struct{}), delay: 150 * time.Millisecond}
	for _, dir := range w.dirs() {
		_ = fw.Add(dir) // missing directories are fine; they are re-tried on parent events
	}
	go w.loop()
	return w, nil
}

// dirs are the directories holding watched files.
func (w *Watcher) dirs() []string {
	ds := []string{w.p.ClaudeDir, w.p.ThemesDir(), w.p.MantleDir, filepath.Dir(w.p.ClaudeJSON)}
	if w.p.Project != "" {
		ds = append(ds, filepath.Join(w.p.Project, ".claude"))
	}
	if w.p.Managed != "" {
		ds = append(ds, filepath.Dir(w.p.Managed), filepath.Join(filepath.Dir(w.p.Managed), "managed-settings.d"))
	}
	if w.flag != "" && !strings.HasPrefix(strings.TrimSpace(w.flag), "{") {
		ds = append(ds, filepath.Dir(w.flag))
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range ds {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

type kind int

const (
	kindNone kind = iota
	kindSettings
	kindKeys
	kindThemes
)

func (w *Watcher) classify(path string) kind {
	base := filepath.Base(path)
	dir := filepath.Dir(path)
	switch {
	case dir == w.p.ThemesDir() && strings.HasSuffix(base, ".json"):
		return kindThemes
	case path == w.p.ThemesDir():
		return kindThemes
	case base == "keybindings.json" && (dir == w.p.ClaudeDir || dir == w.p.MantleDir):
		return kindKeys
	case path == w.p.ClaudeJSON:
		return kindSettings
	case strings.HasPrefix(base, "settings") && strings.HasSuffix(base, ".json"):
		return kindSettings
	case strings.HasPrefix(base, "managed-settings") || filepath.Base(dir) == "managed-settings.d":
		return kindSettings
	case w.flag != "" && path == w.flag:
		return kindSettings
	}
	return kindNone
}

func (w *Watcher) loop() {
	pending := map[kind]bool{}
	var timer <-chan time.Time
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.w.Events:
			if !ok {
				return
			}
			// A new directory (e.g. ~/.claude/themes created later): watch it.
			if ev.Op&fsnotify.Create != 0 {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					for _, d := range w.dirs() {
						if d == ev.Name {
							_ = w.w.Add(d)
						}
					}
				}
			}
			if k := w.classify(ev.Name); k != kindNone {
				pending[k] = true
				timer = time.After(w.delay)
			}
		case <-w.w.Errors:
		case <-timer:
			timer = nil
			if pending[kindSettings] {
				w.send(ReloadMsg{Snap: Load(w.p, w.flag)})
			}
			if pending[kindKeys] {
				w.send(KeybindingsMsg{Sources: LoadKeymapSources(w.p)})
			}
			if pending[kindThemes] {
				ts, errs := LoadCustomThemes(w.p.ThemesDir())
				w.send(ThemesMsg{Themes: ts, Errs: errs})
			}
			pending = map[kind]bool{}
		}
	}
}

// Close stops watching.
func (w *Watcher) Close() error {
	w.once.Do(func() { close(w.done) })
	return w.w.Close()
}
