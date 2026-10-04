package ext

import (
	"encoding/json"
	"os"
)

// Additions in contracts-v1.7 (plan 10's fd hand-off, request 10-02).

// HandoffEngine is an optional interface of Engine: engines that can be handed to a
// new mantle-ui process across exec (instant restart without killing claude).
type HandoffEngine interface {
	// Handoff prepares the engine for exec: it stops its goroutines without closing
	// the process's pipes and returns what the new process needs to adopt it.
	Handoff() (EngineHandoff, error)
}

// EngineHandoff is one engine's state carried across exec.
type EngineHandoff struct {
	EngineID     string
	PID, PGID    int
	Files        [3]*os.File // stdin (write end), stdout, stderr (read ends)
	Init         json.RawMessage
	Session      SessionInfo
	Capabilities []string
}
