package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/KaitouKid1412/mantle/pkg/ext"
	"github.com/KaitouKid1412/mantle/pkg/theme"
)

// Item is one Select row.
type Item struct {
	Label       string
	Description string // shown dim after the label
	Value       any
	Disabled    bool
}

// Select is a vertical option list (Claude Code's "❯ 1. Yes" pickers), optionally
// fuzzy-filtered by typing. It handles the select:* actions in the "Select" context.
// Embed it in a dialog and delegate Focusable/ActionHandler, or mount it directly.
type Select struct {
	IDValue    string
	Items      []Item
	Numbered   bool // "1. Yes"; digits 1-9 pick an item directly (unless Filterable)
	Filterable bool // typing filters with fuzzy matching
	MaxVisible int  // rows to show (0 = the area's height)
	Context    string

	OnAccept func(ext.Ctx, int, Item) tea.Cmd // index into Items
	OnCancel func(ext.Ctx) tea.Cmd
	OnChange func(ext.Ctx, int, Item) tea.Cmd // highlight moved (live previews)

	cursor  int   // index into visible
	offset  int   // first visible row
	filter  string
	visible []int // indices into Items
}

var _ ext.Focusable = (*Select)(nil)
var _ ext.ActionHandler = (*Select)(nil)

// NewSelect returns a Select over items.
func NewSelect(id string, items ...Item) *Select {
	s := &Select{IDValue: id, Items: items}
	s.refilter()
	return s
}

func (s *Select) ID() string            { return s.IDValue }
func (s *Select) Init(ext.Ctx) tea.Cmd  { s.refilter(); return nil }
func (s *Select) Update(ext.Ctx, tea.Msg) tea.Cmd { return nil }
func (s *Select) KeyContext() string {
	if s.Context != "" {
		return s.Context
	}
	return ext.ContextSelect
}

// SetItems replaces the items, keeping the highlight on the same label if possible.
func (s *Select) SetItems(items []Item) {
	cur, ok := s.Current()
	s.Items = items
	s.refilter()
	if ok {
		for i, idx := range s.visible {
			if s.Items[idx].Label == cur.Label {
				s.cursor = i
			}
		}
	}
}

// Current returns the highlighted item.
func (s *Select) Current() (Item, bool) {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return Item{}, false
	}
	return s.Items[s.visible[s.cursor]], true
}

// CurrentIndex returns the highlighted item's index into Items (-1 if none).
func (s *Select) CurrentIndex() int {
	if s.cursor < 0 || s.cursor >= len(s.visible) {
		return -1
	}
	return s.visible[s.cursor]
}

// SetCurrent highlights Items[i].
func (s *Select) SetCurrent(i int) {
	for v, idx := range s.visible {
		if idx == i {
			s.cursor = v
		}
	}
}

// Filter returns the filter text.
func (s *Select) Filter() string { return s.filter }

func (s *Select) refilter() {
	s.visible = s.visible[:0]
	if s.filter == "" {
		for i := range s.Items {
			s.visible = append(s.visible, i)
		}
	} else {
		labels := make([]string, len(s.Items))
		for i, it := range s.Items {
			labels[i] = it.Label
		}
		for _, m := range fuzzy.Find(s.filter, labels) {
			s.visible = append(s.visible, m.Index)
		}
	}
	if s.cursor >= len(s.visible) {
		s.cursor = max(0, len(s.visible)-1)
	}
	s.skipDisabled(1)
}

func (s *Select) skipDisabled(dir int) {
	for range len(s.visible) {
		if s.cursor < 0 || s.cursor >= len(s.visible) || !s.Items[s.visible[s.cursor]].Disabled {
			return
		}
		s.cursor = (s.cursor + dir + len(s.visible)) % len(s.visible)
	}
}

func (s *Select) move(c ext.Ctx, to int) tea.Cmd {
	if len(s.visible) == 0 {
		return nil
	}
	dir := 1
	if to < s.cursor {
		dir = -1
	}
	s.cursor = min(max(to, 0), len(s.visible)-1)
	s.skipDisabled(dir)
	c.Invalidate(s.IDValue)
	if s.OnChange != nil {
		if it, ok := s.Current(); ok {
			return s.OnChange(c, s.CurrentIndex(), it)
		}
	}
	return nil
}

func (s *Select) page(a ext.Area) int { return max(1, s.rows(a)-1) }

func (s *Select) rows(a ext.Area) int {
	n := len(s.visible)
	if s.MaxVisible > 0 {
		n = min(n, s.MaxVisible)
	}
	if a.MaxHeight > 0 {
		reserve := 0
		if s.Filterable {
			reserve = 1
		}
		n = min(n, max(1, a.MaxHeight-reserve))
	}
	return max(1, n)
}

func (s *Select) accept(c ext.Ctx) tea.Cmd {
	it, ok := s.Current()
	if !ok || it.Disabled || s.OnAccept == nil {
		return nil
	}
	return s.OnAccept(c, s.CurrentIndex(), it)
}

