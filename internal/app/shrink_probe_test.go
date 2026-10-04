package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/KaitouKid1412/mantle/internal/testkit"
)

type shrinkModel struct{ lines []string }
type setLines []string

func (m *shrinkModel) Init() tea.Cmd { return nil }
func (m *shrinkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s, ok := msg.(setLines); ok {
		m.lines = s
	}
	return m, nil
}
func (m *shrinkModel) View() tea.View { return tea.NewView(strings.Join(m.lines, "\n")) }

// TestUpstreamInlineShrinkBug pins the Bubble Tea behaviour the host works around in
// holdHeight: a plain model whose inline frame shrinks from 3 rows to 1 leaves the old
// top rows on screen. If this test starts failing, Bubble Tea fixed it: remove the
// workaround (layout.go, shrinkState) and this test.
func TestUpstreamInlineShrinkBug(t *testing.T) {
	hs := testkit.New(t, &shrinkModel{lines: []string{"AAA", "BBB", "CCC"}}, testkit.WithSize(40, 10))
	hs.WaitForText("CCC", 2*time.Second)
	hs.Settle(80*time.Millisecond, time.Second)
	hs.SendMsg(setLines{"ZZZ"})
	hs.WaitForText("ZZZ", 2*time.Second)
	hs.Settle(80*time.Millisecond, time.Second)
	if !strings.Contains(hs.Screen(), "AAA") {
		t.Fatalf("upstream inline shrink now clears old rows; drop the holdHeight workaround\n%s", hs.Screen())
	}
}
