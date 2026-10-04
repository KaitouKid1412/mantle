package selfmod

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// ErrUpToDate is returned by StartUpdate when the promotion branch already
// contains the upstream.
var ErrUpToDate = errors.New("already up to date")

// StartUpdate fetches origin and creates a worktree at the new upstream on
// which the promotion branch's commits are re-applied one at a time with
// UpdateStep. Promote then replaces the branch with the result.
func (w *Workspace) StartUpdate() (*Request, error) {
	if w.Branch == "" {
		return nil, errors.New("update is for ~/.mantle/src; in dev mode, rebase your mantle/mod-* branches yourself")
	}
	src := w.src()
	if _, err := src.run("fetch", "--quiet", "origin"); err != nil {
		return nil, err
	}
	head, err := src.RevParse(w.Branch)
	if err != nil {
		return nil, err
	}
	upstream, err := src.RevParse(w.Upstream)
	if err != nil {
		return nil, err
	}
	if ok, err := src.IsAncestor(upstream, head); err != nil {
		return nil, err
	} else if ok {
		return nil, ErrUpToDate
	}
	out, err := src.run("rev-list", "--reverse", "--no-merges", upstream+".."+head)
	if err != nil {
		return nil, err
	}
	if err := w.Layout.EnsureDirs(); err != nil {
		return nil, err
	}
	mods, _ := w.Mods()
	id := NewModID("update", w.idTaken(mods))
	r, err := w.startAt("update onto "+w.Upstream+" "+short(upstream), RequestUpdate, "", id, upstream)
	if err != nil {
		return nil, err
	}
	r.Pending = strings.Fields(out)
	r.UpdateFrom = head
	return r, w.Save(r)
}

// UpdateStep re-applies the update's pending commits in order. If a
// cherry-pick stopped on conflicts and the builder resolved them, it first
// commits the resolution with the original message (trailers included).
// On a new conflict it returns the commit being applied and a
// *ConflictError, leaving the cherry-pick in progress.
func (w *Workspace) UpdateStep(r *Request) (*Commit, error) {
	wt := w.git(r.Dir)
	if _, err := wt.run("rev-parse", "-q", "--verify", "CHERRY_PICK_HEAD"); err == nil {
		if err := stageResolved(wt); err != nil {
			return w.pendingCommit(r), err
		}
		args := append(wt.identityArgs(), "commit", "--no-verify", "--allow-empty", "-q", "-C", "CHERRY_PICK_HEAD")
		if _, err := wt.run(args...); err != nil {
			return w.pendingCommit(r), err
		}
		r.Pending = r.Pending[1:]
		w.Save(r)
	}
	for len(r.Pending) > 0 {
		if err := wt.CherryPick(r.Pending[0]); err != nil {
			return w.pendingCommit(r), err
		}
		r.Pending = r.Pending[1:]
		w.Save(r)
	}
	return nil, nil
}

func (w *Workspace) pendingCommit(r *Request) *Commit {
	if len(r.Pending) == 0 {
		return nil
	}
	cs, err := w.git(r.Dir).Log(r.Pending[0]+"^!", "")
	if err != nil || len(cs) == 0 {
		return &Commit{SHA: r.Pending[0]}
	}
	return &cs[0]
}

// OriginURL is the clone's origin: the dev repo.
func (w *Workspace) OriginURL() (string, error) { return w.src().run("remote", "get-url", "origin") }

// ExportUpstream exports mod id to the dev repo (the clone's origin) as branch
// mantle/mod-<id>, re-applied onto origin's main. It pushes to that branch
// only: the dev repo's checked-out branch and working tree are untouched.
func (w *Workspace) ExportUpstream(ctx context.Context, id string) (branch string, err error) {
	if w.Branch == "" {
		return "", errors.New("in dev mode the mod already is on its mantle/mod-* branch of the dev repo")
	}
	mods, err := w.Mods()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(mods, func(m Mod) bool { return m.ID == id })
	if i < 0 {
		return "", fmt.Errorf("no mod %q (see /mantle list)", id)
	}
	if mods[i].State != ModActive {
		return "", fmt.Errorf("mod %q is undone", id)
	}
	src := w.src()
	origin, err := src.run("remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	if _, err := src.run("fetch", "--quiet", "origin"); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(w.Layout.Work(), "upstream-"+id+"-")
	if err != nil {
		return "", err
	}
	os.Remove(tmp) // git worktree add wants to create it
	if err := src.WorktreeAddDetached(tmp, w.Upstream); err != nil {
		return "", err
	}
	defer src.WorktreeRemove(tmp)
	wt := w.git(tmp)
	var shas []string
	for _, c := range mods[i].Commits {
		if c.Trailers.Get(TrailerKind) != KindRevert {
			shas = append(shas, c.SHA)
		}
	}
	if err := wt.CherryPick(shas...); err != nil {
		wt.CherryPickAbort()
		return "", fmt.Errorf("the mod does not apply to %s: %w", w.Upstream, err)
	}
	branch = "mantle/mod-" + id
	if _, err := wt.run("push", "--quiet", "--force", origin, "HEAD:refs/heads/"+branch); err != nil {
		return "", err
	}
	return branch, nil
}
