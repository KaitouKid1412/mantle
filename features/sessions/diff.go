package sessions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
	"github.com/KaitouKid1412/mantle/pkg/render"
	"github.com/KaitouKid1412/mantle/pkg/theme"
	"github.com/KaitouKid1412/mantle/pkg/ui/diffview"
)

// DialogDiff is the /diff viewer: uncommitted changes (git), then the files each turn
// of this conversation edited.
const DialogDiff = "dialog.diff"

func (f *feature) registerDiff(r ext.Registrar) {
	r.AddCommand(ext.Command{
		ID: ext.CommandID("diff"), Name: "diff", Source: ext.SourceBuiltin,
		Description: "View uncommitted changes and what each turn changed",
		Run:         f.runDiff,
	})
	r.AddDialog(DialogDiff, func(ctx ext.Ctx, args any) (ext.Dialog, error) {
		return newDiffViewer(f, ctx), nil
	})
}

// diffFile is one file's changes.
type diffFile struct {
	path           string
	hunks          []diffview.Hunk
	added, removed int
	isNew, deleted bool
	binary         bool
}

// diffSource is one set of changes to browse.
type diffSource struct {
	label   string
	files   []diffFile
	err     error
	loading bool
}

// diffBases are what the uncommitted diff compares against (ctrl+x b cycles).
var diffBases = []string{"HEAD", "merge-base"}

type diffViewer struct {
	id      string // DialogDiff, or the fullscreen panel's component ID
	f       *feature
	cwd     string
	base    int
	sources []diffSource
	src     int
	file    int
	scroll  int
	noise   bool // show lockfiles and generated files
	gen     int
	height  int
}

type gitDiffMsg struct {
	gen   int
	files []diffFile
	label string
	err   error
}

func newDiffViewer(f *feature, ctx ext.Ctx) *diffViewer {
	d := &diffViewer{id: DialogDiff, f: f, cwd: f.cwd(ctx)}
	d.sources = append([]diffSource{{label: "Uncommitted changes", loading: true}}, turnSources(ctx.Transcript())...)
	return d
}

func (d *diffViewer) ID() string         { return d.id }
func (d *diffViewer) KeyContext() string { return ext.ContextDiffDialog }
func (d *diffViewer) KeyContexts() []string {
	return []string{ext.ContextDiffDialog, ext.ContextDiffPanel}
}
func (d *diffViewer) Placement() ext.Placement                          { return ext.PlaceAltScreen }
func (d *diffViewer) Init(ctx ext.Ctx) tea.Cmd                          { return d.loadGit() }
func (d *diffViewer) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

// loadGit diffs the working tree (tracked and untracked files) against the base.
func (d *diffViewer) loadGit() tea.Cmd {
	d.gen++
	d.sources[0].loading = true
	gen, cwd, base, to := d.gen, d.cwd, diffBases[d.base], d.id
	return func() tea.Msg {
		files, label, err := gitChanges(cwd, base)
		return ext.AddressedMsg{To: to, Msg: gitDiffMsg{gen: gen, files: files, label: label, err: err}}
	}
}

func gitChanges(cwd, base string) ([]diffFile, string, error) {
	if _, err := runGit(cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, "", fmt.Errorf("not a git repository")
	}
	ref, label := "HEAD", "Uncommitted changes"
	if base == "merge-base" {
		for _, up := range []string{"origin/HEAD", "origin/main", "main", "origin/master", "master"} {
			if out, err := runGit(cwd, "merge-base", "HEAD", up); err == nil {
				ref = strings.TrimSpace(string(out))
				label = "Changes since branching from " + up
				break
			}
		}
	}
	out, err := runGit(cwd, "diff", "--no-color", "--no-ext-diff", "-M", ref)
	if err != nil {
		// A repository without commits: everything is untracked.
		out = nil
	}
	files := parseUnifiedDiff(string(out))
	if raw, err := runGit(cwd, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		for _, p := range strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00") {
			if p != "" {
				files = append(files, untrackedFile(cwd, p))
			}
		}
	}
	return files, label, nil
}

