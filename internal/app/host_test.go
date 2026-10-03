package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/ext/exttest"
)

func printCmd(text string) ext.CommandFunc {
	return func(c ext.Ctx, args string) tea.Cmd { c.Notify(ext.Notice{Key: "out", Text: text}); return nil }
}

func runCommand(t *testing.T, r *Root, name string) string {
	t.Helper()
	cmd, ok := r.Ctx().Command(name)
	if !ok {
		t.Fatalf("command %q not found", name)
	}
	exttest.Exec(cmd.Run(r.Ctx(), ""))
	for _, n := range r.Notices() {
		if n.Key == "out" {
			return n.Text
		}
	}
	return ""
}

func TestSetupOrder(t *testing.T) {
	var order []string
	f := func(id string, o int, after ...string) ext.Feature {
		return ext.Feature{ID: id, Order: o, After: after, Setup: func(ext.Registrar) error { order = append(order, id); return nil }}
	}
	NewHost([]ext.Feature{
		f("mod.z", 1000), f("b.second", 10, "c.first"), f("c.first", 10), f("a.early", 5), f("mod.a", 1000, "mod.z"),
	}, HostOptions{})
	got := strings.Join(order, ",")
	if got != "a.early,c.first,b.second,mod.z,mod.a" {
		t.Fatalf("setup order = %s", got)
	}
}

func TestSafeModeSkipsMods(t *testing.T) {
	ran := map[string]bool{}
	f := func(id string, o int) ext.Feature {
		return ext.Feature{ID: id, Order: o, Setup: func(ext.Registrar) error { ran[id] = true; return nil }}
	}
	h := NewHost([]ext.Feature{f("chrome.x", 10), f("mod.y", 1000)}, HostOptions{Safe: true})
	if !ran["chrome.x"] || ran["mod.y"] {
		t.Fatalf("ran = %v", ran)
	}
	if h.Skipped()["mod.y"] != "safe mode" {
		t.Fatalf("skipped = %v", h.Skipped())
	}
}

func TestOverrides(t *testing.T) {
	base := ext.Feature{ID: "demo.base", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "hello", Description: "base", Run: printCmd("base")})
		r.AddCommand(ext.Command{Name: "bye", Run: printCmd("bye")})
		r.AddRenderer("tool.Bash", func(rc ext.RenderCtx, it *ext.Item) ext.Block { return ext.Block{Lines: []string{"bash"}} })
		r.AddComponent(ext.SlotStatus, &box{id: "demo.box", text: "BOX"}, ext.SlotOpts{})
		r.AddComponent(ext.SlotStatus, &box{id: "demo.other", text: "OTHER"}, ext.SlotOpts{})
		return nil
	}}
	mod1 := ext.Feature{ID: "mod.one", Order: 1000, Setup: func(r ext.Registrar) error {
		r.Alias("cmd.greet", "cmd.hello")
		r.Replace("cmd.greet", ext.Command{Description: "one", Run: printCmd("one")})
		r.Wrap("render.tool.Bash", func(next ext.Renderer) ext.Renderer {
			return func(rc ext.RenderCtx, it *ext.Item) ext.Block {
				b := next(rc, it)
				b.Lines = append([]string{"wrapped:"}, b.Lines...)
				return b
			}
		})
		r.Remove("demo.box")
		r.Wrap("cmd.bye", func(next ext.CommandFunc) ext.CommandFunc {
			return func(c ext.Ctx, a string) tea.Cmd { c.Notify(ext.Notice{Key: "trace", Text: "one"}); return next(c, a) }
		})
		return nil
	}}
	mod2 := ext.Feature{ID: "mod.two", Order: 1001, Setup: func(r ext.Registrar) error {
		r.Replace("cmd.hello", ext.Command{Description: "two", Run: printCmd("two")})
		r.Replace("demo.other", &box{id: "demo.other", text: "REPLACED"})
		r.Replace("cmd.nothing", ext.Command{})
		r.Replace("render.tool.Bash", ext.Command{}) // wrong kind
		return nil
	}}
	// Registration order must not matter: mods first.
	h := NewHost([]ext.Feature{mod2, mod1, base}, HostOptions{})
	r := New(Options{Host: h, NoBackgroundQuery: true})
	r.Update(tea.WindowSizeMsg{Width: 40, Height: 10})

	if got := runCommand(t, r, "hello"); got != "two" {
		t.Errorf("hello ran %q, want two (higher Order wins)", got)
	}
	if c, _ := r.Ctx().Command("hello"); c.Name != "hello" || c.ID != "cmd.hello" {
		t.Errorf("replaced command lost name/ID: %+v", c)
	}
	b := r.Ctx().Renderer("tool.Bash")(ext.RenderCtx{Width: 40}, &ext.Item{Key: "tool.Bash"})
	if strings.Join(b.Lines, "|") != "wrapped:|bash" {
		t.Errorf("wrapped renderer = %v", b.Lines)
	}
	view := r.View().Content
	if strings.Contains(view, "BOX") || !strings.Contains(view, "REPLACED") {
		t.Errorf("component overrides not applied:\n%s", view)
	}
	runCommand(t, r, "bye")
	traced := false
	for _, n := range r.Notices() {
		traced = traced || n.Key == "trace"
	}
	if !traced {
		t.Error("command wrap not applied")
	}

	reports := ""
	for _, rep := range h.Reports() {
		reports += rep.String() + "\n"
	}
	for _, want := range []string{
		"mod.two: cmd.hello: override conflict: mod.one's replace loses to mod.two's replace",
		"cmd.nothing: replace: no such ID",
		"render.tool.Bash: replace: ext.Command does not match",
	} {
		if !strings.Contains(reports, want) {
			t.Errorf("missing report %q in:\n%s", want, reports)
		}
	}
}

