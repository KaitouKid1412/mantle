package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/KaitouKid1412/mantle/internal/cli"
)

func TestClassifyTUI(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{"", ""},
		{"\x1b]0;title\x07loading", ""},
		{"\x1b[?25l\x1b[?1049h\x1b[2J", "fullscreen"},
		{"\x1b[?1002h\x1b[?1006h", "fullscreen"},
		{"╭───╮\r\n│ ❯ \r\n", "default"},
		{"  ? for shortcuts", "default"},
		{"❯ then \x1b[?1049h", "fullscreen"}, // alt screen wins over a prompt glyph
	} {
		if got := classifyTUI([]byte(c.out)); got != c.want {
			t.Errorf("classifyTUI(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestCompareDefaults(t *testing.T) {
	ok := List{Kind: KindDefault, Items: []Item{{Name: "tui", Attrs: map[string]string{"value": "fullscreen"}}}}
	if fs := compareDefaults(ok, "2.1.289"); len(fs) != 0 {
		t.Errorf("matching default: %+v", fs)
	}
	// A claude whose fresh-config renderer differs from the table fails make drift.
	fs := compareDefaults(ok, "2.1.288")
	if len(fs) != 1 || fs[0].Status != StatusMismatch || !fs[0].Fails() {
		t.Errorf("changed default: %+v", fs)
	}
}

func TestCollectDefaults(t *testing.T) {
	c := &Collector{Claude: "claude", TUIProbe: func(context.Context, string, time.Duration) (string, error) { return "default", nil }}
	l := c.collectDefaults(context.Background())
	if !l.Available() || l.Items[0].Attrs["value"] != "default" {
		t.Errorf("list %+v", l)
	}
	c.TUIProbe = func(context.Context, string, time.Duration) (string, error) { return "", errors.New("no prompt") }
	if l := c.collectDefaults(context.Background()); l.Available() {
		t.Error("a failed probe should be unavailable")
	}
	c.NoEngine = true
	if l := c.collectDefaults(context.Background()); l.Available() {
		t.Error("-no-engine should skip the probe")
	}
}

// TestProbeTUILive runs the real interactive claude (zero tokens: fresh config, API at
// a closed port). Opt-in: MANTLE_DRIFT_LIVE=1 go test ./scripts/drift -run Live
func TestProbeTUILive(t *testing.T) {
	if os.Getenv("MANTLE_DRIFT_LIVE") != "1" {
		t.Skip("set MANTLE_DRIFT_LIVE=1 to run the real claude")
	}
	bin, err := cli.ResolveClaude()
	if err != nil {
		t.Skip(err)
	}
	version, err := cli.EngineVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := probeTUI(context.Background(), bin, 0)
	if err != nil || got != cli.DefaultsFor(version).TUI {
		t.Errorf("fresh config: %q %v, table says %q for %s", got, err, cli.DefaultsFor(version).TUI, version)
	}
	got, err = probeTUIWith(context.Background(), bin, 0, `{"tui":"default"}`)
	if err != nil || got != "default" {
		t.Errorf(`tui "default": %q %v`, got, err)
	}
	got, err = probeTUIWith(context.Background(), bin, 0, `{"tui":"fullscreen"}`)
	if err != nil || got != "fullscreen" {
		t.Errorf(`tui "fullscreen": %q %v`, got, err)
	}
}
