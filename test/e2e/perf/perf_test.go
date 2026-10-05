// Package perf is plan 12's performance suite (B3): mantle against claude, both on the
// scripted fakeapi with isolated configs, so it is offline and free. It is slow (a few
// minutes), so it runs only with MANTLE_PERF=1 (`make perf`). Every test logs a table and
// appends it to test/parity/out/perf.md; budgets catch gross regressions only.
package perf

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/test/parity"
)

func root(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

func skipUnlessPerf(t *testing.T) {
	t.Helper()
	if os.Getenv("MANTLE_PERF") == "" {
		t.Skip("perf suite: set MANTLE_PERF=1 (make perf)")
	}
}

var (
	buildOnce sync.Once
	mantleBin string
	buildErr  error
)

// targets are claude (the reference) and the current mantle build.
func targets(t *testing.T) []parity.Target {
	t.Helper()
	buildOnce.Do(func() {
		mantleBin, buildErr = parity.BuildMantleUI(filepath.Join(root(t), "test", "parity", "out", ".bin"))
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return []parity.Target{parity.ClaudeTarget{}, parity.MantleTarget{Bin: mantleBin}}
}

// scenario parses a scenario; script, when not "", is a fakeapi script body written
// next to it.
func scenario(t *testing.T, text, script string) *parity.Scenario {
	t.Helper()
	if script != "" {
		p := filepath.Join(t.TempDir(), "script.json")
		if err := os.WriteFile(p, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		text = "script: " + p + "\n" + text
	}
	sc, err := parity.ParseScenario(text)
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func run(t *testing.T, tg parity.Target, sc *parity.Scenario, o parity.RunOptions) *parity.Result {
	t.Helper()
	if o.Timeout == 0 {
		o.Timeout = 3 * time.Minute
	}
	res := parity.Run(context.Background(), tg, sc, o)
	if res.Err != nil {
		t.Fatalf("%s/%s: %v\n%s", tg.Name(), sc.Name, res.Err, strings.Join(res.Final.Screen, "\n"))
	}
	return res
}

func median(d []time.Duration) time.Duration {
	s := slices.Clone(d)
	slices.Sort(s)
	return s[len(s)/2]
}

func mb(b int64) string { return fmt.Sprintf("%.0f MB", float64(b)/(1<<20)) }

// report logs a markdown table and appends it to test/parity/out/perf.md.
func report(t *testing.T, title string, header []string, rows [][]string) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "\n### %s (%s)\n\n| %s |\n|%s\n", title, time.Now().Format(time.RFC3339),
		strings.Join(header, " | "), strings.Repeat("---|", len(header)))
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s |\n", strings.Join(r, " | "))
	}
	t.Log(b.String())
	out := filepath.Join(root(t), "test", "parity", "out")
	if err := os.MkdirAll(out, 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join(out, "perf.md"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString(b.String())
			_ = f.Close()
		}
	}
}

func runs() int {
	if n := os.Getenv("MANTLE_PERF_N"); n != "" {
		var v int
		if _, err := fmt.Sscan(n, &v); err == nil && v > 0 {
			return v
		}
	}
	return 5
}

// TestPerfStartup: cold launch to the first prompt and to a working engine (mantle
// draws its prompt before the engine answers initialize and holds input until then; its
// startup banner prints at initialize), median of N, and memory.
func TestPerfStartup(t *testing.T) {
	skipUnlessPerf(t)
	const banner = "/help for commands"
	sc := scenario(t, "name: startup\nsize: 100x30\n---\nready 60s\n@mantle wait_for 60s "+banner+"\n", "")
	med := map[string]time.Duration{}
	var rows [][]string
	for _, tg := range targets(t) {
		var times, engine []time.Duration
		var peak, own int64
		for range runs() {
			res := run(t, tg, sc, parity.RunOptions{})
			times = append(times, res.ReadyAfter)
			up := res.ReadyAfter // claude's prompt is up when its engine is
			if d, ok := res.Marks[banner]; ok {
				up = d
			}
			engine = append(engine, up)
			peak, own = max(peak, res.MaxRSS), max(own, res.RSS)
		}
		med[tg.Name()] = median(engine)
		rows = append(rows, []string{tg.Name(), median(times).Round(time.Millisecond).String(),
			med[tg.Name()].Round(time.Millisecond).String(), slices.Max(engine).Round(time.Millisecond).String(),
			mb(own), mb(peak)})
	}
	report(t, "Startup", []string{"Target", "First prompt (median)", "Engine ready (median)", "Engine ready (max)",
		"RSS (own process)", "Peak RSS (with engine)"}, rows)
	if m, c := med["mantle"], med["claude"]; m > 2*c+time.Second {
		t.Errorf("mantle starts in %v, claude in %v: more than twice as slow", m, c)
	}
}

// TestPerfIdleCPU: CPU used while sitting at the prompt for 10 s (spinner off, status
// line on), from the difference between an idle run and a start-only run.
func TestPerfIdleCPU(t *testing.T) {
	skipUnlessPerf(t)
	const idle = 10 * time.Second
	settings := `settings: {"statusLine": {"type": "command", "command": "echo status"}}` + "\n"
	base := scenario(t, "name: idle-base\nsize: 100x30\n"+settings+"---\nready 60s\n", "")
	sit := scenario(t, "name: idle\nsize: 100x30\n"+settings+"---\nready 60s\nsleep 10s\n", "")
	var rows [][]string
	pct := map[string]float64{}
	for _, tg := range targets(t) {
		b := run(t, tg, base, parity.RunOptions{})
		s := run(t, tg, sit, parity.RunOptions{})
		used := max(0, s.CPU-b.CPU)
		pct[tg.Name()] = 100 * float64(used) / float64(idle)
		rows = append(rows, []string{tg.Name(), used.Round(time.Millisecond).String(), fmt.Sprintf("%.1f%%", pct[tg.Name()])})
	}
	report(t, "Idle CPU at the prompt (10 s)", []string{"Target", "CPU time", "CPU"}, rows)
	if pct["mantle"] > 5 {
		t.Errorf("mantle uses %.1f%% CPU while idle", pct["mantle"])
	}
}

// TestPerfStreaming: a ~2k-token answer streamed at full speed; time to the end, and
// no ghost or repeated lines in mantle's scrollback.
func TestPerfStreaming(t *testing.T) {
	skipUnlessPerf(t)
	const n = 250
	var text strings.Builder
	text.WriteString("Streaming test.\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&text, "Stream line %03d: lorem ipsum dolor sit amet, consectetur.\n\n", i)
	}
	text.WriteString("END OF STREAM")
	script, _ := json.Marshal(map[string]any{"turns": []any{map[string]any{
		"reply": []any{map[string]any{"type": "text", "text": text.String()}}}}, "side_reply": "Perf", "default_reply": "OK."})
	sc := scenario(t, "name: streaming\nsize: 100x30\nprompt: Stream please\n---\nready 60s\nwait_for 120s END OF STREAM\nsettle\ncheckpoint end\n", string(script))
	var rows [][]string
	for _, tg := range targets(t) {
		res := run(t, tg, sc, parity.RunOptions{})
		cp, _ := res.Checkpoint("end")
		seen := map[int]int{}
		for _, l := range append(slices.Clone(cp.Frame.Scrollback), cp.Frame.Screen...) {
			var i int
			if _, err := fmt.Sscanf(strings.TrimSpace(l), "Stream line %03d:", &i); err == nil {
				seen[i]++
			}
		}
		missing, repeated := 0, 0
		for i := 1; i <= n; i++ {
			switch {
			case seen[i] == 0:
				missing++
			case seen[i] > 1:
				repeated++
			}
		}
		rows = append(rows, []string{tg.Name(), (cp.At - res.ReadyAfter).Round(time.Millisecond).String(),
			fmt.Sprint(missing), fmt.Sprint(repeated), res.CPU.Round(time.Millisecond).String(), mb(res.RSS), mb(res.MaxRSS)})
		if tg.Name() == "mantle" && (missing > 0 || repeated > 0) {
			t.Errorf("mantle scrollback: %d lines missing, %d repeated (ghost lines)", missing, repeated)
		}
	}
	report(t, "Streaming a 250-line answer", []string{"Target", "Prompt to end", "Missing", "Repeated", "CPU", "RSS (own)", "Peak RSS (with engine)"}, rows)
}

// TestPerfLargeOutput: a multi-megabyte Bash output and a 200,000-character line.
func TestPerfLargeOutput(t *testing.T) {
	skipUnlessPerf(t)
	script := `{"turns": [
	  {"reply": [{"type": "tool_use", "name": "Bash", "input": {"command": "seq 1 300000", "description": "Print many numbers"}}]},
	  {"reply": [{"type": "tool_use", "name": "Bash", "input": {"command": "printf '%*s\\n' 200000 '' | tr ' ' x", "description": "Print a long line"}}]},
	  {"reply": [{"type": "text", "text": "Large output done."}]}
	], "side_reply": "Perf", "default_reply": "OK."}`
	sc := scenario(t, "name: large-output\nsize: 100x30\nprompt: Print a lot\n"+
		`settings: {"permissions": {"allow": ["Bash(seq:*)", "Bash(printf:*)", "Bash(tr:*)"]}}`+"\n"+
		"---\nready 60s\nwait_for 180s Large output done.\nsettle\ncheckpoint done\n", script)
	var rows [][]string
	for _, tg := range targets(t) {
		res := run(t, tg, sc, parity.RunOptions{Timeout: 5 * time.Minute})
		cp, _ := res.Checkpoint("done")
		rows = append(rows, []string{tg.Name(), (cp.At - res.ReadyAfter).Round(time.Millisecond).String(),
			res.CPU.Round(time.Millisecond).String(), mb(res.RSS), mb(res.MaxRSS)})
	}
	report(t, "2 MB of Bash output and a 200k-character line", []string{"Target", "Prompt to done", "CPU", "RSS (own)", "Peak RSS (with engine)"}, rows)
}

// TestPerfWideCharacters: CJK, emoji and a table render with the same cells as claude.
func TestPerfWideCharacters(t *testing.T) {
	skipUnlessPerf(t)
	answer := "宽字符测试：你好，世界。\n\n" +
		"- 日本語の行です\n- 한국어 줄입니다\n- emoji 😀👍🏽🇯🇵 done\n\n" +
		"| 名字 | 数量 |\n|---|---|\n| 苹果 | 3 |\n| 🍣 | 12 |\n\nWIDE DONE"
	script, _ := json.Marshal(map[string]any{"turns": []any{map[string]any{
		"reply": []any{map[string]any{"type": "text", "text": answer}}}}, "side_reply": "Perf", "default_reply": "OK."})
	sc := scenario(t, "name: wide\nsize: 80x30\nprompt: Wide characters\n---\nready 60s\nwait_for 60s WIDE DONE\nsettle\ncheckpoint done\n", string(script))
	n := parity.DefaultNormalizer()
	lines := map[string][]string{}
	for _, tg := range targets(t) {
		res := run(t, tg, sc, parity.RunOptions{})
		cp, _ := res.Checkpoint("done")
		for _, l := range n.Frame(cp.Frame, res.Workspace) {
			if strings.ContainsAny(l, "宽日한😀名苹🍣") {
				lines[tg.Name()] = append(lines[tg.Name()], strings.TrimRight(l, " "))
			}
		}
	}
	var rows [][]string
	for i := range max(len(lines["claude"]), len(lines["mantle"])) {
		c, m := at(lines["claude"], i), at(lines["mantle"], i)
		same := "same"
		if c != m {
			same = "differs"
		}
		rows = append(rows, []string{"`" + c + "`", "`" + m + "`", same})
	}
	report(t, "Wide characters (80 columns)", []string{"claude", "mantle", ""}, rows)
	if len(lines["mantle"]) == 0 {
		t.Error("mantle shows none of the wide-character lines")
	}
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// TestPerfLongResume: resume a 10,000-item session; time until its last answer shows,
// and peak RSS.
func TestPerfLongResume(t *testing.T) {
	skipUnlessPerf(t)
	user, assistant := sessionTemplate(t)
	const pairs = 5000
	sid := "5e55a0a0-0000-4000-8000-000000010000"
	sc := scenario(t, "name: long-resume\nsize: 100x30\nargs: --resume "+sid+"\n---\nready 120s\nwait_for 120s Answer 4999 of the long session.\nsettle\ncheckpoint resumed\n", "")
	var rows [][]string
	for _, tg := range targets(t) {
		res := run(t, tg, sc, parity.RunOptions{Timeout: 5 * time.Minute, Prepare: func(ws parity.Workspace) error {
			return writeSession(ws, sid, user, assistant, pairs)
		}})
		cp, _ := res.Checkpoint("resumed")
		rows = append(rows, []string{tg.Name(), cp.At.Round(time.Millisecond).String(),
			res.ReadyAfter.Round(time.Millisecond).String(), res.CPU.Round(time.Millisecond).String(), mb(res.RSS), mb(res.MaxRSS)})
	}
	report(t, "Resume a 10,000-item session", []string{"Target", "Start to last answer", "Start to prompt", "CPU", "RSS (own)", "Peak RSS (with engine)"}, rows)
}

// sessionTemplate records one real exchange with claude (fakeapi) and returns its user
// and assistant transcript lines, so generated sessions use the engine's own format.
func sessionTemplate(t *testing.T) (user, assistant map[string]any) {
	t.Helper()
	script := `{"turns": [{"reply": [{"type": "text", "text": "Template answer."}]}], "side_reply": "Perf", "default_reply": "OK."}`
	sc := scenario(t, "name: template\nsize: 100x30\nprompt: Template question\n---\nready 60s\nwait_for 60s Template answer.\nsettle 2s\n", script)
	res := run(t, parity.ClaudeTarget{}, sc, parity.RunOptions{KeepWorkspace: true})
	defer os.RemoveAll(res.Workspace.Root)
	files, _ := filepath.Glob(filepath.Join(res.Workspace.ConfigDir, "projects", "*", "*.jsonl"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			var m map[string]any
			if json.Unmarshal([]byte(line), &m) != nil {
				continue
			}
			msg, _ := m["message"].(map[string]any)
			switch {
			case m["type"] == "user" && m["isMeta"] == nil && msg != nil && msg["content"] == "Template question" && user == nil:
				user = m
			case m["type"] == "assistant" && strings.Contains(line, "Template answer.") && assistant == nil:
				assistant = m
			}
		}
	}
	if user == nil || assistant == nil {
		t.Fatalf("no template exchange in %v", files)
	}
	return user, assistant
}

// writeSession writes pairs question/answer exchanges as session sid of ws's project.
func writeSession(ws parity.Workspace, sid string, user, assistant map[string]any, pairs int) error {
	dir := filepath.Join(ws.ConfigDir, "projects", strings.ReplaceAll(ws.WorkDir, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, sid+".jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	parent := any(nil)
	start := time.Now().Add(-time.Hour).UTC()
	for i := range pairs {
		for k, tmpl := range []map[string]any{user, assistant} {
			m := clone(tmpl)
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 2*i+k)
			m["uuid"], m["parentUuid"], m["sessionId"], m["cwd"] = id, parent, sid, ws.WorkDir
			m["timestamp"] = start.Add(time.Duration(2*i+k) * 100 * time.Millisecond).Format(time.RFC3339Nano)
			msg := m["message"].(map[string]any)
			if k == 0 {
				msg["content"] = fmt.Sprintf("Question %d of the long session.", i)
			} else {
				msg["content"] = []any{map[string]any{"type": "text", "text": fmt.Sprintf("Answer %d of the long session.", i)}}
				msg["id"] = fmt.Sprintf("msg_perf_%d", i)
			}
			data, err := json.Marshal(m)
			if err != nil {
				return err
			}
			w.Write(data)
			w.WriteByte('\n')
			parent = id
		}
	}
	return w.Flush()
}

func clone(m map[string]any) map[string]any {
	data, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}
