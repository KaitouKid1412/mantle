package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PluginRoot is an enabled plugin's directory.
type PluginRoot struct {
	ID      string // name@marketplace; empty for session plugins (--plugin-dir)
	Name    string // used as the namespace of its skills and commands
	Path    string
	Scope   Scope // the installation scope (user, project, local, managed)
	Version string
}

// pluginManifest is the subset of .claude-plugin/plugin.json discovery reads.
// Component fields may be a path or a list of paths relative to the plugin.
type pluginManifest struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Commands json.RawMessage `json:"commands"`
	Agents   json.RawMessage `json:"agents"`
	Skills   json.RawMessage `json:"skills"`
	Hooks    json.RawMessage `json:"hooks"`
}

func readManifest(root string) pluginManifest {
	var m pluginManifest
	if b, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// manifestPaths resolves a component field (string or list) against root,
// refusing paths that escape the plugin.
func manifestPaths(root string, raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	var one string
	if json.Unmarshal(raw, &one) == nil {
		list = []string{one}
	} else if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []string
	for _, p := range list {
		if p == "" || filepath.IsAbs(p) {
			continue
		}
		full := filepath.Join(root, p)
		if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		out = append(out, full)
	}
	return out
}

// installedPlugins is ~/.claude/plugins/installed_plugins.json. Version 2 maps
// an ID to a list of installations; version 1 mapped it to one object.
type installedPlugins struct {
	Plugins map[string]json.RawMessage `json:"plugins"`
}

type installation struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	ProjectPath string `json:"projectPath"`
}

// Plugins returns the enabled plugins: installed ones whose enabledPlugins
// setting is true (higher scopes win), then Roots.Plugins.
func Plugins(r Roots, settings []SettingsFile) []PluginRoot {
	var out []PluginRoot
	seen := map[string]bool{}
	add := func(p PluginRoot) {
		if p.Path == "" || seen[filepath.Clean(p.Path)] || !isDir(p.Path) {
			return
		}
		seen[filepath.Clean(p.Path)] = true
		m := readManifest(p.Path)
		if p.Name == "" {
			p.Name = m.Name
		}
		if p.Name == "" {
			p.Name, _, _ = strings.Cut(p.ID, "@")
		}
		if p.Name == "" {
			p.Name = filepath.Base(p.Path)
		}
		if p.Version == "" {
			p.Version = m.Version
		}
		out = append(out, p)
	}

	if r.ConfigDir != "" {
		enabled := mergedBoolMap(settings, "enabledPlugins")
		var inst installedPlugins
		if b, err := os.ReadFile(filepath.Join(r.ConfigDir, "plugins", "installed_plugins.json")); err == nil {
			_ = json.Unmarshal(b, &inst)
		}
		for _, id := range sortedKeys(inst.Plugins) {
			if !enabled[id] {
				continue
			}
			var list []installation
			if json.Unmarshal(inst.Plugins[id], &list) != nil {
				var one installation
				if json.Unmarshal(inst.Plugins[id], &one) != nil {
					continue
				}
				list = []installation{one}
			}
			if in, ok := pickInstallation(list, r.CWD); ok {
				add(PluginRoot{ID: id, Path: in.InstallPath, Scope: Scope(in.Scope), Version: in.Version})
			}
		}
	}
	for _, p := range r.Plugins {
		add(p)
	}
	return out
}

// pickInstallation prefers an installation scoped to this project, then a
// user or managed one.
func pickInstallation(list []installation, cwd string) (installation, bool) {
	inProject := func(in installation) bool {
		if in.ProjectPath == "" {
			return true
		}
		for _, d := range ancestors(cwd) {
			if sameDir(d, in.ProjectPath) {
				return true
			}
		}
		return false
	}
	for _, scope := range []string{"local", "project", "user", "managed", ""} {
		for _, in := range list {
			if in.Scope != scope || in.InstallPath == "" {
				continue
			}
			if (scope == "local" || scope == "project") && !inProject(in) {
				continue
			}
			return in, true
		}
	}
	return installation{}, false
}
