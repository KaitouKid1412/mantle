package history

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const fixture = "../../../../testdata/fixtures/04/history.jsonl"

func TestLoadFixture(t *testing.T) {
	all, err := Load(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 7 {
		t.Fatalf("entries: %d (malformed line must be skipped)", len(all))
	}
	demo := ForProject(all, "/work/demo")
	if len(demo) != 6 {
		t.Fatalf("project entries: %d", len(demo))
	}
	if got := ForSession(all, "00000000-0000-4000-8000-000000000003"); len(got) != 3 {
		t.Fatalf("session entries: %d", len(got))
	}
	p := all[1].Pasted()
	if len(p) != 1 || p[0].Content != "alpha\nbeta\ngamma\ndelta" {
		t.Fatalf("pasted: %+v", p)
	}
	if all[3].PastedContents["3"].ContentHash != "165444bd17d16a28" {
		t.Fatal("content hash")
	}
}

func TestMissingFile(t *testing.T) {
	es, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || es != nil {
		t.Fatal(es, err)
	}
}

// Lines mantle writes are byte-identical to Claude Code's.
func TestEncodeMatchesClaudeCode(t *testing.T) {
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := Load(fixture)
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(l, `{"display"`) {
			lines = append(lines, l)
		}
	}
	for i, e := range all {
		got, err := Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != lines[i]+"\n" {
			t.Errorf("line %d:\n got %s\nwant %s", i, got, lines[i])
		}
	}
}

func TestAppendRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	e := Entry{Display: "hello <world>", Timestamp: 1, Project: "/p", SessionID: "s"}
	e.AddPaste(PastedContent{ID: 1, Type: "text", Content: "x"})
	if err := Append(path, e); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Entry{Display: "two", Timestamp: 2, Project: "/p"}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	if got[0].Display != "hello <world>" || got[0].PastedContents["1"].Content != "x" {
		t.Fatalf("%+v", got[0])
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte(`"pastedContents":{}`)) || bytes.Contains(raw, []byte(`\u003c`)) {
		t.Fatalf("format: %s", raw)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
}

func TestConcurrentAppendsDoNotInterleave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	big := strings.Repeat("y", 3000)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := Append(path, Entry{Display: big, Timestamp: int64(j), Project: "/p"}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	got, err := Load(path)
	if err != nil || len(got) != 200 {
		t.Fatalf("got %d entries, err %v", len(got), err)
	}
}

func TestNavigator(t *testing.T) {
	all, _ := Load(fixture)
	n := NewNavigator(ForProject(all, "/work/demo"))
	if n.Len() != 5 {
		t.Fatalf("distinct entries: %d", n.Len())
	}
	if !n.AtDraft() {
		t.Fatal("starts at draft")
	}
	var seen []string
	for {
		e, ok := n.Older()
		if !ok {
			break
		}
		seen = append(seen, strings.SplitN(e.Display, " ", 2)[0])
	}
	want := "unicode same look fix explain"
	if got := strings.Join(seen, " "); got != want {
		t.Fatalf("older order: %q", got)
	}
	for i := 0; i < 4; i++ {
		if _, ok, draft := n.Newer(); !ok || draft {
			t.Fatalf("newer %d", i)
		}
	}
	if _, ok, draft := n.Newer(); !ok || !draft {
		t.Fatal("back to draft")
	}
	if _, ok, _ := n.Newer(); ok {
		t.Fatal("nothing newer than the draft")
	}
	n.Add(Entry{Display: "new prompt"})
	if e, _ := n.Older(); e.Display != "new prompt" {
		t.Fatal("added entry is newest")
	}
	n.Add(Entry{Display: "new prompt"})
	if n.Len() != 6 {
		t.Fatal("duplicate add collapses")
	}
}

func TestConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/cfg")
	if Path() != "/tmp/cfg/history.jsonl" || PasteCacheDir() != "/tmp/cfg/paste-cache" {
		t.Fatal(Path(), PasteCacheDir())
	}
}