// untrackedFile shows a new file as all additions (binary and huge files as a note).
func untrackedFile(cwd, p string) diffFile {
	df := diffFile{path: p, isNew: true}
	b, err := os.ReadFile(filepath.Join(cwd, p))
	if err != nil || len(b) > 512<<10 || bytes.IndexByte(b, 0) >= 0 {
		df.binary = true
		return df
	}
	df.hunks = diffview.FromStrings("", string(b), 0)
	df.added, df.removed = diffview.Counts(df.hunks)
	return df
}

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseUnifiedDiff reads `git diff` output into files and hunks.
func parseUnifiedDiff(s string) []diffFile {
	var files []diffFile
	var cur *diffFile
	var h *diffview.Hunk
	flush := func() {
		if cur == nil {
			return
		}
		if h != nil {
			cur.hunks = append(cur.hunks, *h)
			h = nil
		}
		cur.added, cur.removed = diffview.Counts(cur.hunks)
		files = append(files, *cur)
		cur = nil
	}
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &diffFile{path: gitPath(line)}
		case cur == nil:
		case strings.HasPrefix(line, "new file mode"):
			cur.isNew = true
		case strings.HasPrefix(line, "deleted file mode"):
			cur.deleted = true
		case strings.HasPrefix(line, "Binary files "):
			cur.binary = true
		case strings.HasPrefix(line, "rename to "):
			cur.path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "--- "):
			if h == nil {
				if p := strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/"); strings.HasPrefix(line, "+++ ") && p != "/dev/null" {
					cur.path = p
				}
				continue
			}
			h.Lines = append(h.Lines, line)
		case strings.HasPrefix(line, "@@"):
			if h != nil {
				cur.hunks = append(cur.hunks, *h)
			}
			h = &diffview.Hunk{}
			if m := hunkRE.FindStringSubmatch(line); m != nil {
				h.OldStart, _ = strconv.Atoi(m[1])
				h.OldLines = atoiDefault(m[2], 1)
				h.NewStart, _ = strconv.Atoi(m[3])
				h.NewLines = atoiDefault(m[4], 1)
			}
		case h != nil && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")):
			h.Lines = append(h.Lines, line)
		}
	}
	flush()
	return files
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, _ := strconv.Atoi(s)
	return n
}

// gitPath is the b/ path of a "diff --git a/x b/x" line.
func gitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return rest
}

// editResult is the structured result of the Edit, MultiEdit and Write tools.
type editResult struct {
	FilePath        string          `json:"filePath"`
	Type            string          `json:"type"`
	Content         string          `json:"content"`
	StructuredPatch []diffview.Hunk `json:"structuredPatch"`
}

// turnSources are the files each turn edited, newest turn first, from the Edit/Write
// tool results in the transcript.
func turnSources(t ext.Transcript) []diffSource {
	if t == nil {
		return nil
	}
	type turn struct {
		prompt string
		files  []diffFile
		index  map[string]int
	}
	var turns []*turn
	cur := &turn{prompt: "Before the first prompt", index: map[string]int{}}
	push := func() {
		if len(cur.files) > 0 {
			turns = append(turns, cur)
		}
	}
	for _, it := range t.Items() {
		if it.Key == ext.KeyUserPrompt && it.ParentID == "" {
			push()
			text := ""
			if u, ok := it.Data.(*proto.User); ok {
				text = oneLine(u.Message.Content.PlainText())
			}
			cur = &turn{prompt: text, index: map[string]int{}}
			continue
		}
		tu, ok := it.Data.(*proto.ToolUse)
		if !ok || it.Result == nil || it.Result.IsError || len(it.Result.Structured) == 0 {
			continue
		}
		switch tu.Name {
		case "Edit", "MultiEdit", "Write", "NotebookEdit":
		default:
			continue
		}
		var r editResult
		if json.Unmarshal(it.Result.Structured, &r) != nil || r.FilePath == "" {
			continue
		}
		hunks := r.StructuredPatch
		isNew := false
		if len(hunks) == 0 && r.Type == "create" {
			hunks, isNew = diffview.FromStrings("", r.Content, 0), true
		}
		if len(hunks) == 0 {
			continue
		}
		i, ok := cur.index[r.FilePath]
		if !ok {
			i = len(cur.files)
			cur.index[r.FilePath] = i
			cur.files = append(cur.files, diffFile{path: r.FilePath, isNew: isNew})
		}
		df := &cur.files[i]
		df.hunks = append(df.hunks, hunks...)
		df.added, df.removed = diffview.Counts(df.hunks)
	}
	push()
	out := make([]diffSource, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		out = append(out, diffSource{label: "Turn: " + truncateRunes(turns[i].prompt, 60), files: turns[i].files})
	}
	return out
}

