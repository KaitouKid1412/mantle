package selfmod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// PreviewDialogID is the before/after review shown before installing a build
// whose stories look different (selfmod.confirm).
const PreviewDialogID = "dialog.selfmod.preview"

// PreviewWidth is the width stories are compared at.
const PreviewWidth = 100

// StoryDiff is one story that renders differently in the candidate build.
type StoryDiff struct {
	ID     string
	Width  int
	Before string
	After  string
	// New is set when the current build has no such story.
	New bool
}

// DiffStories renders every story of the candidate (after) with both
// binaries and returns those that differ. before may be "" (no current
// build): every story is then new.
func DiffStories(ctx context.Context, before, after string) ([]StoryDiff, error) {
	if after == "" {
		return nil, errors.New("no candidate binary")
	}
	ids, err := storyIDs(ctx, after)
	if err != nil {
		return nil, err
	}
	var out []StoryDiff
	for _, id := range ids {
		a, err := renderStory(ctx, after, id, PreviewWidth)
		if err != nil {
			a = "(story failed: " + err.Error() + ")"
		}
		d := StoryDiff{ID: id, Width: PreviewWidth, After: a}
		if before == "" {
			d.New = true
		} else if b, err := renderStory(ctx, before, id, PreviewWidth); err != nil {
			d.New = true
		} else {
			d.Before = b
		}
		if d.New || d.Before != d.After {
			out = append(out, d)
		}
	}
	return out, nil
}

func storyEnv() []string {
	return append(os.Environ(), "ANTHROPIC_BASE_URL=http://127.0.0.1:9", "NO_COLOR=")
}

// storyIDs lists the story ids of a mantle-ui binary: `story --list` (one id
// per line), else the entries of kind "story" in `catalog --json`.
func storyIDs(ctx context.Context, bin string) ([]string, error) {
	list := exec.CommandContext(ctx, bin, "story", "--list")
	list.Env = storyEnv()
	if out, err := list.Output(); err == nil {
		var ids []string
		for _, ln := range strings.Split(string(out), "\n") {
			if ln = strings.TrimSpace(ln); ln != "" {
				ids = append(ids, ln)
			}
		}
		slices.Sort(ids)
		return slices.Compact(ids), nil
	}
	cmd := exec.CommandContext(ctx, bin, "catalog", "--json", "--kind", "story")
	cmd.Env = storyEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s catalog --json: %w", bin, err)
	}
	var v any
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, fmt.Errorf("catalog --json: %w", err)
	}
	var ids []string
	collectStories(v, false, &ids)
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func collectStories(v any, underStories bool, ids *[]string) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collectStories(e, underStories, ids)
		}
	case map[string]any:
		kind, _ := x["kind"].(string)
		if kind == "" {
			kind, _ = x["type"].(string)
		}
		if id, ok := x["id"].(string); ok && (kind == "story" || underStories) {
			*ids = append(*ids, id)
			return
		}
		for k, e := range x {
			collectStories(e, underStories || k == "stories", ids)
		}
	case string:
		if underStories {
			*ids = append(*ids, x)
		}
	}
}

