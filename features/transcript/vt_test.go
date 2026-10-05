package transcript

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/app"
	"github.com/KaitouKid1412/mantle/internal/testkit"
	"github.com/KaitouKid1412/mantle/pkg/ext"
)

const inputMarker = "> type here"

// inputStub stands in for the prompt editor so the live frame has a bottom.
type inputStub struct{}

func (inputStub) ID() string                      { return "test.input" }
func (inputStub) Init(ext.Ctx) tea.Cmd            { return nil }
func (inputStub) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (inputStub) View(ext.Ctx, ext.Area) ext.Rendered {
	return ext.Rendered{Text: inputMarker}
}

func startHost(t *testing.T, w, h int) (*testkit.Harness, *Feature) {
	t.Helper()
	f := New(ext.MainEngine)
	features := []ext.Feature{
		{ID: FeatureID, Order: 100, Setup: f.Setup},
		{ID: "test.input", Order: 10, Setup: func(r ext.Registrar) error {
			r.AddComponent(ext.SlotInput, inputStub{}, ext.SlotOpts{})
			return nil
		}},
	}
	host := app.NewHost(features, app.HostOptions{Core: app.CoreFeatures()})
	root := app.New(app.Options{Host: host, NoBackgroundQuery: true})
	hs := testkit.New(t, root, testkit.WithSize(w, h))
	hs.WaitForText(inputMarker, 5*time.Second)
	return hs, f
}

// expectedScrollback is what the commit policy prints for a session at a
// terminal width, from the same feature code against a fake Ctx.
func expectedScrollback(t *testing.T, ndjson string, w int) []string {
	g := newRig(t, w)
	g.send(ndjson)
	return g.printed()
}

// TestReplayScrollback replays the sample session through the real host in a
// terminal emulator: scrollback plus screen must hold exactly the committed
// transcript followed by the live frame, with no ghost copies of the live area.
func TestReplayScrollback(t *testing.T) {
	if testing.Short() {
		t.Skip("vt replay")
	}
	for _, sz := range [][2]int{{80, 24}, {120, 40}, {61, 16}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			t.Parallel()
			want := expectedScrollback(t, sampleSession, sz[0])
			hs, _ := startHost(t, sz[0], sz[1])
			for _, ev := range decodeLines(t, sampleSession) {
				hs.SendMsg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
			}
			last := want[len(want)-1]
			hs.WaitFor(func(string) bool { return contains(hs.All(), last) }, 10*time.Second)
			hs.Settle(80*time.Millisecond, 3*time.Second)
			checkReplay(t, hs, want)
		})
	}
}

// TestStreamingNoArtifacts streams a long answer in small deltas with
// progressive commit and checks the final scrollback.
func TestStreamingNoArtifacts(t *testing.T) {
	if testing.Short() {
		t.Skip("vt replay")
	}
	answer := strings.Repeat("A paragraph that is long enough to wrap across a couple of lines at this width, streamed in small pieces.\n\n", 30)
	var b strings.Builder
	b.WriteString(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1","content":[]}}}` + "\n")
	b.WriteString(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}` + "\n")
	for i := 0; i < len(answer); i += 9 {
		chunk := answer[i:min(i+9, len(answer))]
		fmt.Fprintf(&b, `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}}`+"\n", chunk)
	}
	b.WriteString(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}` + "\n")
	fmt.Fprintf(&b, `{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"text","text":%q}]}}`+"\n", answer)
	ndjson := b.String()

	want := expectedScrollback(t, ndjson, 80)
	hs, _ := startHost(t, 80, 24)
	for _, ev := range decodeLines(t, ndjson) {
		hs.SendMsg(ext.EngineEventMsg{EngineID: ext.MainEngine, Event: ev})
	}
	hs.WaitFor(func(string) bool { return countContains(hs.All(), "streamed in small") >= 30 }, 10*time.Second)
	hs.Settle(80*time.Millisecond, 3*time.Second)
	checkReplay(t, hs, want)
}

func checkReplay(t *testing.T, hs *testkit.Harness, want []string) {
	t.Helper()
	all := hs.All()
	// Leading blank rows depend on where the first frame sat; compare from the
	// first printed text.
	all = maskClock(trimLeadingBlank(all))
	want = maskClock(trimLeadingBlank(want))
	if len(all) < len(want) {
		t.Fatalf("only %d lines on the terminal, want at least %d\n--- all ---\n%s", len(all), len(want), strings.Join(all, "\n"))
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q\n--- all ---\n%s", i, all[i], want[i], strings.Join(all, "\n"))
		}
	}
	rest := all[len(want):]
	if !contains(rest, inputMarker) {
		t.Fatalf("live frame missing after the transcript: %q", rest)
	}
	for _, l := range hs.Scrollback() {
		if strings.HasPrefix(l, inputMarker) {
			t.Fatalf("ghost live-frame line in scrollback\n--- scrollback ---\n%s", strings.Join(hs.Scrollback(), "\n"))
		}
	}
}

var reDoneTime = regexp.MustCompile(`· done \d{1,2}:\d{2}( [AP]M)?`)

// maskClock hides the time of day on duration lines: the host runs on the real
// clock, the expected transcript on a fixed one.
func maskClock(ls []string) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = reDoneTime.ReplaceAllString(l, "· done <time>")
	}
	return out
}

func trimLeadingBlank(ls []string) []string {
	for len(ls) > 0 && ls[0] == "" {
		ls = ls[1:]
	}
	return ls
}

func contains(ls []string, s string) bool { return countContains(ls, s) > 0 }

func countContains(ls []string, s string) int {
	n := 0
	for _, l := range ls {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}
