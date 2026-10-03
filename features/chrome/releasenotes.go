package chrome

import (
	_ "embed"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/term/terminal"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// ReleaseNotesID names the release-notes feature and its state store.
const ReleaseNotesID = "chrome.releaseNotes"

//go:embed mantle_changelog.md
var mantleChangelog string

const (
	seenClaudeKey = "claudeSeen"
	seenMantleKey = "mantleSeen"
	// whatsNewItems caps the startup "what's new" block per version.
	whatsNewItems = 6
	// whatsNewVersions caps how many versions the startup block covers.
	whatsNewVersions = 3
)

// releaseNotes shows Claude Code's changelog (read at runtime from the engine's cache)
// and mantle's own: a short "what's new" block when the engine version changed, and
// the full notes on /release-notes.
type releaseNotes struct {
	env     terminal.Env
	read    func(path string) ([]byte, error)
	mantle  string
	checked bool
}

func newReleaseNotes(env terminal.Env) *releaseNotes {
	return &releaseNotes{env: env, read: os.ReadFile, mantle: mantleChangelog}
}

func (r *releaseNotes) claudeNotes() []ReleaseNotes {
	path := ClaudeChangelogPath(r.env)
	if path == "" {
		return nil
	}
	data, err := r.read(path)
	if err != nil {
		return nil
	}
	return ParseChangelog(string(data))
}

// onSession prints "what's new" once after the engine version moved past the last one
// seen. The first run only records the version.
func (r *releaseNotes) onSession(ctx ext.Ctx, m ext.SessionChangedMsg) tea.Cmd {
	if r.checked || !isMain(m.EngineID) || m.Info.ClaudeVersion == "" {
		return nil
	}
	r.checked = true
	current := m.Info.ClaudeVersion
	kv := ctx.Store(ReleaseNotesID)
	var seen string
	if ok, _ := kv.Get(seenClaudeKey, &seen); !ok || seen == "" {
		_ = kv.Set(seenClaudeKey, current)
		return nil
	}
	if seen == current || terminal.VersionAtLeast(seen, current) {
		return nil
	}
	_ = kv.Set(seenClaudeKey, current)
	var notes []ReleaseNotes
	for _, n := range NotesSince(r.claudeNotes(), seen, 0) {
		if terminal.VersionAtLeast(current, n.Version) {
			notes = append(notes, n)
		}
	}
	if len(notes) == 0 {
		return nil
	}
	if len(notes) > whatsNewVersions {
		notes = notes[:whatsNewVersions]
	}
	block := renderNotes(ctx, "What's new in Claude Code", notes, whatsNewItems, termWidth(ctx))
	return ctx.Print(block + "\n" + dim(ctx.Theme(), ctx.Accessibility().ScreenReader, "  /release-notes for the full list"))
}

// command is /release-notes: notes newer than the last time they were shown (or the
// latest few), for Claude Code and mantle.
func (r *releaseNotes) command(ctx ext.Ctx, _ string) tea.Cmd {
	kv := ctx.Store(ReleaseNotesID)
	w := termWidth(ctx)
	var blocks []string
	sections := []struct {
		title, key string
		notes      []ReleaseNotes
	}{
		{"Claude Code", seenClaudeKey + ".shown", r.claudeNotes()},
		{"mantle", seenMantleKey, ParseChangelog(r.mantle)},
	}
	for _, s := range sections {
		if len(s.notes) == 0 {
			continue
		}
		var seen string
		_, _ = kv.Get(s.key, &seen)
		show := NotesSince(s.notes, seen, 0)
		if len(show) == 0 {
			show = s.notes[:min(whatsNewVersions, len(s.notes))]
		}
		blocks = append(blocks, renderNotes(ctx, s.title, show, 0, w))
		_ = kv.Set(s.key, LatestVersion(s.notes))
	}
	if len(blocks) == 0 {
		return ctx.Notify(ext.Notice{Key: "chrome:release-notes", Text: "No release notes found",
			Level: ext.NoticeInfo, Source: ReleaseNotesID})
	}
	return ctx.Print(strings.Join(blocks, "\n\n"))
}

// renderNotes formats changelog sections, wrapped to w. maxItems 0 = all.
func renderNotes(ctx ext.Ctx, product string, notes []ReleaseNotes, maxItems, w int) string {
	t, sr := ctx.Theme(), ctx.Accessibility().ScreenReader
	w = max(20, w)
	var lines []string
	for i, n := range notes {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, paint(t, sr, theme.Text, product+" "+n.Version))
		items := 0
		for _, l := range n.Lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if maxItems > 0 && items == maxItems {
				lines = append(lines, dim(t, sr, "  …"))
				break
			}
			items++
			text, bullet := l, "  "
			if rest, ok := strings.CutPrefix(l, "- "); ok {
				text, bullet = rest, "  • "
			} else if rest, ok := strings.CutPrefix(l, "* "); ok {
				text, bullet = rest, "  • "
			}
			wrapped := strings.Split(ansi.Wrap(text, w-len(bullet)-1, ""), "\n")
			for j, wl := range wrapped {
				prefix := bullet
				if j > 0 {
					prefix = strings.Repeat(" ", ansi.StringWidth(bullet))
				}
				lines = append(lines, prefix+wl)
			}
		}
	}
	return strings.Join(lines, "\n")
}
