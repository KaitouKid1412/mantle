//go:build !darwin && !linux

package editor

import "context"

// ReadClipboardImage is not supported on this platform yet.
func ReadClipboardImage(ctx context.Context) (*Image, error) {
	return nil, ErrClipboardUnsupported
}
