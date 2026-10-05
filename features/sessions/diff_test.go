package sessions

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/proto"
)

func TestParseUnifiedDiff(t *testing.T) {
	out := "diff --git a/main.go b/main.go\nindex 1..2 100644\n--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,4 @@\n package main\n+import \"fmt\"\n-func x() {}\n+func y() {}\n diff\n" +
		"diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+hello\n" +
		"diff --git a/img.png b/img.png\nBinary files a/img.png and b/img.png differ\n"
	files := parseUnifiedDiff(out)
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	if f := files[0]; f.path != "main.go" || f.added != 2 || f.removed != 1 || len(f.hunks) != 1 || f.hunks[0].NewLines != 4 {
		t.Fatalf("main.go = %+v", f)
	}
	if !files[1].isNew || files[1].path != "new.txt" || !files[2].binary {
		t.Fatalf("new/binary = %+v %+v", files[1], files[2])
	}
}

var addedLineRE = regexp.MustCompile(`(?m)\+\s?b\s*$`)

func TestDiffTurnsAndViewer(t *testing.T) {
	edit := toolItem("e1", "Edit", `{"file_path":"/w/a.go"}`, "ok")
	edit.Result.Structured = json.RawMessage(`{"filePath":"/w/a.go","structuredPatch":[{"oldStart":1,"oldLines":1,"newStart":1,"newLines":1,"lines":["-a","+b"]}]}`)
	write := toolItem("w1", "Write", `{"file_path":"/w/go.sum"}`, "ok")
	write.Result.Structured = json.RawMessage(`{"filePath":"/w/go.sum","type":"create","content":"x\ny\n"}`)
	tr := &fakeTranscript{items: []*ext.Item{promptItem("p1", "change a"), edit, write, promptItem("p2", "nothing")}}
	src := turnSources(tr)
	if len(src) != 1 || len(src[0].files) != 2 || !strings.Contains(src[0].label, "change a") {
		t.Fatalf("sources = %+v", src)
	}

	h := newHarness(t)
	h.ctx.TranscriptV = tr
	h.f.getwd = func() (string, error) { return t.TempDir(), nil }
	h.ctx.SessionValue.Cwd = ""
	d := h.openDialog(DialogDiff, nil).(*diffViewer)
	if d.sources[0].err == nil {
		t.Fatal("a temp dir is not a git repository")
	}
	_, _ = d.HandleAction(h.ctx, ext.ActDiffNextSource)
	v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 80, MaxHeight: 24}).Text)
	// An added line holding "b", whatever spacing diffview puts after the marker.
	if !strings.Contains(v, "a.go") || strings.Contains(v, "go.sum") || !addedLineRE.MatchString(v) {
		t.Fatalf("turn view (noise hidden):\n%s", v)
	}
	_, _ = d.HandleAction(h.ctx, ext.ActAppToggleDiffNoiseFilter)
	if v := ansi.Strip(d.View(h.ctx, ext.Area{Width: 80, MaxHeight: 24}).Text); !strings.Contains(v, "go.sum") {
		t.Fatalf("noise shown:\n%s", v)
	}
	_ = proto.BlockText
}
