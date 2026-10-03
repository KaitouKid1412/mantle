package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// maxThemeFile is the largest custom theme file read (Claude Code's limit too).
const maxThemeFile = 256 << 10

// themeFile is the custom theme format: ~/.claude/themes/<slug>.json, selected with the
// theme setting "custom:<slug>". base is a built-in theme name (default "dark");
// overrides maps tokens the base defines to colours ("#rrggbb", "rgb(r,g,b)", ANSI
// names). Unknown tokens and invalid colours are ignored.
type themeFile struct {
	Name      string            `json:"name"`
	Base      string            `json:"base"`
	Overrides map[string]string `json:"overrides"`
}

// CustomTheme is a loaded custom theme with its display name.
type CustomTheme struct {
	Slug, Name string
	Theme      theme.Theme
}

// LoadCustomThemes reads every *.json in dir, keyed by slug (the file name without
// .json). Invalid files are skipped and reported in errs.
func LoadCustomThemes(dir string) (themes map[string]CustomTheme, errs []error) {
	themes = map[string]CustomTheme{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return themes, nil
	}
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())
		if fi, err := e.Info(); err == nil && fi.Size() > maxThemeFile {
			errs = append(errs, &os.PathError{Op: "load theme", Path: path, Err: os.ErrInvalid})
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ct, err := ParseCustomTheme(slug, raw)
		if err != nil {
			errs = append(errs, &os.PathError{Op: "load theme", Path: path, Err: err})
			continue
		}
		themes[slug] = ct
	}
	return themes, errs
}

// ParseCustomTheme parses one custom theme document.
func ParseCustomTheme(slug string, raw []byte) (CustomTheme, error) {
	var f themeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return CustomTheme{}, err
	}
	base, ok := theme.Builtin(f.Base)
	if !ok {
		base, _ = theme.Builtin(theme.NameDark)
	}
	t := base.Clone()
	t.Name = theme.CustomPrefix + slug
	t.Base = base.Name
	for tok, val := range f.Overrides {
		if !base.Has(theme.Token(tok)) {
			continue
		}
		if c, ok := theme.ParseColor(val); ok {
			t.Colors[theme.Token(tok)] = c
		}
	}
	name := f.Name
	if name == "" {
		name = slug
	}
	return CustomTheme{Slug: slug, Name: name, Theme: t}, nil
}

// ThemeMap returns the themes by slug, as app.Options.CustomThemes wants them.
func ThemeMap(cts map[string]CustomTheme) map[string]theme.Theme {
	out := make(map[string]theme.Theme, len(cts))
	for slug, ct := range cts {
		out[slug] = ct.Theme
	}
	return out
}