// noisy files are hidden unless the noise filter is off.
var noisyNames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "Cargo.lock": true,
	"poetry.lock": true, "Gemfile.lock": true, "composer.lock": true, "bun.lockb": true, "uv.lock": true,
}

func isNoise(p string) bool {
	base := path.Base(filepath.ToSlash(p))
	return noisyNames[base] || strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") ||
		strings.HasSuffix(base, ".map") || strings.HasSuffix(base, ".snap")
}

// files are the current source's files after the noise filter.
func (d *diffViewer) files() []diffFile {
	if d.src >= len(d.sources) {
		return nil
	}
	var out []diffFile
	for _, f := range d.sources[d.src].files {
		if d.noise || !isNoise(f.path) {
			out = append(out, f)
		}
	}
	return out
}

func (d *diffViewer) Update(ctx ext.Ctx, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(gitDiffMsg); ok && m.gen == d.gen {
		s := &d.sources[0]
		s.loading, s.files, s.err = false, m.files, m.err
		if m.label != "" {
			s.label = m.label
		}
		ctx.Invalidate(d.id)
	}
	return nil
}

func (d *diffViewer) HandleAction(ctx ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	defer ctx.Invalidate(d.id)
	page := max(d.height-2, 1)
	switch a {
	case ext.ActDiffDismiss, ext.ActDiffBack, ext.ActAppInterrupt, ext.ActSelectCancel:
		return true, ctx.CloseDialog(DialogDiff)
	case ext.ActDiffNextSource:
		d.src, d.file, d.scroll = (d.src+1)%len(d.sources), 0, 0
	case ext.ActDiffPreviousSource:
		d.src, d.file, d.scroll = (d.src-1+len(d.sources))%len(d.sources), 0, 0
	case ext.ActDiffNextFile, ext.ActAppDiffFileListDown:
		d.file, d.scroll = min(d.file+1, max(len(d.files())-1, 0)), 0
	case ext.ActDiffPreviousFile, ext.ActAppDiffFileListUp:
		d.file, d.scroll = max(d.file-1, 0), 0
	case ext.ActScrollPageDown, ext.ActScrollFullPageDown:
		d.scroll += page
	case ext.ActScrollPageUp, ext.ActScrollFullPageUp:
		d.scroll = max(d.scroll-page, 0)
	case ext.ActScrollHalfPageDown:
		d.scroll += page / 2
	case ext.ActScrollHalfPageUp:
		d.scroll = max(d.scroll-page/2, 0)
	case ext.ActScrollLineDown:
		d.scroll++
	case ext.ActScrollLineUp:
		d.scroll = max(d.scroll-1, 0)
	case ext.ActScrollTop:
		d.scroll = 0
	case ext.ActScrollBottom:
		d.scroll = 1 << 30
	case ext.ActAppToggleDiffNoiseFilter:
		d.noise = !d.noise
		d.file = 0
	case ext.ActAppCycleDiffBase:
		d.base = (d.base + 1) % len(diffBases)
		d.src, d.file, d.scroll = 0, 0, 0
		return true, d.loadGit()
	default:
		return false, nil
	}
	return true, nil
}

