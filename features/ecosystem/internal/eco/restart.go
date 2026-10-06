package eco

import (
	"errors"
	"io/fs"
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
// --add-dir survive, clears the session fields, keeps the current model and
// permission mode, and picks the session flag like plan 06's hand-off
// (sessionFlags): headless claude writes a transcript only after the first
// turn, and `--resume` of a session that was never written makes claude exit
// ("No conversation found with session ID").
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
	o.Resume, o.SessionID = sessionFlags(o, s.SessionID)
	return o
}

// sessionFlags picks --resume or --session-id for restarting session sid with
// options o (the same rule as plan 06's hand-off):
//   - its transcript holds a message (sessions.Layout.Persisted): resume it;
//   - no transcript file yet: start a new session under the same id, so
//     mantle's view of the session doesn't change;
//   - a file without messages (a title set before the first prompt): no flag,
//     because claude refuses --session-id for an id whose file exists.
func sessionFlags(o ext.SpawnOpts, sid string) (resume, newID string) {
	if !sessions.ValidID(sid) {
		return "", ""
	}
	layout := engineLayout(o)
	if layout.Persisted(o.Cwd, sid) {
		return sid, ""
	}
	if _, err := os.Stat(sessions.SessionFile(layout.ProjectDir(o.Cwd), sid)); errors.Is(err, fs.ErrNotExist) {
		return "", sid
	}
	return "", ""
}

// engineLayout is the session layout the engine writes to, honouring a
// CLAUDE_CONFIG_DIR set in its own environment.
func engineLayout(o ext.SpawnOpts) sessions.Layout {
	return sessions.LayoutFromEnv(func(k string) string {
		if v, ok := o.Env[k]; ok {
			return v
		}
		return os.Getenv(k)
	})
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
