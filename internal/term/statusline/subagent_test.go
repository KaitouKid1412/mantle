package statusline

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func TestSubagentPayloadMarshal(t *testing.T) {
	p := SubagentPayload{SessionID: "s", TranscriptPath: "/t", Cwd: "/w", Columns: 72}
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tasks":[]`) {
		t.Errorf("tasks must be []: %s", data)
	}

	p.Tasks = []SubagentTask{{ID: "a1", Name: "Explore", Type: "local_agent", Status: "running",
		Description: "find callers", Label: "Explore", StartTime: 1_700_000_000_000,
		Model: "claude-haiku-4-5", Effort: "low", ContextWindowSize: 200_000, TokenCount: 1234, Cwd: "/w"}}
	data, _ = p.Marshal()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	task := m["tasks"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "name", "type", "status", "description", "label", "startTime",
		"model", "effort", "contextWindowSize", "tokenCount", "tokenSamples", "cwd"} {
		if _, ok := task[k]; !ok {
			t.Errorf("task key %s missing", k)
		}
	}
	if task["tokenSamples"] == nil {
		t.Error("tokenSamples must be []")
	}

	p.Tasks[0].Effort, p.Tasks[0].Model, p.Tasks[0].ContextWindowSize = nil, "", 0
	data, _ = p.Marshal()
	for _, k := range []string{`"effort"`, `"model"`, `"contextWindowSize"`} {
		if strings.Contains(string(data), k) {
			t.Errorf("%s should be omitted when unknown: %s", k, data)
		}
	}
}

func TestParseRows(t *testing.T) {
	out := strings.Join([]string{
		`{"id":"a1","content":"\u001b[32mExplore\u001b[0m · 1.2k"}`,
		`{"id":"b2","content":""}`,
		`not json`,
		`{"content":"no id"}`,
		`{"id":"c3"}`,
		`  {"id":"d4","content":"first\nsecond"}  `,
		`{"id":"a1","content":"later wins\u001b[2J"}`,
		``,
	}, "\n")
	got := ParseRows([]byte(out))
	want := map[string]string{"a1": "later wins", "b2": "", "d4": "first"}
	if !maps.Equal(got, want) {
		t.Errorf("ParseRows = %q, want %q", got, want)
	}
	if len(ParseRows(nil)) != 0 {
		t.Error("empty output")
	}
}