func (d *diffViewer) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	actions := map[string]ext.ActionID{
		"esc": ext.ActDiffDismiss, "q": ext.ActDiffDismiss, "left": ext.ActDiffPreviousSource,
		"right": ext.ActDiffNextSource, "up": ext.ActDiffPreviousFile, "down": ext.ActDiffNextFile,
		"k": ext.ActDiffPreviousFile, "j": ext.ActDiffNextFile, "pgdown": ext.ActScrollPageDown,
		"pgup": ext.ActScrollPageUp, "space": ext.ActScrollFullPageDown, "b": ext.ActScrollFullPageUp,
		"g": ext.ActScrollTop, "G": ext.ActScrollBottom, "n": ext.ActAppToggleDiffNoiseFilter,
	}
	if a, ok := actions[k.String()]; ok {
		return d.HandleAction(ctx, a)
	}
	return true, nil
}

func (d *diffViewer) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	th := ctx.Theme()
	w := max(a.Width, 20)
	h := a.MaxHeight
	if h <= 0 {
		h = 30
	}
	src := d.sources[d.src]
	files := d.files()
	head := th.Fg(theme.Accent).Bold(true).Render("Diff") + th.Paint(theme.Inactive,
		fmt.Sprintf(" · %s (%d/%d)", src.label, d.src+1, len(d.sources)))
	lines := []string{fit(head, w)}

	footer := th.Paint(theme.Inactive, "←→ source · ↑↓ file · space/b page · n noise filter · ctrl+x b base · esc close")
	switch {
	case src.loading:
		lines = append(lines, "", th.Paint(theme.Inactive, "Reading changes…"))
	case src.err != nil:
		lines = append(lines, "", th.Paint(theme.Inactive, src.err.Error()))
	case len(files) == 0:
		msg := "No changes."
		if hidden := len(src.files) - len(files); hidden > 0 {
			msg = plural(hidden, "noisy file is", "noisy files are") + " hidden (n shows them)."
		}
		lines = append(lines, "", th.Paint(theme.Inactive, msg))
	default:
		d.file = min(d.file, len(files)-1)
		listRows := min(len(files), max(h/4, 3))
		start := max(0, min(d.file-listRows/2, len(files)-listRows))
		lines = append(lines, "")
		for i := start; i < start+listRows && i < len(files); i++ {
			fl := files[i]
			stat := th.Paint(theme.DiffAdded, fmt.Sprintf("+%d", fl.added)) + " " + th.Paint(theme.DiffRemoved, fmt.Sprintf("−%d", fl.removed))
			tag := ""
			switch {
			case fl.binary:
				tag = " (binary)"
			case fl.isNew:
				tag = " (new)"
			case fl.deleted:
				tag = " (deleted)"
			}
			name := shortPath(fl.path, d.cwd+"/")
			name = strings.TrimPrefix(name, "~")
			if i == d.file {
				lines = append(lines, fit(th.Paint(theme.Suggestion, "› ")+th.Fg(theme.Suggestion).Bold(true).Render(name)+th.Paint(theme.Inactive, tag)+"  "+stat, w))
			} else {
				lines = append(lines, fit("  "+name+th.Paint(theme.Inactive, tag)+"  "+stat, w))
			}
		}
		lines = append(lines, th.Paint(theme.Subtle, strings.Repeat("─", w)))
		body := max(h-len(lines)-2, 3)
		d.height = body
		fl := files[d.file]
		var diff []string
		if fl.binary {
			diff = []string{th.Paint(theme.Inactive, "Binary file")}
		} else {
			diff = diffview.Render(fl.hunks, diffview.Options{
				Width: w, Palette: render.PaletteFunc(func(tok string) color.Color { return th.Color(theme.Token(tok)) }),
				Filename: fl.path, NoHighlight: ext.ClaudeBool(ctx.Settings(), "syntaxHighlightingDisabled", false),
			})
		}
		d.scroll = min(d.scroll, max(len(diff)-body, 0))
		end := min(d.scroll+body, len(diff))
		lines = append(lines, diff[d.scroll:end]...)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, fit(footer, w))
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}
