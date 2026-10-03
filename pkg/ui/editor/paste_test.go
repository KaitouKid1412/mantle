package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ui/editor/history"
)

const fixtureDir = "../../../testdata/fixtures/04"

func TestPasteInlineAndCollapse(t *testing.T) {
	e := newTestEditor()
	if r, _ := e.InsertPaste("short\ntext"); r != PastedInline {
		t.Fatalf("2 lines stay inline, got %v", r)
	}
	if e.Display() != "short\ntext" {
		t.Fatalf("inline: %q", e.Display())
	}
	e.Undo()
	if !e.Empty() {
		t.Fatal("paste undoes in one step")
	}

	if r, _ := e.InsertPaste("a\nb\nc\nd"); r != PastedChip {
		t.Fatal("4 lines collapse")
	}
	if e.Display() != "[Pasted text #1 +3 lines]" || e.Text() != "a\nb\nc\nd" {
		t.Fatalf("chip: %q / %q", e.Display(), e.Text())
	}
	e.Clear()
	if r, _ := e.InsertPaste(strings.Repeat("x", 801)); r != PastedChip {
		t.Fatal("801 chars collapse")
	}
	if e.Display() != "[Pasted text #2]" {
		t.Fatalf("single-line chip: %q", e.Display())
	}
	e.Clear()
	if r, _ := e.InsertPaste(strings.Repeat("x", 800)); r != PastedInline {
		t.Fatal("800 chars stay inline")
	}
}

func TestPasteMessages(t *testing.T) {
	e := newTestEditor()
	e.HandlePasteMsg(tea.PasteStartMsg{})
	e.HandlePasteMsg(tea.PasteMsg{Content: "1\n2\n"})
	e.HandlePasteMsg(tea.PasteMsg{Content: "3\n4"})
	if !e.Empty() {
		t.Fatal("nothing inserted before PasteEnd")
	}
	e.HandlePasteMsg(tea.PasteEndMsg{})
	if e.Text() != "1\n2\n3\n4" || len(e.Chips()) != 1 {
		t.Fatalf("accumulated paste: %q", e.Text())
	}
	// A bare PasteMsg is processed at once.
	e2 := newTestEditor()
	e2.HandlePasteMsg(tea.PasteMsg{Content: "hi"})
	if e2.Display() != "hi" {
		t.Fatal(e2.Display())
	}
}

func TestPasteSanitizes(t *testing.T) {
	got := SanitizePaste("a\r\nb\rc\x1b[31mred\x1b[0m\x07\x00\u009b2J\td")
	if got != "a\nb\ncred\td" {
		t.Fatalf("%q", got)
	}
	e := newTestEditor()
	e.InsertPaste("x\x1b]0;title\x07y")
	if e.Display() != "xy" {
		t.Fatalf("OSC stripped: %q", e.Display())
	}
}

func TestPasteStore(t *testing.T) {
	dir := t.TempDir()
	e := newTestEditor()
	e.Paste.Store = DirStore{Dir: dir}
	small := "a\nb\nc\nd"
	if _, cmd := e.InsertPaste(small); cmd != nil {
		t.Fatal("short pastes are not stored")
	}
	big := strings.Repeat("0123456789\n", 200)
	_, cmd := e.InsertPaste(big)
	if cmd == nil {
		t.Fatal("long paste must be stored")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("store: %v", msg)
	}
	ch := e.Chips()[1]
	if ch.Hash != PasteHash(big) || len(ch.Hash) != 16 {
		t.Fatalf("hash %q", ch.Hash)
	}
	b, err := os.ReadFile(filepath.Join(dir, ch.Hash+".txt"))
	if err != nil || string(b) != big {
		t.Fatal("cache file", err)
	}
	if got, _ := (DirStore{Dir: dir}).Get(ch.Hash); got != big {
		t.Fatal("get")
	}
	if _, err := (DirStore{Dir: dir}).Get("../x"); err == nil {
		t.Fatal("path traversal")
	}
}

// A 1 MB paste collapses quickly and never renders in full.
func TestHugePaste(t *testing.T) {
	big := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 1<<20/44+1)
	e := newTestEditor()
	e.Paste.Store = DirStore{Dir: t.TempDir()}
	start := time.Now()
	r, _ := e.InsertPaste(big)
	_ = e.View()
	d := time.Since(start)
	if r != PastedChip {
		t.Fatal("1 MB paste must collapse")
	}
	if d > 50*time.Millisecond {
		t.Fatalf("1 MB paste took %v", d)
	}
	if len(e.View()) > 200 {
		t.Fatal("render must show only the chip")
	}
	if e.Text() != SanitizePaste(big) {
		t.Fatal("submit text keeps the full paste")
	}
}

func TestChipsAreAtomic(t *testing.T) {
	e := newTestEditor()
	typeText(e, "see ")
	e.InsertPaste("1\n2\n3\n4")
	typeText(e, " ok")
	if e.Display() != "see [Pasted text #1 +3 lines] ok" {
		t.Fatal(e.Display())
	}
	// Cursor steps over the chip in one move.
	e.LineEnd()
	for i := 0; i < 3; i++ {
		e.CharBackward()
	}
	if c, _, _ := e.at(e.cur); c.isChip() {
		t.Fatal("cursor should be after the chip")
	}
	e.CharBackward()
	if c, _, _ := e.at(e.cur); !c.isChip() {
		t.Fatal("one left lands on the chip")
	}
	e.DeleteForward()
	if e.Display() != "see  ok" {
		t.Fatalf("chip deleted whole: %q", e.Display())
	}
	e.Undo()
	e.LineEnd()
	e.KillWordBackward()
	e.KillWordBackward()
	if e.Display() != "see " {
		t.Fatalf("ctrl+w kills the chip as a word: %q", e.Display())
	}
	e.Yank()
	if len(e.Chips()) != 1 || e.Text() != "see 1\n2\n3\n4 ok" {
		t.Fatalf("yank restores the chip: %q", e.Text())
	}
}

