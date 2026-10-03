// Package themes lists the themes /theme offers and runs the picker's preview state
// machine. Rendering a theme is the theme engine's job (plan 01); this package only
// decides which name to preview, apply or restore.
package themes

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KaitouKid1412/mantle/features/settings/action"
	"github.com/KaitouKid1412/mantle/features/settings/patch"
)

// CustomPrefix marks a custom theme in the theme setting ("custom:<name>").
const CustomPrefix = "custom:"

// BuiltIn lists the built-in theme setting values in picker order.
var BuiltIn = []string{"auto", "dark", "light", "dark-daltonized", "light-daltonized", "dark-ansi", "light-ansi"}

var builtInLabels = map[string]string{
	"auto":             "Auto (follow the terminal background)",
	"dark":             "Dark",
	"light":            "Light",
	"dark-daltonized":  "Dark, colour-blind friendly",
	"light-daltonized": "Light, colour-blind friendly",
	"dark-ansi":        "Dark, terminal palette only",
	"light-ansi":       "Light, terminal palette only",
}

// Entry is one theme in the list.
type Entry struct {
	Name   string // the theme setting value
	Label  string
	Custom bool
	Path   string // custom theme file
	Err    string // the custom file could not be read; shown but not selectable
}

// Selectable reports whether the entry can be previewed and applied.
func (e Entry) Selectable() bool { return e.Err == "" }

// Label returns the display label for a theme setting value.
func Label(name string) string {
	if l, ok := builtInLabels[name]; ok {
		return l
	}
	if strings.HasPrefix(name, CustomPrefix) {
		return "Custom: " + strings.TrimPrefix(name, CustomPrefix)
	}
	return name
}

// List returns the built-in themes followed by the custom themes in dir (usually
// ~/.claude/themes), sorted by name. A missing directory yields no custom themes.
func List(dir string) []Entry {
	return ListFS(os.DirFS(dir), dir)
}

// ListFS is List over an fs.FS; base is joined to file names for Entry.Path.
func ListFS(fsys fs.FS, base string) []Entry {
	out := make([]Entry, 0, len(BuiltIn))
	for _, n := range BuiltIn {
		out = append(out, Entry{Name: n, Label: Label(n)})
	}
	files, err := fs.Glob(fsys, "*.json")
	if err != nil {
		return out
	}
	sort.Strings(files)
	for _, f := range files {
		name := CustomPrefix + strings.TrimSuffix(f, ".json")
		e := Entry{Name: name, Label: Label(name), Custom: true, Path: filepath.Join(base, f)}
		if b, err := fs.ReadFile(fsys, f); err != nil {
			e.Err = err.Error()
		} else if !json.Valid(b) {
			e.Err = "not valid JSON"
		}
		out = append(out, e)
	}
	return out
}

// Picker is the /theme picker state. Moving the cursor previews the highlighted
// theme; Cancel restores the theme that was active when the picker opened.
type Picker struct {
	Items    []Entry
	Cursor   int
	Original string // theme setting when opened
	// SyntaxOff is the syntax-highlighting toggle (ctrl+t in the picker).
	SyntaxOff         bool
	originalSyntaxOff bool
	done              bool
}

// NewPicker opens the picker on current; an unknown current value (say a deleted
// custom theme) is added so it can be restored on cancel.
func NewPicker(items []Entry, current string, syntaxOff bool) *Picker {
	p := &Picker{Items: items, Original: current, SyntaxOff: syntaxOff, originalSyntaxOff: syntaxOff}
	if current == "" {
		current = "dark"
		p.Original = current
	}
	p.Cursor = -1
	for i, e := range items {
		if e.Name == current {
			p.Cursor = i
		}
	}
	if p.Cursor < 0 {
		p.Items = append(append([]Entry(nil), items...), Entry{Name: current, Label: Label(current), Custom: strings.HasPrefix(current, CustomPrefix)})
		p.Cursor = len(p.Items) - 1
	}
	return p
}

// Highlighted is the entry under the cursor.
func (p *Picker) Highlighted() Entry { return p.Items[p.Cursor] }

// Move steps the cursor over selectable entries (stopping at the ends) and returns the
// theme to preview. ok is false when the cursor did not move.
func (p *Picker) Move(delta int) (preview string, ok bool) {
	if p.done || delta == 0 {
		return "", false
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	cur := p.Cursor
	for n := delta * step; n > 0; n-- {
		next := cur + step
		for next >= 0 && next < len(p.Items) && !p.Items[next].Selectable() {
			next += step
		}
		if next < 0 || next >= len(p.Items) {
			break
		}
		cur = next
	}
	if cur == p.Cursor {
		return "", false
	}
	p.Cursor = cur
	return p.Items[cur].Name, true
}

// ToggleSyntax flips syntax highlighting for the preview.
func (p *Picker) ToggleSyntax() bool {
	p.SyntaxOff = !p.SyntaxOff
	return p.SyntaxOff
}

// Confirm closes the picker and returns the effects that save the choice: the theme
// (and the syntax toggle, if changed) in user settings. Nothing is written when
// nothing changed.
func (p *Picker) Confirm() (chosen string, effects []action.Effect) {
	p.done = true
	chosen = p.Highlighted().Name
	var ops []patch.Op
	if chosen != p.Original {
		ops = append(ops, patch.Op{Kind: patch.Set, Path: []string{"theme"}, Value: chosen})
	}
	if p.SyntaxOff != p.originalSyntaxOff {
		if p.SyntaxOff {
			ops = append(ops, patch.Op{Kind: patch.Set, Path: []string{"syntaxHighlightingDisabled"}, Value: true})
		} else {
			ops = append(ops, patch.Op{Kind: patch.Delete, Path: []string{"syntaxHighlightingDisabled"}})
		}
	}
	if len(ops) > 0 {
		effects = append(effects, action.PatchEffect(patch.Patch{Scope: patch.User, Ops: ops}))
	}
	return chosen, effects
}

// Cancel closes the picker and returns the theme and syntax setting to restore.
func (p *Picker) Cancel() (restore string, syntaxOff bool) {
	p.done = true
	p.Cursor = p.indexOf(p.Original)
	p.SyntaxOff = p.originalSyntaxOff
	return p.Original, p.originalSyntaxOff
}

func (p *Picker) indexOf(name string) int {
	for i, e := range p.Items {
		if e.Name == name {
			return i
		}
	}
	return p.Cursor
}

// Done reports whether Confirm or Cancel was called.
func (p *Picker) Done() bool { return p.done }

// CustomPath is the file a custom theme name maps to in dir.
func CustomPath(dir, name string) (string, bool) {
	if !strings.HasPrefix(name, CustomPrefix) {
		return "", false
	}
	n := strings.TrimPrefix(name, CustomPrefix)
	if n == "" || strings.ContainsAny(n, `/\`) || n == "." || n == ".." {
		return "", false
	}
	return filepath.Join(dir, n+".json"), true
}
