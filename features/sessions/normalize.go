package sessions

import (
	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/internal/sessions/normalize"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// NormalizeOptions controls Normalize (internal/sessions/normalize.Options).
type NormalizeOptions = normalize.Options

// keyResult is normalize.KeyResult.
var keyResult = normalize.KeyResult

// Normalize turns a transcript's active branch into transcript items. It forwards to
// internal/sessions/normalize, which other features use directly.
func Normalize(t *sessions.Transcript, opts NormalizeOptions) []*ext.Item {
	return normalize.Normalize(t, opts)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