func renderStory(ctx context.Context, bin, id string, width int) (string, error) {
	cmd := exec.CommandContext(ctx, bin, "story", id, "--width", strconv.Itoa(width))
	cmd.Env = storyEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, firstLine(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// previewArgs opens the dialog.
type previewArgs struct {
	ID, Request string
	Diffs       []StoryDiff
	Note        string
}

// previewResult is the dialog's answer.
type previewResult struct {
	ID     string
	Accept bool
}

type previewDialog struct {
	args   previewArgs
	scroll int
	accept bool
}

func (c *controller) newPreviewDialog(_ ext.Ctx, a any) (ext.Dialog, error) {
	pa, ok := a.(previewArgs)
	if !ok {
		return nil, fmt.Errorf("selfmod preview: unexpected args %T", a)
	}
	return &previewDialog{args: pa}, nil
}

func (d *previewDialog) ID() string                      { return "selfmod.preview." + d.args.ID }
func (d *previewDialog) Init(ext.Ctx) tea.Cmd            { return nil }
func (d *previewDialog) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (d *previewDialog) KeyContext() string              { return ext.ContextConfirmation }
func (d *previewDialog) Placement() ext.Placement        { return ext.PlaceAltScreen }
func (d *previewDialog) Result() any                     { return previewResult{ID: d.args.ID, Accept: d.accept} }

func (d *previewDialog) HandlePaste(ext.Ctx, tea.PasteMsg) (bool, tea.Cmd) { return true, nil }

func (d *previewDialog) HandleKey(ctx ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k.String() {
	case "y", "enter":
		d.accept = true
		return true, ctx.CloseDialog(d.ID())
	case "n", "esc", "q":
		d.accept = false
		return true, ctx.CloseDialog(d.ID())
	case "up", "k":
		d.scroll = max(0, d.scroll-1)
	case "down", "j":
		d.scroll++
	case "pgup", "b":
		d.scroll = max(0, d.scroll-10)
	case "pgdown", "space", "f":
		d.scroll += 10
	case "home", "g":
		d.scroll = 0
	default:
		return true, nil
	}
	ctx.Invalidate(d.ID())
	return true, nil
}

func (d *previewDialog) View(ctx ext.Ctx, a ext.Area) ext.Rendered {
	return ext.Rendered{Text: strings.Join(d.lines(ctx.Theme(), a.Width, a.MaxHeight), "\n")}
}

// lines renders the review: a header, the before/after columns of each
// changed story, and the key hints, scrolled to fit height.
func (d *previewDialog) lines(th *theme.Theme, width, height int) []string {
	width = max(width, 40)
	title := fmt.Sprintf("Review /mantle %s before installing", d.args.ID)
	head := []string{th.Paint(theme.Permission, title)}
	if d.args.Request != "" {
		head = append(head, th.Paint(theme.Inactive, ansi.Truncate("request: "+d.args.Request, width, "…")))
	}
	if d.args.Note != "" {
		head = append(head, th.Paint(theme.Warning, ansi.Truncate(d.args.Note, width, "…")))
	}
	switch n := len(d.args.Diffs); n {
	case 0:
		head = append(head, "No story looks different.")
	case 1:
		head = append(head, "1 story looks different:")
	default:
		head = append(head, fmt.Sprintf("%d stories look different:", n))
	}
	var body []string
	col := (width - 3) / 2
	for _, diff := range d.args.Diffs {
		label := fmt.Sprintf("── %s (width %d)", diff.ID, diff.Width)
		if diff.New {
			label += " · new"
		}
		body = append(body, th.Paint(theme.Subtle, ansi.Truncate(label, width, "…")))
		body = append(body, pad(th.Paint(theme.Inactive, "before"), col)+" │ "+th.Paint(theme.Inactive, "after"))
		before := strings.Split(diff.Before, "\n")
		if diff.New {
			before = []string{th.Paint(theme.Subtle, "(none)")}
		}
		after := strings.Split(diff.After, "\n")
		for i := range max(len(before), len(after)) {
			l, r := "", ""
			if i < len(before) {
				l = before[i]
			}
			if i < len(after) {
				r = after[i]
			}
			body = append(body, pad(ansi.Truncate(l, col, "…"), col)+" │ "+ansi.Truncate(r, col, "…"))
		}
	}
	foot := []string{"", th.Paint(theme.Inactive, "y install · n keep without installing · ↑↓ scroll")}
	room := len(body)
	if height > 0 {
		room = max(1, height-len(head)-len(foot))
	}
	d.scroll = min(d.scroll, max(0, len(body)-room))
	end := min(len(body), d.scroll+room)
	out := append(head, body[d.scroll:end]...)
	return append(out, foot...)
}

func pad(s string, w int) string {
	if n := ansi.StringWidth(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}
