package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

func runCapture(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestSelftest(t *testing.T) {
	code, out, errs := runCapture("selftest")
	if code != 0 {
		t.Fatalf("selftest exit %d\n%s%s", code, out, errs)
	}
	if !strings.Contains(out, "selftest: ok") {
		t.Fatalf("output: %s", out)
	}
}

func TestStory(t *testing.T) {
	code, out, _ := runCapture("story", "ui.tabs/default", "--width", "60")
	if code != 0 || !strings.Contains(out, "Config") || strings.Contains(out, "\x1b[") {
		t.Fatalf("exit %d out %q", code, out)
	}
	code, out, _ = runCapture("story", "--width", "60", "ui.tabs/default", "--ansi")
	if code != 0 || !strings.Contains(out, "\x1b[") {
		t.Fatalf("--ansi: exit %d out %q", code, out)
	}
	if code, _, _ := runCapture("story", "no.such/story"); code != 1 {
		t.Fatalf("unknown story exit %d", code)
	}
	code, out, _ = runCapture("story", "--list")
	if code != 0 || !strings.Contains(out, "ui.select/numbered") {
		t.Fatalf("--list: %d %q", code, out)
	}
	if code, _, _ := runCapture("story", "--bogus"); code != ext.ExitUsage {
		t.Fatalf("bad flag exit %d", code)
	}
}

func TestCatalogJSON(t *testing.T) {
	code, out, errs := runCapture("catalog", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var c catalogOut
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatal(err)
	}
	has := func(kind, id string) bool {
		for _, e := range c.Entries {
			if string(e.Kind) == kind && e.ID == id {
				return true
			}
		}
		return false
	}
	if !has("feature", "core.app") || !has("action", "app:interrupt") || !has("story", "ui.select/numbered") {
		t.Fatal("catalog misses core entries")
	}
	if c.APIVersion != ext.APIVersion || len(c.Bindings) < 100 || len(c.Themes) < 6 {
		t.Fatalf("catalog: api %d, %d bindings, themes %v", c.APIVersion, len(c.Bindings), c.Themes)
	}
}
