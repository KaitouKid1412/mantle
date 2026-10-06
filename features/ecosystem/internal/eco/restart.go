package eco

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/KaitouKid1412/mantle/internal/sessions"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// optioner is implemented by engines that report their launch options
// (internal/engine.Engine.Options).
type optioner interface{ Options() ext.SpawnOpts }

// RestartOpts returns the options for restarting the main engine after a login,
// logout or configuration change. It starts from the engine's own launch
// options, so the user's claude flags, the startup gates' --settings and
// --add-dir survive, and clears the session fields. It resumes the current
// session only when its transcript exists: headless claude writes a
// transcript after the first turn, and `--resume` of a session that was never
// written makes claude exit ("No conversation found with session ID"). A
// fresh restart keeps the current model and permission mode.
func RestartOpts(ctx ext.Ctx) ext.SpawnOpts {
	s := ctx.Session()
	var o ext.SpawnOpts
	if op, ok := ctx.Engine("").(optioner); ok {
		o = op.Options()
	}
	o.Resume, o.Continue, o.ForkSession = "", false, false
	o.ResumeSessionAt, o.ResumeDropsTurn, o.SessionID, o.Stop = "", false, "", false
	o.ExtraArgs = stripSessionArgs(o.ExtraArgs)
	if o.Cwd == "" {
		o.Cwd = s.Cwd
	}
	if s.Model != "" {
		o.Model = s.Model
	}
	if s.PermissionMode != "" {
		o.PermissionMode = s.PermissionMode
	}
	if s.SessionID != "" && SessionPersisted(o, s.SessionID) {
		o.Resume = s.SessionID
	}
	return o
}

// stripSessionArgs drops session-selecting flags from forwarded claude args, so
// a restart doesn't resume or continue something else.
func stripSessionArgs(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, _, hasValue := strings.Cut(a, "=")
		switch name {
		case "-c", "--continue", "--fork-session":
			continue
		case "-r", "--resume", "--session-id", "--resume-session-at", "--resume-drops-turn":
			if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// SessionPersisted reports whether claude can resume session id for an engine
// started with o: its transcript (<config>/projects/<project>/<id>.jsonl, with
// the engine's CLAUDE_CONFIG_DIR) exists and holds a conversation message.
func SessionPersisted(o ext.SpawnOpts, id string) bool {
	if !sessions.ValidID(id) {
		return false
	}
	getenv := func(k string) string {
		if v, ok := o.Env[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
	layout := sessions.LayoutFromEnv(getenv)
	for _, dir := range layout.ProjectDirs(o.Cwd) {
		if hasConversation(sessions.SessionFile(dir, id)) {
			return true
		}
	}
	return false
}

// hasConversation reports whether a transcript file has a user or assistant
// record. It reads at most a few thousand lines.
func hasConversation(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for n := 0; n < 5000 && sc.Scan(); n++ {
		var rec struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && (rec.Type == "user" || rec.Type == "assistant") {
			return true
		}
	}
	// A line longer than the buffer means a large message, so a conversation.
	return errors.Is(sc.Err(), bufio.ErrTooLong)
}
