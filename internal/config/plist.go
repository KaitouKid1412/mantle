package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// plutil converts plists; it ships with macOS.
var plutil = "/usr/bin/plutil"

// readPlist reads a managed-preferences plist as a settings document, converting it
// with `plutil -convert json`. A missing file returns fs.ErrNotExist.
func readPlist(path string) (map[string]any, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, plutil, "-convert", "json", "-o", "-", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: plutil: %v %s", path, err, bytes.TrimSpace(stderr.Bytes()))
	}
	doc, _, err := Decode(out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return normalize(doc).(map[string]any), nil
}
