package engine

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GitWorkspaceDiff is get_workspace_diff's fallback: the uncommitted changes in dir
// (tracked files against HEAD, plus untracked files), in the same shape.
func GitWorkspaceDiff(ctx context.Context, dir string) (WorkspaceDiff, error) {
	var d WorkspaceDiff
	d.FromGit = true
	d.Source = &DiffSource{Kind: "working-tree"}
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
	base := "HEAD"
	if _, err := git("rev-parse", "--verify", "-q", "HEAD"); err != nil {
		base = "4b825dc642cb6eb9a060e54bf8d69288fbee4904" // the empty tree: no commits yet
	}
	num, err := git("diff", "--numstat", base)
	if err != nil {
		return d, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(num)), "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		st := FileDiffStat{Path: f[2], IsBinary: f[0] == "-"}
		st.Added, _ = strconv.Atoi(f[0])
		st.Removed, _ = strconv.Atoi(f[1])
		d.PerFileStats = append(d.PerFileStats, st)
	}
	patch, err := git("diff", "--no-color", "-U3", base)
	if err != nil {
		return d, err
	}
	d.Hunks = parseUnified(patch)
	if un, err := git("ls-files", "--others", "--exclude-standard"); err == nil {
		for _, p := range strings.Split(strings.TrimSpace(string(un)), "\n") {
			if p != "" {
				d.PerFileStats = append(d.PerFileStats, FileDiffStat{Path: p, IsUntracked: true})
			}
		}
	}
	for _, s := range d.PerFileStats {
		d.Stats.FilesCount++
		d.Stats.LinesAdded += s.Added
		d.Stats.LinesRemoved += s.Removed
	}
	return d, nil
}

// parseUnified splits `git diff` output into per-file hunks.
func parseUnified(patch []byte) []FileDiffHunks {
	var out []FileDiffHunks
	var cur *FileDiffHunks
	var h *DiffHunk
	sc := bufio.NewScanner(bytes.NewReader(patch))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			out = append(out, FileDiffHunks{})
			cur, h = &out[len(out)-1], nil
			if i := strings.LastIndex(line, " b/"); i >= 0 {
				cur.Path = line[i+3:]
			}
		case cur == nil:
		case strings.HasPrefix(line, "+++ b/"):
			cur.Path = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "@@ "):
			var hk DiffHunk
			parseRange(line, &hk)
			cur.Hunks = append(cur.Hunks, hk)
			h = &cur.Hunks[len(cur.Hunks)-1]
		case h != nil && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ")):
			if !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
				h.Lines = append(h.Lines, line)
			}
		}
	}
	return out
}

// parseRange reads "@@ -a,b +c,d @@".
func parseRange(line string, h *DiffHunk) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return
	}
	h.OldStart, h.OldLines = rangePair(strings.TrimPrefix(f[1], "-"))
	h.NewStart, h.NewLines = rangePair(strings.TrimPrefix(f[2], "+"))
}

func rangePair(s string) (int, int) {
	a, b, ok := strings.Cut(s, ",")
	start, _ := strconv.Atoi(a)
	if !ok {
		return start, 1
	}
	n, _ := strconv.Atoi(b)
	return start, n
}
