package selfmod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Trailer keys carried by every mod commit.
const (
	TrailerMod     = "Mantle-Mod"
	TrailerRequest = "Mantle-Request"
	TrailerKind    = "Mantle-Kind"
)

// Mod commit kinds (Mantle-Kind).
const (
	KindMod      = "mod"       // files only under mods/
	KindCoreSeam = "core-seam" // files outside mods/, committed before the mod
	KindRevert   = "revert"    // /mantle undo
)

// ModsDir is the top-level directory for mod packages.
const ModsDir = "mods"

// Git runs git in one repository or worktree.
type Git struct {
	Dir string
	// Bin is the git binary; "" means "git".
	Bin string
	// Env is appended to the environment of every git command.
	Env []string
}

// GitError is a failed git command.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *GitError) Unwrap() error { return e.Err }

// ConflictError reports a rebase, revert or cherry-pick that stopped on
// conflicts. The operation is left in progress so it can be resolved and
// continued, or aborted.
type ConflictError struct {
	Op    string // "rebase", "revert", "cherry-pick"
	Files []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s stopped on conflicts in %s", e.Op, strings.Join(e.Files, ", "))
}

// ErrNothingToCommit is returned by CommitMod when the worktree is clean.
var ErrNothingToCommit = errors.New("nothing to commit")

func (g Git) cmd(ctx context.Context, stdin []byte, args ...string) *exec.Cmd {
	bin := g.Bin
	if bin == "" {
		bin = "git"
	}
	full := append([]string{"-C", g.Dir, "--literal-pathspecs", "-c", "core.quotepath=off"}, args...)
	c := exec.CommandContext(ctx, bin, full...)
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_SEQUENCE_EDITOR=true", "LC_ALL=C")
	c.Env = append(c.Env, g.Env...)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	return c
}

// run executes git and returns stdout with surrounding whitespace trimmed.
func (g Git) run(args ...string) (string, error) {
	out, err := g.runRaw(nil, args...)
	return strings.TrimSpace(out), err
}