// HandleAction implements the select:* actions.
func (s *Select) HandleAction(c ext.Ctx, a ext.ActionID) (bool, tea.Cmd) {
	page := 5
	if s.MaxVisible > 0 {
		page = max(1, s.MaxVisible-1)
	}
	switch a {
	case ext.ActSelectNext:
		if s.cursor == len(s.visible)-1 {
			return true, s.move(c, 0) // wrap
		}
		return true, s.move(c, s.cursor+1)
	case ext.ActSelectPrevious:
		if s.cursor == 0 {
			return true, s.move(c, len(s.visible)-1)
		}
		return true, s.move(c, s.cursor-1)
	case ext.ActSelectPageDown:
		return true, s.move(c, s.cursor+page)
	case ext.ActSelectPageUp:
		return true, s.move(c, s.cursor-page)
	case ext.ActSelectFirst:
		return true, s.move(c, 0)
	case ext.ActSelectLast:
		return true, s.move(c, len(s.visible)-1)
	case ext.ActSelectAccept, ext.ActConfirmYes:
		return true, s.accept(c)
	case ext.ActSelectCancel, ext.ActConfirmNo:
		if s.Filterable && s.filter != "" {
			s.filter = ""
			s.refilter()
			c.Invalidate(s.IDValue)
			return true, nil
		}
		if s.OnCancel != nil {
			return true, s.OnCancel(c)
		}
		return false, nil
	}
	return false, nil
}

// HandleKey handles filtering and number picks; navigation comes through actions.
func (s *Select) HandleKey(c ext.Ctx, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.Filterable {
		switch {
		case k.Code == tea.KeyBackspace && k.Mod == 0:
			if s.filter != "" {
				_, size := lastRune(s.filter)
				s.filter = s.filter[:len(s.filter)-size]
				s.refilter()
				c.Invalidate(s.IDValue)
			}
			return true, nil
		case k.Text != "" && k.Mod&^tea.ModShift == 0:
			s.filter += k.Text
			s.cursor = 0
			s.refilter()
			c.Invalidate(s.IDValue)
			return true, nil
		}
		return false, nil
	}
	if s.Numbered && len(k.Text) == 1 && k.Text[0] >= '1' && k.Text[0] <= '9' {
		n := int(k.Text[0] - '1')
		if n < len(s.visible) && !s.Items[s.visible[n]].Disabled {
			s.cursor = n
			c.Invalidate(s.IDValue)
			return true, s.accept(c)
		}
		return true, nil
	}
	return false, nil
}

// HandlePaste appends pasted text to the filter.
func (s *Select) HandlePaste(c ext.Ctx, p tea.PasteMsg) (bool, tea.Cmd) {
	if !s.Filterable {
		return false, nil
	}
	s.filter += strings.ReplaceAll(p.Content, "\n", " ")
	s.cursor = 0
	s.refilter()
	c.Invalidate(s.IDValue)
	return true, nil
}

// View renders the visible window of items.
func (s *Select) View(c ext.Ctx, a ext.Area) ext.Rendered {
	t := c.Theme()
	var lines []string
	if s.Filterable {
		q := s.filter
		if q == "" {
			q = t.Paint(theme.Subtle, "Type to filter")
		}
		lines = append(lines, t.Paint(theme.Inactive, "⌕ ")+q)
	}
	n := s.rows(a)
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+n {
		s.offset = s.cursor - n + 1
	}
	s.offset = min(s.offset, max(0, len(s.visible)-n))
	if len(s.visible) == 0 {
		lines = append(lines, t.Paint(theme.Inactive, "  No matches"))
	}
	numW := len(strconv.Itoa(len(s.Items)))
	for row := s.offset; row < min(len(s.visible), s.offset+n); row++ {
		it := s.Items[s.visible[row]]
		sel := row == s.cursor
		prefix := "  "
		if sel {
			prefix = "❯ "
		}
		label := it.Label
		if s.Numbered {
			label = Pad(strconv.Itoa(row+1)+".", numW+1) + " " + label
		}
		tok := theme.Text
		switch {
		case it.Disabled:
			tok = theme.Inactive
		case sel:
			tok = theme.Suggestion
		}
		line := t.Paint(tok, prefix+label)
		if it.Description != "" {
			line += "  " + t.Paint(theme.Inactive, it.Description)
		}
		lines = append(lines, ansi.Truncate(line, a.Width, "…"))
	}
	if more := len(s.visible) - (s.offset + n); more > 0 && a.MaxHeight == 0 {
		lines = append(lines, t.Paint(theme.Inactive, "  ↓ "+strconv.Itoa(more)+" more"))
	}
	return ext.Rendered{Text: strings.Join(lines, "\n")}
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xC0 != 0x80 {
			return []rune(s[i:])[0], len(s) - i
		}
	}
	return 0, 0
}