func TestDuplicateRegistrationHigherOrderWins(t *testing.T) {
	a := ext.Feature{ID: "a.cmd", Order: 10, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "x", Run: printCmd("a")})
		return nil
	}}
	m := ext.Feature{ID: "mod.cmd", Order: 1000, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "x", Run: printCmd("mod")})
		return nil
	}}
	h := NewHost([]ext.Feature{m, a}, HostOptions{})
	r := New(Options{Host: h, NoBackgroundQuery: true})
	if got := runCommand(t, r, "x"); got != "mod" {
		t.Fatalf("x ran %q", got)
	}
	if c, _ := r.Ctx().Command("x"); c.Source != ext.SourceMod {
		t.Fatalf("source = %q", c.Source)
	}
}

func TestSetupErrorDropsRegistrations(t *testing.T) {
	bad := ext.Feature{ID: "bad.setup", Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "half"})
		panic("setup exploded")
	}}
	h := NewHost([]ext.Feature{bad}, HostOptions{})
	if len(h.live(KindCommand)) != 0 {
		t.Fatal("registrations of a failed Setup must be dropped")
	}
	if len(h.Reports()) != 1 || !h.Reports()[0].Fatal || !strings.Contains(h.Reports()[0].Message, "setup exploded") {
		t.Fatalf("reports = %v", h.Reports())
	}
}

func TestCatalog(t *testing.T) {
	f := ext.Feature{ID: "demo.cat", Order: 10, Parity: []string{"CORE-99"}, Setup: func(r ext.Registrar) error {
		r.AddCommand(ext.Command{Name: "cat", Description: "meow"})
		r.AddStory(ext.Story{ID: "demo.cat/default"})
		return nil
	}}
	h := NewHost([]ext.Feature{f}, HostOptions{})
	var cmd *CatalogEntry
	for _, e := range h.Catalog() {
		if e.Kind == KindCommand && e.ID == "cmd.cat" {
			cmd = &e
		}
	}
	if cmd == nil {
		t.Fatal("cmd.cat missing from catalog")
	}
	if cmd.Feature != "demo.cat" || cmd.Description != "meow" || cmd.Parity[0] != "CORE-99" || !strings.HasPrefix(cmd.File, "internal/app/host_test.go:") {
		t.Fatalf("catalog entry = %+v", *cmd)
	}
}
