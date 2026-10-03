package selfmod

import (
	"bufio"
	"bytes"
	"strings"
)

// ProtectedPaths are the paths a /mantle change may never touch. The pipeline's
// first step enforces this with a diff check, because permission rules cannot
// stop a shell command from writing files.
//
//   - the launcher and its command: if they break, /mantle cannot repair them;
//   - this package: the pipeline and this list must not vet themselves;
//   - .claude/ and .mcp.json: settings, hooks and MCP servers there would be
//     picked up by the next builder session.
//
// The go.mod toolchain line is protected separately (ToolchainChanged).
var ProtectedPaths = []string{
	"cmd/mantle/**",
	"internal/launcher/**",
	"internal/selfmod/**",
	".claude/**",
	".mcp.json",
}

// IsProtected reports whether path (slash-separated, relative to the repo
// root) matches one of patterns. "dir/**" matches dir and everything under
// it; other patterns match exactly.
func IsProtected(path string, patterns []string) bool {
	for _, p := range patterns {
		if dir, ok := strings.CutSuffix(p, "/**"); ok {
			if path == dir || strings.HasPrefix(path, dir+"/") {
				return true
			}
		} else if path == p {
			return true
		}
	}
	return false
}

// ToolchainLine returns the toolchain directive of a go.mod file ("" if none).
func ToolchainLine(gomod []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(gomod))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "toolchain"); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\t') {
			if i := strings.Index(rest, "//"); i >= 0 {
				rest = rest[:i]
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}
