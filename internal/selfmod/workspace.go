package selfmod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/KaitouKid1412/mantle/internal/launcher"
)

// Request kinds.
const (
	RequestMod    = "mod"    // /mantle <request>
	RequestUndo   = "undo"   // /mantle undo <id>
	RequestEdit   = "edit"   // /mantle edit <id> <request>
	RequestUpdate = "update" // /mantle update
)

// Request states.
const (
	StateBuilding  = "building"  // the builder is working in the worktree
	StateVetting   = "vetting"   // the pipeline is running
	StateFailed    = "failed"    // the pipeline rejected it; worktree kept for retry/edit
	StateReady     = "ready"     // the pipeline passed; waiting for promotion
	StatePromoted  = "promoted"  // installed as a version
	StateConfig    = "config"    // satisfied by a config change, nothing built
	StateAbandoned = "abandoned" // worktree removed without promotion
)

// MaxRounds is how many builder rounds a request gets before it stops and
// keeps the worktree for /mantle retry or edit.
const MaxRounds = 3

// Request is one in-flight or finished /mantle request, persisted as
// builds/<id>/request.json.
type Request struct {
	ID      string `json:"id"`
	Request string `json:"request"`
	Kind    string `json:"kind"`
	// Target is the mod an undo or edit applies to.
	Target string `json:"target,omitempty"`
	// Base is the commit the worktree started from.
	Base   string `json:"base"`
	Branch string `json:"branch"`
	Dir    string `json:"dir"`
	Round  int    `json:"round"`
	State  string `json:"state"`
	// LastFailure is TrimForBuilder of the last failed pipeline run.
	LastFailure string `json:"last_failure,omitempty"`
	// LastReport is the path of the last pipeline report.
	LastReport string    `json:"last_report,omitempty"`
	BuildID    string    `json:"build_id,omitempty"`
	CostUSD    float64   `json:"cost_usd,omitempty"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
}

// Done reports whether the request reached a final state.
func (r *Request) Done() bool {
	return r.State == StatePromoted || r.State == StateConfig || r.State == StateAbandoned
}

// Workspace is the /mantle working area: the source repository, worktrees
// per request, build logs and the version store.
type Workspace struct {
	Layout launcher.Layout
	// Source is the repository mods are built from (~/.mantle/src, or the
	// dev repo in dev mode).
	Source string
	// Upstream is the ref mods sit on ("origin/main"; "main" in dev mode).
	Upstream string
	// Branch is the branch promotions fast-forward ("user"). Empty in dev
	// mode: each mod stays on its own branch and is never merged.
	Branch string
	// BranchPrefix names request branches ("mod/"; "mantle/mod-" in dev mode).
	BranchPrefix string
	// Pipeline configures the checks.
	Pipeline Config
	Go, Git  string
	Now      func() time.Time
}

// NewWorkspace returns the standard workspace in l: ~/.mantle/src with
// branch user on origin/main.
func NewWorkspace(l launcher.Layout) *Workspace {
	return &Workspace{Layout: l, Source: l.Src(), Upstream: "origin/main", Branch: UserBranch, BranchPrefix: "mod/"}
}

// NewDevWorkspace returns a dev-mode workspace on the dev repo: requests
// branch from base (usually "main") as mantle/mod-<id> and are never merged.
func NewDevWorkspace(l launcher.Layout, devRepo, base string) *Workspace {
	return &Workspace{Layout: l, Source: devRepo, Upstream: base, BranchPrefix: "mantle/mod-"}
}

func (w *Workspace) git(dir string) Git { return Git{Dir: dir, Bin: w.Git} }
func (w *Workspace) src() Git           { return w.git(w.Source) }
func (w *Workspace) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// head is the ref new requests branch from.
func (w *Workspace) head() string {
	if w.Branch != "" {
		return w.Branch
	}
	return w.Upstream
}

func (w *Workspace) requestFile(id string) string {
	return filepath.Join(w.Layout.Build(id), "request.json")
}

// Save writes r to builds/<id>/request.json.
func (w *Workspace) Save(r *Request) error {
	r.Updated = w.now().UTC()
	if err := os.MkdirAll(w.Layout.Build(r.ID), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.requestFile(r.ID), append(data, '\n'), 0o644)
}

// Load reads a request by id.
func (w *Workspace) Load(id string) (*Request, error) {
	if !ValidModID(id) {
		return nil, fmt.Errorf("invalid request id %q", id)
	}
	data, err := os.ReadFile(w.requestFile(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("no request %q", id)
	}
	if err != nil {
		return nil, err
	}
	var r Request
	return &r, json.Unmarshal(data, &r)
}

// Requests lists every saved request, newest first.
func (w *Workspace) Requests() ([]*Request, error) {
	ents, err := os.ReadDir(w.Layout.Builds())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Request
	for _, e := range ents {
		if !e.IsDir() || !ValidModID(e.Name()) {
			continue
		}
		if r, err := w.Load(e.Name()); err == nil {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b *Request) int { return b.Created.Compare(a.Created) })
	return out, nil
}

// Mods lists the mods on the promotion branch, oldest first.
func (w *Workspace) Mods() ([]Mod, error) {
	return w.src().ListMods(w.Upstream + ".." + w.head())
}

// idTaken reports whether id is used by a request, a worktree or a mod.
func (w *Workspace) idTaken(mods []Mod) func(string) bool {
	return func(id string) bool {
		if _, err := os.Stat(w.Layout.Build(id)); err == nil {
			return true
		}
		if _, err := os.Stat(w.Layout.Worktree(id)); err == nil {
			return true
		}
		return slices.ContainsFunc(mods, func(m Mod) bool { return m.ID == id })
	}
}

// Start creates a worktree for a new request on a fresh branch from the
// promotion branch's head.
func (w *Workspace) Start(request, kind, target string) (*Request, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return nil, errors.New("empty request")
	}
	if err := w.Layout.EnsureDirs(); err != nil {
		return nil, err
	}
	mods, err := w.Mods()
	if err != nil {
		return nil, fmt.Errorf("cannot read mods from %s: %w", w.Source, err)
	}
	seed := request
	switch kind {
	case RequestUndo:
		seed = "undo " + target
	case RequestEdit:
		seed = "edit " + target
	}
	id := NewModID(seed, w.idTaken(mods))
	base, err := w.src().RevParse(w.head())
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s in %s: %w", w.head(), w.Source, err)
	}
	r := &Request{
		ID: id, Request: request, Kind: kind, Target: target, Base: base,
		Branch: w.BranchPrefix + id, Dir: w.Layout.Worktree(id),
		State: StateBuilding, Created: w.now().UTC(),
	}
	if err := w.src().WorktreeAdd(r.Dir, r.Branch, base); err != nil {
		return nil, err
	}
	if err := w.Save(r); err != nil {
		return nil, err
	}
	return r, nil
}

// StartUndo creates a worktree that reverts mod target in one commit.
func (w *Workspace) StartUndo(target string) (*Request, error) {
	mods, err := w.Mods()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(mods, func(m Mod) bool { return m.ID == target })
	if i < 0 {
		return nil, fmt.Errorf("no mod %q (see /mantle list)", target)
	}
	if mods[i].State == ModUndone {
		return nil, fmt.Errorf("mod %q is already undone", target)
	}
	r, err := w.Start("undo "+target, RequestUndo, target)
	if err != nil {
		return nil, err
	}
	if _, err := w.git(r.Dir).RevertMod(target); err != nil {
		w.git(r.Dir).RevertAbort()
		w.Abandon(r)
		return nil, fmt.Errorf("undo %s: %w", target, err)
	}
	return r, nil
}

// Vet runs the pipeline over the request's worktree as the next round and
// records the outcome.
func (w *Workspace) Vet(ctx context.Context, r *Request) (*Report, error) {
	r.Round++
	r.State = StateVetting
	w.Save(r)
	logDir := filepath.Join(w.Layout.Build(r.ID), "round-"+strconv.Itoa(r.Round))
	rep, err := NewPipeline(w.Pipeline).Run(ctx, Run{BuildID: r.ID, Dir: r.Dir, Base: r.Base, LogDir: logDir})
	if err != nil {
		r.State = StateFailed
		r.LastFailure = err.Error()
		w.Save(r)
		return nil, err
	}
	r.LastReport = filepath.Join(logDir, "report.json")
	if rep.OK {
		r.State, r.LastFailure = StateReady, ""
	} else {
		r.State, r.LastFailure = StateFailed, TrimForBuilder(rep)
	}
	return rep, w.Save(r)
}

// Promotion is the result of Promote.
type Promotion struct {
	Version launcher.Version
	// Rebased is true if the promotion branch had moved and the request was
	// rebased and re-vetted (steps 6-9).
	Rebased bool
	// Commits are the request's new commits.
	Commits []string
}

// ErrRevetFailed is returned when the post-rebase checks fail.
var ErrRevetFailed = errors.New("checks failed after rebasing onto the moved branch")

// Promote commits a vetted request, rebases it onto the promotion branch if
// that moved (re-running steps 6-9), fast-forwards the branch, builds and
// installs a version from it and makes it current, all under promote.lock.
// The worktree and request branch are removed on success. rep is the
// passing report of the last Vet.
func (w *Workspace) Promote(ctx context.Context, r *Request, rep *Report) (*Promotion, error) {
	if rep == nil || !rep.OK {
		return nil, errors.New("promote: the request has not passed the pipeline")
	}
	lock, err := launcher.LockFile(ctx, w.Layout.PromoteLock())
	if err != nil {
		return nil, err
	}
	defer lock.Unlock()

	wt := w.git(r.Dir)
	p := &Promotion{}
	modID := r.ID
	if r.Kind == RequestEdit && r.Target != "" {
		modID = r.Target
	}
	if clean, err := wt.IsClean(); err != nil {
		return nil, err
	} else if !clean {
		if p.Commits, err = wt.CommitMod(ModCommit{ID: modID, Request: r.Request}); err != nil {
			return nil, err
		}
	}
	summary := rep.Summary(w.Layout.Build(r.ID))

	buildDir := w.Source
	if w.Branch == "" {
		buildDir = r.Dir // dev mode: install from the request branch, never merge
	} else {
		src := w.src()
		if branch, _ := src.CurrentBranch(); branch != w.Branch {
			return nil, fmt.Errorf("%s must have %s checked out (it has %q)", w.Source, w.Branch, branch)
		}
		if clean, err := src.IsClean(); err != nil || !clean {
			return nil, fmt.Errorf("%s has uncommitted changes; promotion needs it clean", w.Source)
		}
		head, err := src.RevParse(w.Branch)
		if err != nil {
			return nil, err
		}
		if ok, err := wt.IsAncestor(head, "HEAD"); err != nil {
			return nil, err
		} else if !ok {
			if err := wt.Rebase(w.Branch); err != nil {
				return nil, err // *ConflictError leaves the rebase for the builder
			}
			p.Rebased = true
			logDir := filepath.Join(w.Layout.Build(r.ID), "promote")
			rerun, err := NewPipeline(w.Pipeline).Run(ctx, Run{BuildID: r.ID, Dir: r.Dir, Base: head, LogDir: logDir, Steps: PostRebaseSteps})
			if err != nil {
				return nil, err
			}
			if !rerun.OK {
				r.State, r.LastFailure, r.LastReport = StateFailed, TrimForBuilder(rerun), filepath.Join(logDir, "report.json")
				w.Save(r)
				return nil, fmt.Errorf("%w:\n%s", ErrRevetFailed, r.LastFailure)
			}
			summary = rerun.Summary(w.Layout.Build(r.ID))
		}
		if err := src.MergeFastForward(r.Branch); err != nil {
			return nil, err
		}
	}

	v, err := BuildVersion(ctx, BuildOptions{
		Layout: w.Layout, Dir: buildDir, Upstream: w.Upstream, Source: r.Kind,
		Pipeline: summary, Go: w.Go, Git: w.Git, Now: w.Now,
	})
	if err != nil {
		return nil, err
	}
	store := launcher.Store{L: w.Layout}
	if err := store.SetCurrent(v.ID); err != nil {
		return nil, err
	}
	store.GC(KeepVersions, nil)
	p.Version = v
	r.State, r.BuildID = StatePromoted, v.ID
	w.Save(r)
	if w.Branch != "" {
		w.removeWorktree(r)
	}
	return p, nil
}

func (w *Workspace) removeWorktree(r *Request) {
	w.src().WorktreeRemove(r.Dir)
	if w.src().BranchExists(r.Branch) {
		w.src().DeleteBranch(r.Branch)
	}
}

// Abandon removes a request's worktree and branch without promoting it.
func (w *Workspace) Abandon(r *Request) error {
	w.removeWorktree(r)
	r.State = StateAbandoned
	return w.Save(r)
}

// Rollback flips current to last-good (or the previous build) without
// rebuilding, under promote.lock. It returns the old and new build ids.
func (w *Workspace) Rollback(ctx context.Context) (from, to string, err error) {
	lock, err := launcher.LockFile(ctx, w.Layout.PromoteLock())
	if err != nil {
		return "", "", err
	}
	defer lock.Unlock()
	store := launcher.Store{L: w.Layout}
	from = store.CurrentID()
	to, err = launcher.RollbackTarget(store, from)
	if err != nil {
		return from, "", err
	}
	launcher.ResetProbation(w.Layout, to)
	return from, to, store.SetCurrent(to)
}

// ModDetail is what /mantle show prints.
type ModDetail struct {
	Mod   Mod
	Stats []string // `git show --stat` per commit
	// Request is the saved request, if the mod was built on this machine.
	Request *Request
	Report  *Report
}

// Show returns the details of mod id.
func (w *Workspace) Show(id string) (*ModDetail, error) {
	mods, err := w.Mods()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(mods, func(m Mod) bool { return m.ID == id })
	if i < 0 {
		return nil, fmt.Errorf("no mod %q (see /mantle list)", id)
	}
	d := &ModDetail{Mod: mods[i]}
	for _, c := range d.Mod.Commits {
		if st, err := w.src().run("show", "--stat", "--format=%h %s", c.SHA); err == nil {
			d.Stats = append(d.Stats, st)
		}
	}
	if r, err := w.Load(id); err == nil {
		d.Request = r
		if data, err := os.ReadFile(r.LastReport); err == nil {
			var rep Report
			if json.Unmarshal(data, &rep) == nil {
				d.Report = &rep
			}
		}
	}
	return d, nil
}

// Status is what /mantle status prints.
type Status struct {
	Current   string
	LastGood  string
	Probation *launcher.ProbationState // nil when current is healthy
	Active    []*Request               // requests not in a final state
}

// Status reports the current build, its probation state and the requests
// in flight.
func (w *Workspace) Status() (*Status, error) {
	store := launcher.Store{L: w.Layout}
	st := &Status{Current: store.CurrentID(), LastGood: store.LastGoodID()}
	if st.Current != "" {
		if p := launcher.LoadProbation(w.Layout, st.Current); !p.Healthy {
			st.Probation = &p
		}
	}
	reqs, err := w.Requests()
	if err != nil {
		return st, err
	}
	for _, r := range reqs {
		if !r.Done() {
			st.Active = append(st.Active, r)
		}
	}
	return st, nil
}
