//go:build !unix

package gates

// lockFile is a no-op where flock is unavailable; writes stay atomic via rename.
func lockFile(string) (func(), error) { return func() {}, nil }
