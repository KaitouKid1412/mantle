package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Expected values come from running the engine's slug algorithm under node (JavaScript
// string semantics), not from this package.
func TestSlugGolden(t *testing.T) {
	a := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct{ in, want string }{
		{"/Users/x/proj", "-Users-x-proj"},
		{"/Users/alice/My Project_v2.0/src", "-Users-alice-My-Project-v2-0-src"},
		{"/private/tmp/claude-501/-Users-x-abc/scratchpad", "-private-tmp-claude-501--Users-x-abc-scratchpad"},
		{"/home/zoë/café", "-home-zo--caf-"},
		{"/home/u/emoji-😀-dir", "-home-u-emoji----dir"}, // one emoji = two UTF-16 units
		{`C:\Users\x\proj`, "C--Users-x-proj"},
		{"/" + a(199), "-" + a(199)},
		{"/" + a(200), "-" + a(199) + "-b6ymvl"},
		{"/" + a(250), "-" + a(199) + "-feo44x"},
		{"/very/long/" + strings.Repeat("segment-name/", 20) + "end",
			"-very-long-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-name-segment-ftn8vm"},
		{"/deep/" + strings.Repeat("ü", 230), "-deep-" + strings.Repeat("-", 194) + "-u4a9ho"},
		{"/x/" + strings.Repeat("😀", 120), "-x-" + strings.Repeat("-", 197) + "-rq304y"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Slug(c.in); got != c.want {
			t.Errorf("Slug(%.40q…)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestSlugHashInt32Edge(t *testing.T) {
	// abs(math.MinInt32) must not overflow.
	if got := slugHash(nil); got != "0" {
		t.Fatalf("empty hash = %q", got)
	}
}

func TestLayoutFromEnv(t *testing.T) {
	env := map[string]string{"CLAUDE_CONFIG_DIR": "/cfg", "CLAUDE_CODE_PROJECT_DIR_NAME": "pinned_1"}
	l := LayoutFromEnv(func(k string) string { return env[k] })
	if l.ConfigDir != "/cfg" || l.ProjectDirName != "pinned_1" {
		t.Fatalf("layout = %+v", l)
	}
	if got := l.ProjectDir("/any/where"); got != filepath.Join("/cfg", "projects", "pinned_1") {
		t.Fatalf("ProjectDir = %q", got)
	}
	// The project name override needs CLAUDE_CONFIG_DIR, and must be a safe name.
	delete(env, "CLAUDE_CONFIG_DIR")
	if l := LayoutFromEnv(func(k string) string { return env[k] }); l.ProjectDirName != "" {
		t.Fatalf("override honoured without CLAUDE_CONFIG_DIR: %+v", l)
	}
	env["CLAUDE_CONFIG_DIR"] = "/cfg"
	for _, bad := range []string{"../x", "con", "LPT1", strings.Repeat("x", 65)} {
		env["CLAUDE_CODE_PROJECT_DIR_NAME"] = bad
		if l := LayoutFromEnv(func(k string) string { return env[k] }); l.ProjectDirName != "" {
			t.Errorf("unsafe name %q accepted", bad)
		}
	}
	l = Layout{ConfigDir: "/cfg"}
	if got := l.ProjectDir("/Users/x/proj"); got != "/cfg/projects/-Users-x-proj" {
		t.Fatalf("ProjectDir = %q", got)
	}
	if got := l.FileHistoryDir(sidPlain); got != "/cfg/file-history/"+sidPlain {
		t.Fatalf("FileHistoryDir = %q", got)
	}
}

func TestValidID(t *testing.T) {
	if !ValidID(sidPlain) || ValidID("not-a-uuid") || ValidID("../"+sidPlain) {
		t.Fatal("ValidID")
	}
}

func TestFindSession(t *testing.T) {
	l := copyConfig(t)
	p, err := l.FindSession(sidTools, "/work/demo")
	if err != nil || p != SessionFile(l.ProjectDir("/work/demo"), sidTools) {
		t.Fatalf("FindSession = %q, %v", p, err)
	}
	// Found by scanning every project when the cwd does not match.
	p, err = l.FindSession(sidOther, "/somewhere/else")
	if err != nil || filepath.Base(filepath.Dir(p)) != "-work-other" {
		t.Fatalf("FindSession scan = %q, %v", p, err)
	}
	if _, err := l.FindSession("99999999-9999-4999-8999-999999999999", "/work/demo"); err != ErrNotFound {
		t.Fatalf("missing session err = %v", err)
	}
	if _, err := l.FindSession("../../etc/passwd", ""); err != ErrNotFound {
		t.Fatalf("bad id err = %v", err)
	}
	// Empty files do not count.
	empty := SessionFile(l.ProjectDir("/work/demo"), "88888888-8888-4888-8888-888888888888")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.FindSession("88888888-8888-4888-8888-888888888888", "/work/demo"); err != ErrNotFound {
		t.Fatalf("empty file err = %v", err)
	}
}

func TestProjectDirsLongPath(t *testing.T) {
	l := Layout{ConfigDir: t.TempDir()}
	cwd := "/work/" + strings.Repeat("deep/", 50) + "repo"
	slug := Slug(cwd)
	if len(slug) <= MaxSlugLength {
		t.Fatal("test path too short")
	}
	exact := filepath.Join(l.ProjectsDir(), slug)
	legacy := filepath.Join(l.ProjectsDir(), slug[:MaxSlugLength]+"-legacyhash")
	unrelated := filepath.Join(l.ProjectsDir(), slug[:MaxSlugLength]+"-otherhash")
	for _, d := range []string{exact, legacy, unrelated} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, cwd string) {
		line := `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"hi"},"uuid":"u1","timestamp":"2026-09-01T10:00:00.000Z","cwd":"` + cwd + `","sessionId":"` + sidPlain + `"}` + "\n"
		if err := os.WriteFile(SessionFile(dir, sidPlain), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(legacy, cwd)
	write(unrelated, "/elsewhere/"+strings.Repeat("x", 300))

	got := l.ProjectDirs(cwd)
	if len(got) != 2 || got[0] != exact || got[1] != legacy {
		t.Fatalf("ProjectDirs = %q", got)
	}
}
