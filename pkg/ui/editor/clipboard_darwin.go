package editor

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// ReadClipboardImage reads a PNG from the macOS clipboard with osascript
// (no cgo), writing it through a temporary file.
func ReadClipboardImage(ctx context.Context) (*Image, error) {
	f, err := os.CreateTemp("", "mantle-clipboard-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
	cmd := exec.CommandContext(ctx, "osascript",
		"-e", "set png_data to (the clipboard as «class PNGf»)",
		"-e", "set fp to open for access POSIX file "+quoted+" with write permission",
		"-e", "write png_data to fp",
		"-e", "close access fp",
	)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrNoClipboardImage
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrNoClipboardImage
	}
	return PrepareImage(data)
}
