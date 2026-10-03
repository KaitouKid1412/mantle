package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MemoryKind says what a memory file is.
type MemoryKind string

const (
	MemoryClaudeMD MemoryKind = "CLAUDE.md"
	MemoryLocal    MemoryKind = "CLAUDE.local.md"
	MemoryRule     MemoryKind = "rule"      // .claude/rules/**/*.md
	MemoryAgentsMD MemoryKind = "AGENTS.md" // read as instructions when present
	MemoryAuto     MemoryKind = "auto"      // auto-memory directory
	MemoryImport   MemoryKind = "import"    // pulled in with @path from another file
)

// MaxImportDepth is how many @import hops are followed.
const MaxImportDepth = 5

// MemoryFile is one instructions file.
type MemoryFile struct {
	Path         string
	Scope        Scope
	Kind         MemoryKind
	Exists       bool
	ImportedFrom string   // the file whose @path pulled this one in
	Depth        int      // 0 for files loaded directly
	Paths        []string // a rule's "paths" frontmatter: it applies only to matching files
}

// MemoryInfo is everything the /memory panel lists.
type MemoryInfo struct {
	Files       []MemoryFile
	AutoEnabled bool
	AutoDir     string
	AutoDirFrom Scope // the settings scope that set autoMemoryDirectory, if any
}

// AutoMemoryEntry is the index file inside the auto-memory directory.
const AutoMemoryEntry = "MEMORY.md"

// Memory lists memory files in load order (lowest priority first): managed,
// user, project files from the outermost parent down to the cwd, then auto
// memory. The user and cwd CLAUDE.md are listed even when missing, so a panel
// can offer to create them.
func Memory(r Roots, settings []SettingsFile) MemoryInfo {
	var info MemoryInfo
	seen := map[string]bool{}
	add := func(f MemoryFile) {
		key := filepath.Clean(f.Path)
		if seen[key] {
			return
		}
		seen[key] = true
		if !f.Exists {
			f.Exists = isFile(f.Path)
		}
		info.Files = append(info.Files, f)
		if f.Exists {
			info.Files = append(info.Files, imports(r, f.Path, 1, seen)...)
		}
	}
	addIfExists := func(f MemoryFile) {
		if isFile(f.Path) {
			add(f)
		}
	}
	addRules := func(dir string, scope Scope) {
		for _, p := range mdFilesRecursive(dir) {
			fm, _ := readFrontmatter(p)
			add(MemoryFile{Path: p, Scope: scope, Kind: MemoryRule, Paths: fm.List("paths")})
		}
	}

	if r.ManagedDir != "" {
		addIfExists(MemoryFile{Path: filepath.Join(r.ManagedDir, "CLAUDE.md"), Scope: ScopeManaged, Kind: MemoryClaudeMD})
		addRules(filepath.Join(r.ManagedDir, ".claude", "rules"), ScopeManaged)
	}
	if r.ConfigDir != "" {
		add(MemoryFile{Path: filepath.Join(r.ConfigDir, "CLAUDE.md"), Scope: ScopeUser, Kind: MemoryClaudeMD})
		addRules(filepath.Join(r.ConfigDir, "rules"), ScopeUser)
	}
	dirs := ancestors(r.CWD)
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		main := MemoryFile{Path: filepath.Join(d, "CLAUDE.md"), Scope: ScopeProject, Kind: MemoryClaudeMD}
		if i == 0 {
			add(main)
		} else {
			addIfExists(main)
		}
		if dc := filepath.Join(d, ".claude"); !r.isUserConfig(dc) {
			addIfExists(MemoryFile{Path: filepath.Join(dc, "CLAUDE.md"), Scope: ScopeProject, Kind: MemoryClaudeMD})
			addRules(filepath.Join(dc, "rules"), ScopeProject)
		}
		addIfExists(MemoryFile{Path: filepath.Join(d, "AGENTS.md"), Scope: ScopeProject, Kind: MemoryAgentsMD})
		addIfExists(MemoryFile{Path: filepath.Join(d, "CLAUDE.local.md"), Scope: ScopeLocal, Kind: MemoryLocal})
	}

	info.AutoEnabled, info.AutoDir, info.AutoDirFrom = AutoMemory(r, settings)
	if info.AutoDir != "" {
		add(MemoryFile{Path: filepath.Join(info.AutoDir, AutoMemoryEntry), Scope: ScopeAuto, Kind: MemoryAuto})
		for _, p := range mdFiles(info.AutoDir) {
			add(MemoryFile{Path: p, Scope: ScopeAuto, Kind: MemoryAuto})
		}
	}
	return info
}

// AutoMemory resolves whether auto memory is on and where it lives. The
// directory can be moved with autoMemoryDirectory in managed, local or user
// settings; a checked-in project setting is ignored, as Claude Code does.
func AutoMemory(r Roots, settings []SettingsFile) (enabled bool, dir string, from Scope) {
	enabled = effectiveBool(settings, "autoMemoryEnabled", true)
	if v := strings.ToLower(r.getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY")); v == "1" || v == "true" {
		enabled = false
	}
	for _, scope := range []Scope{ScopeManaged, ScopeLocal, ScopeUser} {
		for i := len(settings) - 1; i >= 0; i-- {
			f := settings[i]
			var d string
			if f.Scope == scope && f.Get("autoMemoryDirectory", &d) && d != "" {
				return enabled, expandHome(d, r.Home), scope
			}
		}
	}
	if r.ConfigDir == "" || r.CWD == "" {
		return enabled, "", ""
	}
	return enabled, filepath.Join(r.ConfigDir, "projects", ProjectSlug(CanonicalRoot(r.CWD)), "memory"), ""
}

var reImport = regexp.MustCompile(`(?:^|\s)@((?:~/|/|\.{1,2}/)?[A-Za-z0-9_.\-/~]+)`)

// imports follows @path references in a memory file, outside code blocks and
// inline code, up to MaxImportDepth hops.
func imports(r Roots, from string, depth int, seen map[string]bool) []MemoryFile {
	if depth > MaxImportDepth {
		return nil
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return nil
	}
	var out []MemoryFile
	inFence := false
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range reImport.FindAllStringSubmatch(stripInlineCode(line), -1) {
			p := strings.TrimRight(m[1], ".")
			p = expandHome(p, r.Home)
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(from), p)
			}
			p = filepath.Clean(p)
			if seen[p] || !isFile(p) {
				continue
			}
			seen[p] = true
			out = append(out, MemoryFile{Path: p, Scope: scopeOf(r, p), Kind: MemoryImport, Exists: true,
				ImportedFrom: from, Depth: depth})
			out = append(out, imports(r, p, depth+1, seen)...)
		}
	}
	return out
}

func stripInlineCode(line string) string {
	var b strings.Builder
	in := false
	for _, c := range line {
		if c == '`' {
			in = !in
			continue
		}
		if !in {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// scopeOf guesses the scope of an imported file from where it lives.
func scopeOf(r Roots, p string) Scope {
	switch {
	case r.ConfigDir != "" && within(p, r.ConfigDir):
		return ScopeUser
	case r.ManagedDir != "" && within(p, r.ManagedDir):
		return ScopeManaged
	case r.CWD != "" && within(p, CanonicalRoot(r.CWD)):
		return ScopeProject
	case r.Home != "" && within(p, r.Home):
		return ScopeUser
	}
	return ScopeProject
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// AutoMemorySetting returns the file and scope where autoMemoryEnabled is set
// with the highest precedence, for the toggle in /memory.
func AutoMemorySetting(settings []SettingsFile) (json.RawMessage, Scope) {
	return Effective(settings, "autoMemoryEnabled")
}
