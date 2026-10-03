package keymap

import (
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// Match is an action bound to the keys just pressed, in one context.
type Match struct {
	Action  ext.ActionID
	Context string
	Chord   string
}

// Outcome of resolving a key press.
type Outcome int

const (
	// NoMatch: nothing is bound; pass the key to the focused component.
	NoMatch Outcome = iota
	// Matched: Matches holds the bound actions, most specific context first. Offer
	// them in order; if none handles it, pass the key to the focused component.
	Matched
	// Pending: the key started or continued a chord; swallow it and wait.
	Pending
	// Abandoned: a pending chord did not complete; the prefix keys are dropped. The
	// key itself was re-resolved on its own (see Matches / Retry).
	Abandoned
)

// Result of Resolver.Resolve.
type Result struct {
	Outcome Outcome
	Matches []Match
	// For Abandoned: how the key resolved on its own (NoMatch, Matched or Pending).
	Retry Outcome
}

// Resolver turns key presses into actions, tracking chord state. It is not safe for
// concurrent use; the host calls it on the UI goroutine.
type Resolver struct {
	km      *Keymap
	pending []string
	since   time.Time
	Timeout time.Duration
}

// NewResolver returns a resolver with Claude Code's 3 s chord timeout.
func NewResolver(km *Keymap) *Resolver {
	return &Resolver{km: km, Timeout: ext.ChordTimeoutMillis * time.Millisecond}
}

// SetKeymap swaps the keymap (hot reload) and drops any pending chord.
func (r *Resolver) SetKeymap(km *Keymap) {
	r.km = km
	r.pending = nil
}

// Keymap returns the current keymap.
func (r *Resolver) Keymap() *Keymap { return r.km }

// Pending returns the keys of an unfinished chord ("ctrl+x"), or "" if none.
func (r *Resolver) Pending() string { return strings.Join(r.pending, " ") }

// Reset drops any pending chord.
func (r *Resolver) Reset() { r.pending = nil }

// Expired reports whether a pending chord has timed out at now (the host clears its
// "ctrl+x …" hint then).
func (r *Resolver) Expired(now time.Time) bool {
	return len(r.pending) > 0 && now.Sub(r.since) > r.Timeout
}

// Resolve handles one key press. keys are the event's canonical forms (EventKeys or
// WheelKey), most specific first. contexts is the active context stack, most specific
// first; "Global" is appended if missing.
func (r *Resolver) Resolve(contexts []string, keys []string, now time.Time) Result {
	if len(keys) == 0 {
		r.pending = nil
		return Result{Outcome: NoMatch}
	}
	if r.Expired(now) {
		r.pending = nil
	}
	stack := contexts
	if len(stack) == 0 || stack[len(stack)-1] != ext.ContextGlobal {
		stack = append(append([]string(nil), contexts...), ext.ContextGlobal)
	}
	if len(r.pending) > 0 {
		res, out := r.try(stack, keys, r.pending, now)
		if out != NoMatch {
			return res
		}
		r.pending = nil
		alone, _ := r.try(stack, keys, nil, now)
		return Result{Outcome: Abandoned, Matches: alone.Matches, Retry: alone.Outcome}
	}
	res, _ := r.try(stack, keys, nil, now)
	return res
}

func (r *Resolver) try(stack, keys, prefix []string, now time.Time) (Result, Outcome) {
	// A chord prefix in any active context takes priority over a single-key binding:
	// "ctrl+x" waits for the next key.
	for _, k := range keys {
		chord := strings.TrimSpace(strings.Join(prefix, " ") + " " + k)
		for _, c := range stack {
			if r.km.IsPrefix(c, chord) {
				r.pending = append(append([]string(nil), prefix...), k)
				r.since = now
				return Result{Outcome: Pending}, Pending
			}
		}
	}
	var matches []Match
	seen := map[ext.ActionID]bool{}
	for _, c := range stack {
		for _, k := range keys {
			chord := strings.TrimSpace(strings.Join(prefix, " ") + " " + k)
			if a, ok := r.km.Lookup(c, chord); ok && !seen[a] {
				seen[a] = true
				matches = append(matches, Match{Action: a, Context: c, Chord: chord})
				break // one match per context: the most specific key form
			}
		}
	}
	r.pending = nil
	if len(matches) == 0 {
		return Result{Outcome: NoMatch}, NoMatch
	}
	return Result{Outcome: Matched, Matches: matches}, Matched
}
