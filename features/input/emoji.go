package input

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/yuin/goldmark-emoji/definition"

	"github.com/KaitouKid1412/mantle/pkg/ext"
)

// emojiEntry is one shortcode.
type emojiEntry struct {
	name, char string
}

var (
	emojiOnce  sync.Once
	emojiIndex []emojiEntry // sorted by name
	emojiDefs  definition.Emojis
)

// emojis returns every GitHub shortcode. goldmark-emoji only exposes lookup
// by name, so the list is read from its table with read-only reflection; if
// that ever fails, completion degrades to exact names (emojiChar still works).
func emojis() []emojiEntry {
	emojiOnce.Do(func() {
		emojiDefs = definition.Github()
		defer func() { _ = recover() }()
		v := reflect.ValueOf(emojiDefs)
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return
		}
		list := v.FieldByName("list")
		if !list.IsValid() || list.Kind() != reflect.Slice {
			return
		}
		for i := 0; i < list.Len(); i++ {
			e := list.Index(i)
			names, uni := e.FieldByName("ShortNames"), e.FieldByName("Unicode")
			if !names.IsValid() || !uni.IsValid() {
				continue
			}
			rs := make([]rune, uni.Len())
			for k := range rs {
				rs[k] = rune(uni.Index(k).Int())
			}
			if len(rs) == 1 && rs[0] == 0xFFFD {
				continue // image-only GitHub emoji
			}
			for j := 0; j < names.Len(); j++ {
				emojiIndex = append(emojiIndex, emojiEntry{name: names.Index(j).String(), char: string(rs)})
			}
		}
		sort.Slice(emojiIndex, func(i, j int) bool { return emojiIndex[i].name < emojiIndex[j].name })
	})
	return emojiIndex
}

// emojiChar returns the emoji for a shortcode.
func emojiChar(name string) (string, bool) {
	emojis()
	e, ok := emojiDefs.Get(name)
	if !ok || !e.IsUnicode() {
		return "", false
	}
	return string(e.Unicode), true
}

// isEmojiQuery reports whether s can be a shortcode being typed (two or more
// of a-z 0-9 _ + -).
func isEmojiQuery(s string) bool {
	if len(s) < 2 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '+' || r == '-') {
			return false
		}
	}
	return true
}

// emoji fills the menu with shortcodes starting with, then containing, q.
func (m *completion) emoji(q string) {
	var pre, sub []compItem
	for _, e := range emojis() {
		switch {
		case strings.HasPrefix(e.name, q):
			pre = append(pre, compItem{value: e.char, label: e.char + " :" + e.name + ":"})
		case strings.Contains(e.name, q):
			sub = append(sub, compItem{value: e.char, label: e.char + " :" + e.name + ":"})
		}
	}
	if len(pre)+len(sub) == 0 {
		if ch, ok := emojiChar(q); ok {
			pre = append(pre, compItem{value: ch, label: ch + " :" + q + ":"})
		}
	}
	m.kind = compEmoji
	m.items = limit(append(pre, sub...))
	if m.sel >= len(m.items) {
		m.sel = 0
	}
}

// afterTyping replaces a just-closed ":shortcode:" with its emoji.
func (s *state) afterTyping(c ext.Ctx, typed string) {
	if typed != ":" || !s.cfg.emoji || s.mode != modePrompt {
		return
	}
	tok, start := s.ed.TokenBeforeCursor()
	if len(tok) < 4 || tok[0] != ':' || !strings.HasSuffix(tok, ":") || !s.ed.TokenAtWordStart(start) {
		return
	}
	name := tok[1 : len(tok)-1]
	if !isEmojiQuery(name) {
		return
	}
	if ch, ok := emojiChar(name); ok {
		s.ed.ReplaceBeforeCursor(start, ch)
	}
}
