package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// echoModel prints every submitted line into scrollback with tea.Println and shows the
// current input in its (inline) view.
type echoModel struct {
	input  string
	pasted string
	width  int
}

func (m *echoModel) Init() tea.Cmd { return nil }

func (m *echoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.PasteMsg:
		m.pasted = msg.Content
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			line := m.input
			m.input = ""
			return m, tea.Println("> " + line)
		case "backspace":
			if m.input != "" {
				m.input = m.input[:len(m.input)-1]
			}
		default:
			if t := msg.Text; t != "" {
				m.input += t
			} else {
				m.input += "<" + msg.String() + ">"
			}
		}
	}
	return m, nil
}

func (m *echoModel) View() tea.View {
	return tea.NewView(fmt.Sprintf("input: %s\npasted: %s\nwidth: %d", m.input, m.pasted, m.width))
}

func TestHarnessExample(t *testing.T) {
	t.Run("pty", func(t *testing.T) { harnessExample(t) })
	t.Run("pipe", func(t *testing.T) { harnessExample(t, WithPipe()) })
}

func harnessExample(t *testing.T, opts ...Option) {
	h := New(t, &echoModel{}, append([]Option{WithSize(40, 10)}, opts...)...)
	h.WaitForText("width: 40", 2*time.Second)

	h.Type("hello")
	h.Send("enter")
	h.WaitFor(func(s string) bool { return strings.Contains(s, "> hello") && strings.Contains(s, "input:") }, 2*time.Second)

	h.Paste("pasted text")
	h.WaitForText("pasted: pasted text", 2*time.Second)

	h.Send("shift+tab", "ctrl+x ctrl+k")
	h.WaitForText("<shift+tab><ctrl+x><ctrl+k>", 2*time.Second)

	h.Resize(50, 10)
	h.WaitForText("width: 50", 2*time.Second)

	h.Send("ctrl+c")
	if _, err := h.Wait(2 * time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestHarnessScrollback prints more lines than the screen holds and checks that they
// land in the emulator's scrollback, in order, followed by the live view.
func TestHarnessScrollback(t *testing.T) {
	h := New(t, &echoModel{}, WithSize(30, 6))
	h.WaitForText("width: 30", 2*time.Second)
	for i := range 8 {
		h.Type(fmt.Sprintf("l%d", i))
		h.Send("enter")
		h.WaitForText(fmt.Sprintf("> l%d", i), 2*time.Second)
	}
	h.Settle(50*time.Millisecond, time.Second)
	all := h.All()
	var printed []string
	for _, l := range all {
		if strings.HasPrefix(l, "> ") {
			printed = append(printed, l)
		}
	}
	want := []string{"> l0", "> l1", "> l2", "> l3", "> l4", "> l5", "> l6", "> l7"}
	if strings.Join(printed, ",") != strings.Join(want, ",") {
		t.Fatalf("printed lines = %q\nall:\n%s", printed, strings.Join(all, "\n"))
	}
	if len(h.Scrollback()) == 0 {
		t.Fatal("expected scrollback")
	}
}

func TestKeySeq(t *testing.T) {
	cases := map[string]string{
		"a": "a", "enter": "\r", "ctrl+c": "\x03", "shift+tab": "\x1b[Z", "alt+p": "\x1bp",
		"ctrl+up": "\x1b[1;5A", "shift+enter": "\x1b[13;2u", "ctrl+enter": "\x1b[13;5u",
		"ctrl+_": "\x1f", "esc": "\x1b", "pagedown": "\x1b[6~", "shift+g": "G",
	}
	for k, want := range cases {
		got, ok := KeySeq(k)
		if !ok || got != want {
			t.Errorf("KeySeq(%q) = %q, %v; want %q", k, got, ok, want)
		}
	}
	if _, ok := KeySeq("hyper+x"); ok {
		t.Error("unknown modifier should fail")
	}
}

func TestRunStory(t *testing.T) {
	s := ext.Story{ID: "testkit.example", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
		return ext.Rendered{Text: strings.Repeat("=", a.Width) + "\n" + c.Theme().Name}
	}}
	for _, w := range DefaultWidths {
		out, err := RenderStory(s, w)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(out, "dark") {
			t.Fatalf("got %q", out)
		}
	}
	bad := ext.Story{ID: "testkit.bad", Render: func(c ext.Ctx, a ext.Area) ext.Rendered {
		return ext.Rendered{Text: strings.Repeat("x", a.Width+1)}
	}}
	if _, err := RenderStory(bad, 60); err == nil {
		t.Fatal("overflow not reported")
	}
	panics := ext.Story{ID: "testkit.panic", Render: func(ext.Ctx, ext.Area) ext.Rendered { panic("boom") }}
	if _, err := RenderStory(panics, 60); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("panic not reported: %v", err)
	}
	RunStory(t, s, 20)
}
