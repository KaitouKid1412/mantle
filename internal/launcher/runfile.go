package launcher

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RunFile is $MANTLE_HOME/run/<pid>.json, written by mantle-ui while it runs
// and read by the launcher after it exits.
type RunFile struct {
	// PID is mantle-ui's pid (also the file name).
	PID         int `json:"pid"`
	LauncherPID int `json:"launcher_pid,omitempty"`
	// Version is the build id of the running mantle-ui.
	Version   string `json:"version,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
	// Argv is mantle-ui's original arguments (without argv[0]).
	Argv []string `json:"argv,omitempty"`
	// HandoffArgs are the arguments for a relaunch (exit 75 or rollback):
	// the original flags without the initial prompt and session selection,
	// plus --resume <session-id>. mantle-ui computes them because only it
	// has the full flag table.
	HandoffArgs []string `json:"handoff_args,omitempty"`
	// EnginePGIDs maps engine ids ("main", "builder-<id>", ...) to their
	// process groups, which the launcher kills if mantle-ui dies abnormally.
	EnginePGIDs map[string]int `json:"engine_pgids,omitempty"`
	Started     time.Time      `json:"started,omitzero"`
	Updated     time.Time      `json:"updated,omitzero"`

	// Engine records share run/: internal/engine writes run/<engine-pid>.json
	// with these fields for every engine it spawns, so the launcher can kill
	// the engines of a UI that died.
	EngineID string `json:"engine_id,omitempty"`
	PGID     int    `json:"pgid,omitempty"`
	UIPID    int    `json:"ui_pid,omitempty"`
}

// IsEngine reports whether rf is an engine record rather than a UI run file.
func (rf RunFile) IsEngine() bool { return rf.EngineID != "" }

// enginePGID is the process group of an engine record.
func (rf RunFile) enginePGID() int {
	if rf.PGID != 0 {
		return rf.PGID
	}
	return rf.PID
}

// RelaunchArgs returns the arguments for relaunching mantle-ui after this
// run: HandoffArgs if set, else --resume <session-id>, else none. The
// original argv is never reused because it may contain an initial prompt.
func (rf RunFile) RelaunchArgs() []string {
	if len(rf.HandoffArgs) > 0 {
		return rf.HandoffArgs
	}
	if rf.SessionID != "" {
		return []string{"--resume", rf.SessionID}
	}
	return nil
}

// ReadRunFile reads one run file.
func ReadRunFile(path string) (RunFile, error) {
	var rf RunFile
	data, err := os.ReadFile(path)
	if err != nil {
		return rf, err
	}
	err = json.Unmarshal(data, &rf)
	return rf, err
}

// WriteRunFile writes rf to path atomically, setting Updated.
func WriteRunFile(path string, rf RunFile) error {
	rf.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

// RunFiles reads every run file in run/, skipping unreadable ones.
func (l Layout) RunFiles() ([]RunFile, error) {
	ents, err := os.ReadDir(l.Run())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []RunFile
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		rf, err := ReadRunFile(filepath.Join(l.Run(), name))
		if err != nil {
			continue
		}
		if rf.PID == 0 {
			rf.PID = pid
		}
		out = append(out, rf)
	}
	return out, nil
}

// RemoveStaleRunFiles deletes run files whose process is gone and returns
// how many it removed.
func (l Layout) RemoveStaleRunFiles(alive func(int) bool) int {
	if alive == nil {
		alive = ProcessAlive
	}
	runs, _ := l.RunFiles()
	n := 0
	for _, rf := range runs {
		if !alive(rf.PID) && os.Remove(l.RunFile(rf.PID)) == nil {
			n++
		}
	}
	return n
}

// ProcessAlive reports whether a process with this pid exists.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// HealthyMarker is the path of state/healthy-<build-id>.
func (l Layout) HealthyMarker(buildID string) string {
	return filepath.Join(l.State(), "healthy-"+buildID)
}

// MarkHealthy writes the healthy marker for buildID, ending its probation.
func MarkHealthy(l Layout, buildID string) error {
	if !ValidBuildID(buildID) {
		return ErrInvalidBuildID
	}
	if err := os.MkdirAll(l.State(), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(l.HealthyMarker(buildID), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// IsHealthy reports whether buildID has passed probation.
func IsHealthy(l Layout, buildID string) bool {
	_, err := os.Stat(l.HealthyMarker(buildID))
	return err == nil
}

func (l Layout) probationFile(buildID string) string {
	return filepath.Join(l.State(), "probation-"+buildID+".json")
}

type probationRecord struct {
	Failures    int       `json:"failures"`
	LastFailure time.Time `json:"last_failure,omitzero"`
}

// LoadProbation returns the probation state of buildID from state/.
func LoadProbation(l Layout, buildID string) ProbationState {
	st := ProbationState{Healthy: IsHealthy(l, buildID)}
	if data, err := os.ReadFile(l.probationFile(buildID)); err == nil {
		var rec probationRecord
		if json.Unmarshal(data, &rec) == nil {
			st.Failures = rec.Failures
		}
	}
	return st
}

// SaveProbation persists st for buildID: the healthy marker and the failure
// counter.
func SaveProbation(l Layout, buildID string, st ProbationState) error {
	if st.Healthy {
		os.Remove(l.probationFile(buildID))
		if IsHealthy(l, buildID) {
			return nil
		}
		return MarkHealthy(l, buildID)
	}
	if st.Failures == 0 {
		err := os.Remove(l.probationFile(buildID))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(l.State(), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(probationRecord{Failures: st.Failures, LastFailure: time.Now().UTC()})
	return writeFileAtomic(l.probationFile(buildID), append(data, '\n'), 0o644)
}

// ResetProbation clears the failure counter of buildID (used when the user
// explicitly selects a version).
func ResetProbation(l Layout, buildID string) {
	os.Remove(l.probationFile(buildID))
}
