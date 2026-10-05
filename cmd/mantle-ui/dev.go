package main

// mantle-ui's developer subcommands: the /mantle builder's map (catalog) and eyes
// (story), and the pipeline's self-test.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/keymap"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// devHost sets up every linked feature without starting a program or an engine.
func devHost() *app.Host {
	return app.NewHost(ext.Pending(), app.HostOptions{
		Safe: os.Getenv(ext.EnvSafe) == "1",
		Core: app.CoreFeatures(),
	})
}

func cmdStory(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("story", flag.ContinueOnError)
	fs.SetOutput(stderr)
	width := fs.Int("width", 100, "render width in cells")
	height := fs.Int("height", 40, "terminal height the story sees")
	withANSI := fs.Bool("ansi", false, "keep colours and styles")
	list := fs.Bool("list", false, "list story IDs")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: mantle-ui story <id> [--width N] [--ansi]   |   mantle-ui story --list")
		fs.PrintDefaults()
	}
	id, err := parseInterleaved(fs, args)
	if err != nil {
		return ext.ExitUsage
	}
	h := devHost()
	if *list || id == "" {
		for _, s := range h.Stories() {
			fmt.Fprintln(stdout, s.ID)
		}
		if id == "" && !*list {
			return ext.ExitUsage
		}
		return 0
	}
	s, ok := h.Story(id)
	if !ok {
		fmt.Fprintf(stderr, "mantle-ui: no story %q (try --list)\n", id)
		return 1
	}
	root := app.StoryRoot(h, *width, *height)
	out, err := testkit.RenderStoryCtx(root.Ctx(), s, *width)
	if !*withANSI {
		out = ansi.Strip(out)
	}
	fmt.Fprintln(stdout, out)
	if err != nil {
		fmt.Fprintln(stderr, "mantle-ui:", err)
		return 1
	}
	return 0
}

// parseInterleaved parses flags anywhere among the arguments and returns the first
// positional one, so `story ui.select/numbered --width 60` and
// `story --width 60 ui.select/numbered --ansi` both work.
func parseInterleaved(fs *flag.FlagSet, args []string) (string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) == 0 {
		return "", nil
	}
	return pos[0], nil
}

type catalogOut struct {
	APIVersion   int                `json:"apiVersion"`
	Entries      []app.CatalogEntry `json:"entries"`
	Bindings     []keymap.Binding   `json:"bindings"`
	Reports      []app.Report       `json:"reports,omitempty"`
	KeymapIssues []keymap.Issue     `json:"keymapIssues,omitempty"`
	Skipped      map[string]string  `json:"skipped,omitempty"`
	Themes       []string           `json:"themes"`
	Settings     []ext.SettingSpec  `json:"settings,omitempty"`
}

func cmdCatalog(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	kind := fs.String("kind", "", "only this kind (command, action, renderer, component, dialog, story, …)")
	if err := fs.Parse(args); err != nil {
		return ext.ExitUsage
	}
	h := devHost()
	root := app.StoryRoot(h, 100, 40)
	out := catalogOut{
		APIVersion:   ext.APIVersion,
		Entries:      h.Catalog(),
		Bindings:     root.Keymap().All(),
		Reports:      h.Reports(),
		KeymapIssues: root.Keymap().Issues,
		Skipped:      h.Skipped(),
		Settings:     h.Settings(),
	}
	for name := range h.Themes() {
		out.Themes = append(out.Themes, name)
	}
	sort.Strings(out.Themes)
	if *kind != "" {
		var es []app.CatalogEntry
		for _, e := range out.Entries {
			if string(e.Kind) == *kind {
				es = append(es, e)
			}
		}
		out.Entries = es
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "mantle-ui:", err)
			return 1
		}
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tID\tFEATURE\tFILE\tPARITY")
	for _, e := range out.Entries {
		id := e.ID
		if e.Removed {
			id += " (overridden)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.Kind, id, e.Feature, e.File, strings.Join(e.Parity, ","))
	}
	tw.Flush()
	for _, r := range out.Reports {
		fmt.Fprintln(stdout, "report:", r)
	}
	for _, i := range out.KeymapIssues {
		fmt.Fprintln(stdout, "keymap:", i)
	}
	return 0
}

// selftest checks that the registry resolves and every story renders at 60, 100 and
// 160 columns without panicking and within its width (pipeline step 8).
func cmdSelftest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("selftest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	verbose := fs.Bool("v", false, "list every check")
	if err := fs.Parse(args); err != nil {
		return ext.ExitUsage
	}
	failures := 0
	fail := func(format string, a ...any) {
		failures++
		fmt.Fprintf(stdout, "FAIL "+format+"\n", a...)
	}
	ok := func(format string, a ...any) {
		if *verbose {
			fmt.Fprintf(stdout, "ok   "+format+"\n", a...)
		}
	}

	h := devHost()
	for _, r := range h.Reports() {
		if r.Fatal {
			fail("setup: %s", r)
		} else {
			fmt.Fprintf(stdout, "warn setup: %s\n", r)
		}
	}
	for id, why := range h.Skipped() {
		if why != "safe mode" {
			fail("feature %s skipped: %s", id, why)
		}
	}
	root := app.StoryRoot(h, 100, 40)
	for _, i := range root.Keymap().Issues {
		if i.Severity == keymap.SevError && (i.Source == "defaults" || i.Source == "features") {
			fail("keymap: %s", i)
		}
	}
	for name, t := range h.Themes() {
		if m := t.Missing(); len(m) > 0 && !strings.HasPrefix(name, theme.CustomPrefix) {
			fail("theme %s misses tokens %v", name, m)
		}
	}
	stories := h.Stories()
	for _, s := range stories {
		for _, w := range []int{60, 100, 160} {
			r := app.StoryRoot(h, w, 40)
			if _, err := testkit.RenderStoryCtx(r.Ctx(), s, w); err != nil {
				fail("%v", err)
				continue
			}
			ok("story %s at %d", s.ID, w)
		}
	}
	// Rendering the full frame must not panic either.
	for _, w := range []int{60, 100, 160} {
		r := app.StoryRoot(h, w, 40)
		func() {
			defer func() {
				if v := recover(); v != nil {
					fail("root view at %d panicked: %v", w, v)
				}
			}()
			r.View()
		}()
	}
	if failures > 0 {
		fmt.Fprintf(stdout, "selftest: %d failure(s)\n", failures)
		return 1
	}
	fmt.Fprintf(stdout, "selftest: ok (%d features, %d stories × 3 widths)\n", len(h.Catalog()), len(stories))
	return 0
}