func TestExpandChip(t *testing.T) {
	e := newTestEditor()
	e.InsertPaste("1\n2\n3\n4")
	if !e.ExpandChip() {
		t.Fatal("expand")
	}
	if e.Display() != "1\n2\n3\n4" || len(e.Chips()) != 0 {
		t.Fatal(e.Display())
	}
}

func TestAttachments(t *testing.T) {
	e := newTestEditor()
	e.InsertImage(&Image{MediaType: "image/png", Data: []byte{1}})
	typeText(e, "and")
	e.InsertPaste("a\nb\nc\nd")
	if e.Display() != "[Image #1] and[Pasted text #2 +3 lines]" {
		t.Fatalf("%q", e.Display())
	}
	if !e.EnterAttachments() || e.SelectedAttachment().ID != 2 {
		t.Fatal("enter selects the last chip")
	}
	e.HandleKey(key("left"))
	if e.SelectedAttachment().ID != 1 {
		t.Fatal("left selects the previous chip")
	}
	e.HandleKey(key("backspace"))
	if len(e.Chips()) != 1 || e.SelectedAttachment().ID != 2 {
		t.Fatal("remove keeps navigating")
	}
	e.HandleKey(key("delete"))
	if e.InAttachments() || len(e.Chips()) != 0 {
		t.Fatal("removing the last chip exits")
	}
	e.InsertImage(&Image{})
	e.EnterAttachments()
	e.HandleKey(key("esc"))
	if e.InAttachments() {
		t.Fatal("esc exits")
	}
	e.EnterAttachments()
	e.HandleKey(key("x"))
	if e.InAttachments() || !strings.HasSuffix(e.Display(), "x") {
		t.Fatal("typing exits and types")
	}
}

func TestDroppedImagePaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "shot one.png")
	b := filepath.Join(dir, "b.JPG")
	for _, p := range []string{a, b} {
		os.WriteFile(p, []byte("x"), 0o600)
	}
	esc := strings.ReplaceAll(a, " ", `\ `)
	cases := map[string]int{
		a:                 1, // unescaped spaces, single path
		esc:               1,
		"'" + a + "'":     1,
		esc + " " + b:     2,
		esc + "\n" + b:    2,
		"look at " + b:    0,
		b + " and words":  0,
		dir + "/none.png": 0,
		"/etc/hosts":      0,
	}
	for in, want := range cases {
		if got := ImagePaths(in, fileExists); len(got) != want {
			t.Errorf("%q: got %v", in, got)
		}
	}
	e := newTestEditor()
	typeText(e, "see")
	if r, _ := e.InsertPaste(esc + " " + b); r != PastedImages {
		t.Fatal("drop makes image chips")
	}
	if e.Display() != "see [Image #1] [Image #2] " {
		t.Fatalf("%q", e.Display())
	}
	if e.Chips()[0].Image.Path != a {
		t.Fatal("chip keeps the path")
	}
}

func TestStripInvisible(t *testing.T) {
	in := "a​b‮c\U000E0041d 👨‍👩‍👧 🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"
	out, n := StripInvisible(in)
	if n != 3 {
		t.Fatalf("removed %d", n)
	}
	if out != "abcd 👨‍👩‍👧 🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F" {
		t.Fatalf("%q", out)
	}
	e := newTestEditor()
	e.SetValue("hi​ there")
	e.InsertChip(NewPasteChip(e.NextChipID(), "x⁦y\nz\nw\nv"))
	if got := e.StripInvisible(); got != 2 {
		t.Fatalf("editor removed %d", got)
	}
	if e.Text() != "hi therexy\nz\nw\nv" {
		t.Fatalf("%q", e.Text())
	}
	if e.StripInvisible() != 0 {
		t.Fatal("second pass finds nothing")
	}
}

func TestHistoryRoundTrip(t *testing.T) {
	all, err := history.Load(fixtureDir + "/history.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	store := DirStore{Dir: fixtureDir + "/paste-cache"}
	for _, h := range all {
		e := newTestEditor()
		e.SetHistoryEntry(h, store)
		if e.Display() != h.Display {
			t.Fatalf("display %q != %q", e.Display(), h.Display)
		}
		got := e.HistoryEntry(h.Project, h.SessionID, time.UnixMilli(h.Timestamp))
		a, _ := history.Encode(got)
		b, _ := history.Encode(h)
		if string(a) != string(b) {
			t.Errorf("round trip:\n got %s\nwant %s", a, b)
		}
	}
	// The hashed paste was restored from the cache.
	e := newTestEditor()
	e.SetHistoryEntry(all[3], store)
	if len(e.Chips()) != 1 || !strings.Contains(e.Text(), "line 39 of a long pasted log") {
		t.Fatalf("hashed paste: %q", e.Text())
	}
	if e.ChipSeq() != 3 {
		t.Fatal("chip counter continues after recalled ids")
	}
	// Without a store the label stays text.
	e2 := newTestEditor()
	e2.SetHistoryEntry(all[3], nil)
	if len(e2.Chips()) != 0 || e2.Display() != all[3].Display {
		t.Fatal("no store")
	}
}
