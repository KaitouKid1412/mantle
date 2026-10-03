package termsetup

import "path/filepath"

// Env is what config lookup needs from the environment.
type Env struct {
	Home          string // $HOME
	XDGConfigHome string // $XDG_CONFIG_HOME, may be empty
	GOOS          string // runtime.GOOS
}

func (e Env) configHome() string {
	if e.XDGConfigHome != "" {
		return e.XDGConfigHome
	}
	return filepath.Join(e.Home, ".config")
}

// Location says where a terminal keeps the config an installer edits.
type Location struct {
	Kind       TargetKind
	Path       string // file path, or the preferences file shown to the user
	Domain     string // defaults domain for TargetDefaults
	LegacyPath string // an old-format config found instead of Path (Alacritty YAML)
}

// Locate returns the config location for t. exists reports whether a file exists; it
// picks between candidate paths (nil means "nothing exists").
func Locate(t Terminal, e Env, exists func(string) bool) Location {
	if exists == nil {
		exists = func(string) bool { return false }
	}
	switch t {
	case ITerm2:
		return Location{Kind: TargetDefaults, Domain: iTermDomain,
			Path: filepath.Join(e.Home, "Library", "Preferences", iTermDomain+".plist")}
	case AppleTerminal:
		return Location{Kind: TargetDefaults, Domain: appleTerminalDomain,
			Path: filepath.Join(e.Home, "Library", "Preferences", appleTerminalDomain+".plist")}
	case VSCode, VSCodeInsiders, VSCodium, Cursor, Windsurf:
		base := e.configHome()
		if e.GOOS == "darwin" {
			base = filepath.Join(e.Home, "Library", "Application Support")
		}
		return Location{Kind: TargetFile, Path: filepath.Join(base, vscodeAppDir(t), "User", "keybindings.json")}
	case Alacritty:
		var candidates []string
		if e.XDGConfigHome != "" {
			candidates = append(candidates, filepath.Join(e.XDGConfigHome, "alacritty", "alacritty"))
		}
		candidates = append(candidates,
			filepath.Join(e.Home, ".config", "alacritty", "alacritty"),
			filepath.Join(e.Home, ".alacritty"))
		loc := Location{Kind: TargetFile, Path: candidates[0] + ".toml"}
		for _, c := range candidates {
			if exists(c + ".toml") {
				loc.Path = c + ".toml"
				return loc
			}
		}
		for _, c := range candidates {
			if exists(c + ".yml") {
				loc.LegacyPath = c + ".yml"
				break
			}
		}
		return loc
	}
	return Location{Kind: TargetNone}
}

func vscodeAppDir(t Terminal) string {
	switch t {
	case VSCodeInsiders:
		return "Code - Insiders"
	case VSCodium:
		return "VSCodium"
	case Cursor:
		return "Cursor"
	case Windsurf:
		return "Windsurf"
	}
	return "Code"
}
