package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseSample(t *testing.T) {
	src, _ := os.ReadFile("testdata/sample.d.ts")
	p := fromSDK(string(src))
	join := func(m map[string]bool) string {
		var s []string
		for k := range m {
			s = append(s, k)
		}
		return strings.Join(sorted(s), ",")
	}
	if got := join(p.Types); got != "assistant,brand_new_type,control_request,control_response,keep_alive,result,system" {
		t.Errorf("types %s", got)
	}
	if got := join(p.SystemSubtypes); got != "brand_new_subtype,init" {
		t.Errorf("system %s", got)
	}
	if got := join(p.ResultSubtypes); got != "error_during_execution,error_max_turns,success" {
		t.Errorf("result %s", got)
	}
	if got := join(p.ControlSubs); got != "brand_new_request,can_use_tool,interrupt" {
		t.Errorf("control %s", got)
	}

	items := diff(p, fromProto())
	want := map[string]string{
		"type:brand_new_type":       "sdk-only",
		"system:brand_new_subtype":  "sdk-only",
		"control:brand_new_request": "sdk-only",
		"type:tool_progress":        "proto-only",
		"control:initialize":        "proto-only",
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.Name] = it.Kind
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	if items[0].Kind != "sdk-only" {
		t.Error("sdk-only items should come first")
	}
}

func sorted(s []string) []string {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s
}

func TestJSONOutputAndDownload(t *testing.T) {
	src, _ := os.ReadFile("testdata/sample.d.ts")
	// Serve a tarball like the npm registry does.
	var tgz bytes.Buffer
	gz := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "package/README.md", Mode: 0o644, Size: 2})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "package/sdk.d.ts", Mode: 0o644, Size: int64(len(src))})
	tw.Write(src)
	tw.Close()
	gz.Close()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/@anthropic-ai/claude-agent-sdk/-/claude-agent-sdk-0.3.999.tgz" {
			http.NotFound(w, r)
			return
		}
		w.Write(tgz.Bytes())
	}))
	defer srv.Close()
	cache := t.TempDir()
	for i := 0; i < 2; i++ { // the second run reads the cache
		var out, errb bytes.Buffer
		if code := run([]string{"-json", "-sdk", "0.3.999", "-registry", srv.URL, "-cache", cache}, &out, &errb); code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
		var items []Item
		if err := json.Unmarshal(out.Bytes(), &items); err != nil || len(items) == 0 {
			t.Fatalf("json: %v %s", err, out.String())
		}
	}
	if hits != 1 {
		t.Errorf("downloaded %d times, want 1 (cache)", hits)
	}
	var errb bytes.Buffer
	if code := run([]string{"-sdk", "0.3.404", "-registry", srv.URL, "-cache", cache}, &bytes.Buffer{}, &errb); code == 0 {
		t.Error("missing version should fail")
	}
}
