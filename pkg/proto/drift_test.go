package proto

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// TestDriftReport decodes every stdout frame in local captures and lists fields the
// types don't model, types that decode as Unknown, and shape mismatches. Captures are
// fixture files ({"dir":"out"|"<","msg":...}) or raw stdout JSONL. Point it at a
// directory of real recordings (never committed; they hold personal data):
//
//	MANTLE_DRIFT_DIR=/path/to/captures go test -run DriftReport -v ./pkg/proto
func TestDriftReport(t *testing.T) {
	dir := os.Getenv("MANTLE_DRIFT_DIR")
	if dir == "" {
		t.Skip("set MANTLE_DRIFT_DIR")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	idx := regexp.MustCompile(`\[\d+\]`)
	lost := map[string]int{}
	unknown := map[string]int{}
	mismatch := map[string]string{}
	frames := 0
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 256<<20)
		for sc.Scan() {
			line := sc.Bytes()
			var wrap struct {
				Dir string          `json:"dir"`
				Msg json.RawMessage `json:"msg"`
			}
			if json.Unmarshal(line, &wrap) == nil && wrap.Dir != "" {
				if len(wrap.Msg) == 0 || wrap.Msg[0] != '{' {
					continue
				}
				if wrap.Dir != "out" && wrap.Dir != "<" {
					continue
				}
				line = wrap.Msg
			}
			ev, err := Decode(line)
			if err != nil {
				continue
			}
			frames++
			env := ev.Env()
			key := env.Type
			if env.Subtype != "" {
				key += "/" + env.Subtype
			}
			if _, ok := ev.(*Unknown); ok {
				unknown[key]++
				continue
			}
			if env.Mismatch != nil {
				mismatch[key] = env.Mismatch.Error()
			}
			paths, _ := Unmodelled(ev)
			for _, p := range paths {
				lost[key+": "+idx.ReplaceAllString(p, "[]")]++
			}
		}
		fh.Close()
	}
	t.Logf("%d frames from %d files", frames, len(files))
	for _, m := range []map[string]int{unknown, lost} {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Logf("%-70s %d", k, m[k])
		}
	}
	for k, v := range mismatch {
		t.Errorf("mismatch %s: %s", k, v)
	}
}