func (g Git) runRaw(stdin []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c := g.cmd(ctx, stdin, args...)
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := c.Run(); err != nil {
		return stdout.String(), &GitError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// RevParse resolves rev to a full sha.
func (g Git) RevParse(rev string) (string, error) {
	return g.run("rev-parse", "--verify", "--quiet", rev+"^{commit}")
}

// HeadSHA is the sha of HEAD.
func (g Git) HeadSHA() (string, error) { return g.RevParse("HEAD") }

// CurrentBranch is the checked-out branch, or "" when HEAD is detached.
func (g Git) CurrentBranch() (string, error) {
	out, err := g.run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		var ge *GitError
		if errors.As(err, &ge) {
			return "", nil
		}
	}
	return out, err
}

// BranchExists reports whether refs/heads/name exists.
func (g Git) BranchExists(name string) bool {
	_, err := g.run("show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// IsAncestor reports whether a is an ancestor of (or equal to) b.
func (g Git) IsAncestor(a, b string) (bool, error) {
	_, err := g.run("merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	var ge *GitError
	var ee *exec.ExitError
	if errors.As(err, &ge) && errors.As(ge.Err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// MergeBase returns the best common ancestor of a and b.
func (g Git) MergeBase(a, b string) (string, error) { return g.run("merge-base", a, b) }

// WorktreeAdd creates a worktree at path on a new branch starting at base.
func (g Git) WorktreeAdd(path, branch, base string) error {
	_, err := g.run("worktree", "add", "-b", branch, path, base)
	return err
}

// WorktreeAddDetached creates a worktree at path with a detached HEAD at rev.
func (g Git) WorktreeAddDetached(path, rev string) error {
	_, err := g.run("worktree", "add", "--detach", path, rev)
	return err
}

// WorktreeRemove removes the worktree at path (discarding its changes) and
// prunes stale worktree records.
func (g Git) WorktreeRemove(path string) error {
	_, err := g.run("worktree", "remove", "--force", "--force", path)
	if _, perr := g.run("worktree", "prune"); err == nil {
		err = perr
	}
	return err
}

// DeleteBranch deletes a local branch, merged or not.
func (g Git) DeleteBranch(name string) error {
	_, err := g.run("branch", "-D", name)
	return err
}

// ChangedFiles lists every path that differs between base and the working
// tree: committed, staged and unstaged changes to tracked files, deletions,
// both sides of renames, and untracked files. Sorted, without duplicates.
func (g Git) ChangedFiles(base string) ([]string, error) {
	out, err := g.runRaw(nil, "diff", "--name-only", "-z", "--no-renames", base)
	if err != nil {
		return nil, err
	}
	files := splitNUL(out)
	untracked, err := g.Untracked()
	if err != nil {
		return nil, err
	}
	files = append(files, untracked...)
	slices.Sort(files)
	return slices.Compact(files), nil
}

// Untracked lists untracked, non-ignored files.
func (g Git) Untracked() ([]string, error) {
	out, err := g.runRaw(nil, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

// IsClean reports whether the worktree has no changes and no untracked files.
func (g Git) IsClean() (bool, error) {
	out, err := g.run("status", "--porcelain", "--untracked-files=all")
	return out == "", err
}

// ShowFile returns the contents of path at rev.
func (g Git) ShowFile(rev, path string) ([]byte, error) {
	out, err := g.runRaw(nil, "show", rev+":"+path)
	return []byte(out), err
}

// DiffStat returns `git diff --stat` between two revisions.
func (g Git) DiffStat(from, to string) (string, error) {
	return g.run("diff", "--stat", from, to)
}

// ModCommit describes the changes of one /mantle request.
type ModCommit struct {
	ID      string
	Request string
}

// identityArgs makes commits work in a clone without user.name/user.email,
// never signs (a pinentry prompt would hang the pipeline) and skips hooks.
func (g Git) identityArgs() []string {
	args := []string{"-c", "commit.gpgsign=false"}
	if email, _ := g.run("config", "user.email"); email == "" {
		args = append(args, "-c", "user.email=mantle@localhost")
	}
	if name, _ := g.run("config", "user.name"); name == "" {
		args = append(args, "-c", "user.name=mantle")
	}
	return args
}

// CommitMod commits every change in the worktree as one mod, applying the
// commit-split rule: files outside mods/ go into a core-seam commit first,
// then files under mods/ go into a mod commit. Both carry the same
// Mantle-Mod id. It returns the new commit shas, oldest first.
func (g Git) CommitMod(m ModCommit) ([]string, error) { return g.commitMod(m, nil) }

// CommitModFixups commits like CommitMod, except that a part whose kind
// appears in fixupOf (kind → subject of the mod's existing commit of that
// kind) becomes a "fixup! <subject>" commit, for Autosquash.
func (g Git) CommitModFixups(m ModCommit, fixupOf map[string]string) ([]string, error) {
	return g.commitMod(m, fixupOf)
}

// Autosquash folds fixup! commits into their targets: a non-interactive
// `rebase -i --autosquash` from base. On conflicts it aborts and returns a
// *ConflictError, leaving the branch as it was.
func (g Git) Autosquash(base string) error {
	args := append(g.identityArgs(), "rebase", "-i", "--autosquash", "--no-autostash", base)
	if _, err := g.run(args...); err != nil {
		err = g.conflicts("rebase", err)
		g.RebaseAbort()
		return err
	}
	return nil
}

func (g Git) commitMod(m ModCommit, fixupOf map[string]string) ([]string, error) {
	if !ValidModID(m.ID) {
		return nil, fmt.Errorf("invalid mod id %q", m.ID)
	}
	if _, err := g.run("add", "-A"); err != nil {
		return nil, err
	}
	out, err := g.runRaw(nil, "diff", "--cached", "--name-only", "-z", "--no-renames")
	if err != nil {
		return nil, err
	}
	files := splitNUL(out)
	if len(files) == 0 {
		return nil, ErrNothingToCommit
	}
	if _, err := g.run("reset", "-q"); err != nil {
		return nil, err
	}
	core, mods := SplitModFiles(files)
	var shas []string
	for _, part := range []struct {
		kind  string
		files []string
	}{{KindCoreSeam, core}, {KindMod, mods}} {
		if len(part.files) == 0 {
			continue
		}
		if _, err := g.runRaw(nulJoin(part.files), "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return shas, err
		}
		msg := CommitMessage(m, part.kind)
		if subject, ok := fixupOf[part.kind]; ok && subject != "" {
			_, trailers, _ := strings.Cut(msg, "\n\n")
			msg = "fixup! " + subject + "\n\n" + trailers
		}
		sha, err := g.commit(msg)
		if err != nil {
			return shas, err
		}
		shas = append(shas, sha)
	}
	return shas, nil
}

func (g Git) commit(msg string) (string, error) {
	args := append(g.identityArgs(), "commit", "--no-verify", "-q", "-F", "-")
	if _, err := g.runRaw([]byte(msg), args...); err != nil {
		return "", err
	}
	return g.HeadSHA()
}

// SplitModFiles splits paths into those outside mods/ and those under it.
func SplitModFiles(files []string) (core, mods []string) {
	for _, f := range files {
		if strings.HasPrefix(f, ModsDir+"/") {
			mods = append(mods, f)
		} else {
			core = append(core, f)
		}
	}
	return core, mods
}

// CommitMessage builds the message of a mod commit with its trailers.
func CommitMessage(m ModCommit, kind string) string {
	req := OneLine(m.Request)
	var subject string
	switch kind {
	case KindCoreSeam:
		subject = "mantle: core seam for " + m.ID
	case KindRevert:
		subject = "mantle: undo " + m.ID
	default:
		subject = "mantle: " + m.ID
	}
	if req != "" && kind != KindRevert {
		subject += ": " + truncate(req, 60)
	}
	return fmt.Sprintf("%s\n\n%s: %s\n%s: %s\n%s: %s\n", subject,
		TrailerMod, m.ID, TrailerRequest, truncate(req, 500), TrailerKind, kind)
}

// OneLine collapses whitespace, including newlines, to single spaces.
func OneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (g Git) conflicts(op string, err error) error {
	files, ferr := g.run("diff", "--name-only", "--diff-filter=U")
	if ferr == nil && files != "" {
		return &ConflictError{Op: op, Files: strings.Split(files, "\n")}
	}
	return err
}

// Rebase rebases the current branch onto onto. On conflicts it returns a
// *ConflictError and leaves the rebase in progress.
func (g Git) Rebase(onto string) error {
	args := append(g.identityArgs(), "rebase", "--no-autostash", onto)
	if _, err := g.run(args...); err != nil {
		return g.conflicts("rebase", err)
	}
	return nil
}

// RebaseContinue continues a rebase after conflicts were resolved and staged.
func (g Git) RebaseContinue() error {
	args := append(g.identityArgs(), "rebase", "--continue")
	if _, err := g.run(args...); err != nil {
		return g.conflicts("rebase", err)
	}
	return nil
}

// RebaseAbort abandons a rebase in progress.
func (g Git) RebaseAbort() error { _, err := g.run("rebase", "--abort"); return err }

// CherryPick applies commits onto HEAD in the given order. On conflicts it
// returns a *ConflictError and leaves the cherry-pick in progress.
func (g Git) CherryPick(shas ...string) error {
	args := append(g.identityArgs(), "cherry-pick", "--allow-empty")
	if _, err := g.run(append(args, shas...)...); err != nil {
		return g.conflicts("cherry-pick", err)
	}
	return nil
}

// CherryPickAbort abandons a cherry-pick in progress.
func (g Git) CherryPickAbort() error { _, err := g.run("cherry-pick", "--abort"); return err }

// RevertMod reverts every commit of mod id reachable from HEAD in one commit
// with Mantle-Kind: revert. On conflicts it returns a *ConflictError and
// leaves the revert in progress (abort with RevertAbort).
func (g Git) RevertMod(id string) (string, error) {
	mods, err := g.ListMods("HEAD")
	if err != nil {
		return "", err
	}
	idx := slices.IndexFunc(mods, func(m Mod) bool { return m.ID == id })
	if idx < 0 {
		return "", fmt.Errorf("mod %q not found", id)
	}
	mod := mods[idx]
	if mod.State == ModUndone {
		return "", fmt.Errorf("mod %q is already undone", id)
	}
	var shas []string
	for i := len(mod.Commits) - 1; i >= 0; i-- {
		shas = append(shas, mod.Commits[i].SHA)
	}
	args := append([]string{"revert", "--no-commit"}, shas...)
	if _, err := g.run(args...); err != nil {
		return "", g.conflicts("revert", err)
	}
	return g.commit(CommitMessage(ModCommit{ID: id, Request: "undo " + id}, KindRevert))
}

// RevertAbort abandons a revert in progress.
func (g Git) RevertAbort() error { _, err := g.run("revert", "--abort"); return err }

// MergeFastForward fast-forwards the checked-out branch to rev; it fails if
// that is not a fast-forward.
func (g Git) MergeFastForward(rev string) error {
	_, err := g.run("merge", "--ff-only", "-q", rev)
	return err
}

// Commit is one commit with its parsed trailers.
type Commit struct {
	SHA      string
	Date     time.Time
	Subject  string
	Body     string
	Trailers Trailers
}

// Log lists commits in revRange (anything `git log` accepts), oldest first.
// grep, if set, limits to commits whose message matches the regex.
func (g Git) Log(revRange, grep string) ([]Commit, error) {
	args := []string{"log", "--reverse", "--format=%H%x00%aI%x00%B%x1e"}
	if grep != "" {
		args = append(args, "--grep="+grep)
	}
	args = append(args, revRange, "--")
	out, err := g.runRaw(nil, args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x00", 3)
		if len(parts) != 3 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, parts[1])
		body := strings.TrimRight(parts[2], "\n")
		subject, _, _ := strings.Cut(body, "\n")
		commits = append(commits, Commit{SHA: parts[0], Date: date, Subject: subject, Body: body, Trailers: ParseTrailers(body)})
	}
	return commits, nil
}

// Mod states.
const (
	ModActive = "active"
	ModUndone = "undone"
)

// Mod is one /mantle change, reconstructed from commit trailers.
type Mod struct {
	ID      string
	Request string
	// Kinds are the distinct Mantle-Kind values of its non-revert commits,
	// in commit order (e.g. core-seam, mod).
	Kinds []string
	// Commits are oldest first, including reverts.
	Commits []Commit
	// Date is the date of the newest commit.
	Date  time.Time
	State string
}

// Kind is a short description of the mod's commit kinds ("mod",
// "core-seam+mod").
func (m Mod) Kind() string { return strings.Join(m.Kinds, "+") }

// ListMods returns the mods with commits in revRange, in order of their
// first commit.
func (g Git) ListMods(revRange string) ([]Mod, error) {
	commits, err := g.Log(revRange, "^"+TrailerMod+":")
	if err != nil {
		return nil, err
	}
	return GroupMods(commits), nil
}

// GroupMods groups commits (oldest first) into mods by their Mantle-Mod
// trailer. A mod whose newest commit is a revert is undone.
func GroupMods(commits []Commit) []Mod {
	var mods []Mod
	index := map[string]int{}
	for _, c := range commits {
		id := c.Trailers.Get(TrailerMod)
		if id == "" {
			continue
		}
		i, ok := index[id]
		if !ok {
			i = len(mods)
			index[id] = i
			mods = append(mods, Mod{ID: id, State: ModActive})
		}
		m := &mods[i]
		m.Commits = append(m.Commits, c)
		m.Date = c.Date
		kind := c.Trailers.Get(TrailerKind)
		if kind == KindRevert {
			m.State = ModUndone
			continue
		}
		m.State = ModActive
		if m.Request == "" {
			m.Request = c.Trailers.Get(TrailerRequest)
		}
		if kind != "" && !slices.Contains(m.Kinds, kind) {
			m.Kinds = append(m.Kinds, kind)
		}
	}
	return mods
}

// Trailer is one "Key: value" line of a commit's trailer block.
type Trailer struct{ Key, Value string }

// Trailers is a commit's trailer block in order.
type Trailers []Trailer

// Get returns the last value for key (case-insensitive), or "".
func (ts Trailers) Get(key string) string {
	for i := len(ts) - 1; i >= 0; i-- {
		if strings.EqualFold(ts[i].Key, key) {
			return ts[i].Value
		}
	}
	return ""
}

// ParseTrailers parses the trailer block: the last paragraph of msg, if
// every non-continuation line in it is "Key: value". Continuation lines
// (starting with whitespace) are folded into the previous value.
func ParseTrailers(msg string) Trailers {
	msg = strings.TrimRight(strings.ReplaceAll(msg, "\r\n", "\n"), "\n ")
	paras := strings.Split(msg, "\n\n")
	if len(paras) < 2 {
		return nil
	}
	last := paras[len(paras)-1]
	var ts Trailers
	for _, line := range strings.Split(last, "\n") {
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(ts) > 0 {
			ts[len(ts)-1].Value += " " + strings.TrimSpace(line)
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return nil
		}
		ts = append(ts, Trailer{Key: k, Value: strings.TrimSpace(v)})
	}
	return ts
}

func splitNUL(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func nulJoin(files []string) []byte {
	return []byte(strings.Join(files, "\x00") + "\x00")
}
