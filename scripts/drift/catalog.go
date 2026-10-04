package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Catalog is what `mantle-ui catalog --json` says mantle registers: native slash
// commands and renderer keys. Read through the catalog, not imports, so the drift tool
// doesn't depend on every feature compiling.
type Catalog struct {
	Commands  map[string]bool // command names ("resume")
	Renderers map[string]bool // content keys ("tool.Bash", "tool.mcp.*")
}

type catalogEntry struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Removed bool   `json:"removed,omitempty"`
}

func parseCatalog(data []byte) (*Catalog, error) {
	var entries []catalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	c := &Catalog{Commands: map[string]bool{}, Renderers: map[string]bool{}}
	for _, e := range entries {
		if e.Removed {
			continue
		}
		switch e.Kind {
		case "command":
			c.Commands[strings.TrimPrefix(e.ID, "cmd.")] = true
		case "renderer":
			c.Renderers[strings.TrimPrefix(e.ID, "render.")] = true
		}
	}
	return c, nil
}

// loadCatalog reads a catalog file, or with "auto" runs `go run ./cmd/mantle-ui catalog
// --json`. "" or "none" skips it.
func loadCatalog(ctx context.Context, src string) (*Catalog, error) {
	switch src {
	case "", "none":
		return nil, nil
	case "auto":
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "run", "./cmd/mantle-ui", "catalog", "--json")
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("mantle-ui catalog --json: %v %s", err, firstLine(errb.String()))
		}
		return parseCatalog(out.Bytes())
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	return parseCatalog(b)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

// HasToolRenderer reports whether a tool has its own renderer: a key more specific
// than the generic tool.* and default ones.
func (c *Catalog) HasToolRenderer(tool string) bool {
	for _, k := range ext.ToolKey(tool).Candidates() {
		if k == "tool.*" || k == ext.KeyDefault {
			continue
		}
		if c.Renderers[string(k)] {
			return true
		}
	}
	return false
}
