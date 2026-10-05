package engine

import (
	"sync"

	"github.com/KaitouKid1412/mantle/pkg/proto"
)

// capabilities answers Supports(x) for control subtypes and engine capabilities.
//
//   - Documented control subtypes are supported until the engine answers one with
//     "Unsupported control request subtype".
//   - Unstable subtypes (unstable.go) are supported only on engine versions they were
//     verified on, and also turn off on an unsupported error.
//   - Capability names (interrupt_receipt_v1, ...) come from the initialize response
//     and system/init capabilities[].
type capabilities struct {
	mu       sync.RWMutex
	known    map[string]bool
	disabled map[string]bool
	caps     map[string]bool
	version  string
}

func newCapabilities() *capabilities {
	c := &capabilities{known: map[string]bool{}, disabled: map[string]bool{}, caps: map[string]bool{}}
	for _, s := range proto.KnownRequestSubtypes() {
		c.known[s] = true
	}
	return c
}

// Supports reports whether subtype (or capability name) can be used.
func (c *capabilities) Supports(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.disabled[name] {
		return false
	}
	if c.known[name] || c.caps[name] {
		return true
	}
	if u, ok := unstableSubtypes[name]; ok {
		return versionAtLeast(c.version, u.verifiedOn)
	}
	return false
}

// Disable turns a subtype off (the engine said it is unsupported).
func (c *capabilities) Disable(subtype string) {
	c.mu.Lock()
	c.disabled[subtype] = true
	c.mu.Unlock()
}

// AddCapabilities records capability names reported by the engine.
func (c *capabilities) AddCapabilities(names []string) {
	c.mu.Lock()
	for _, n := range names {
		c.caps[n] = true
	}
	c.mu.Unlock()
}

// list returns the capability names reported by the engine.
func (c *capabilities) list() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.caps))
	for n := range c.caps {
		out = append(out, n)
	}
	return out
}

// SetVersion records the engine version (from system/init).
func (c *capabilities) SetVersion(v string) {
	c.mu.Lock()
	c.version = v
	c.mu.Unlock()
}

// versionAtLeast compares dotted numeric versions ("2.1.288"). An unknown version
// counts as not new enough.
func versionAtLeast(v, min string) bool {
	if v == "" {
		return false
	}
	a, b := splitVersion(v), splitVersion(min)
	for i := 0; i < len(b); i++ {
		var x int
		if i < len(a) {
			x = a[i]
		}
		if x != b[i] {
			return x > b[i]
		}
	}
	return true
}

func splitVersion(v string) []int {
	var out []int
	n, digits := 0, false
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
			digits = true
		case r == '.':
			out = append(out, n)
			n, digits = 0, false
		default:
			if digits {
				out = append(out, n)
			}
			return out
		}
	}
	if digits {
		out = append(out, n)
	}
	return out
}
