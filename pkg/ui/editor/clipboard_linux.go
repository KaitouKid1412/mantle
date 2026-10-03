package editor

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// ReadClipboardImage reads an image from the clipboard with wl-paste
// (Wayland) or xclip (X11).
func ReadClipboardImage(ctx context.Context) (*Image, error) {
	var tries [][]string
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		tries = append(tries, []string{"wl-paste", "--no-newline", "--type", "image/png"})
	}
	if os.Getenv("DISPLAY") != "" {
		tries = append(tries, []string{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"})
	}
	found := false
	for _, args := range tries {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		found = true
		out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
		if err != nil || len(out) == 0 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if strings.HasPrefix(string(out[:min(len(out), 16)]), "\x89PNG") {
			return PrepareImage(out)
		}
	}
	if !found {
		return nil, ErrClipboardUnsupported
	}
	return nil, ErrNoClipboardImage
}
