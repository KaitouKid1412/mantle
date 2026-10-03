package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// PasteConfig controls paste handling.
type PasteConfig struct {
	// Pastes longer than MaxChars characters or with more than MaxLines
	// lines collapse into a "[Pasted text #N +L lines]" chip. Zero disables
	// the respective limit; both zero disables collapsing.
	MaxChars, MaxLines int
	// Store saves the text of collapsed pastes longer than InlinePasteMax
	// (Claude Code's paste-cache). Nil skips storing.
	Store PasteStore
	// DetectImages turns pasted image file paths (drag and drop) into image
	// chips.
	DetectImages bool
	// FileExists checks dropped paths; nil means os.Stat.
	FileExists func(path string) bool
}

// DefaultPasteConfig matches Claude Code: collapse above 800 characters or
// 3 lines, detect dropped images, no store (hosts set one).
func DefaultPasteConfig() PasteConfig {
	return PasteConfig{MaxChars: 800, MaxLines: 3, DetectImages: true}
}

// InlinePasteMax is the longest paste (in characters) whose text history
// keeps inline; longer pastes are referenced by their paste-cache hash.
const InlinePasteMax = 1024

// PasteStore saves collapsed paste text.
type PasteStore interface {
	// Put stores text under key (see PasteHash).
	Put(key, text string) error
	// Get returns the text stored under key.
	Get(key string) (string, error)
}

// PasteHash is the paste-cache key for text: the first 16 hex digits of its
// SHA-256, as Claude Code names ~/.claude/paste-cache/<hash>.txt.
func PasteHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// DirStore stores pastes as <Dir>/<hash>.txt.
type DirStore struct{ Dir string }

// Put writes the file atomically; an existing file is left alone.
func (d DirStore) Put(key, text string) error {
	p := filepath.Join(d.Dir, key+".txt")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(d.Dir, ".paste-*")
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), p)
}

// Get reads <Dir>/<key>.txt.
func (d DirStore) Get(key string) (string, error) {
	if strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return "", os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(d.Dir, key+".txt"))
	return string(b), err
}

// PasteStoreErrMsg reports a failed paste-cache write. The chip still holds
// the text, so the prompt is unaffected; hosts may show a notice.
type PasteStoreErrMsg struct{ Err error }

// PasteResult says what a paste turned into.
type PasteResult int

const (
	PastedNothing PasteResult = iota
	PastedInline
	PastedChip
	PastedImages
)

// HandlePasteMsg accepts tea.PasteStartMsg, tea.PasteMsg and
// tea.PasteEndMsg. Content between start and end is accumulated and
// processed at the end; a PasteMsg outside start/end is processed at once.
func (e *Editor) HandlePasteMsg(msg tea.Msg) (bool, tea.Cmd) {
	switch m := msg.(type) {
	case tea.PasteStartMsg:
		e.pasting = true
		e.pasteBuf.Reset()
		return true, nil
	case tea.PasteMsg:
		if e.pasting {
			e.pasteBuf.WriteString(m.Content)
			return true, nil
		}
		_, cmd := e.InsertPaste(m.Content)
		return true, cmd
	case tea.PasteEndMsg:
		if !e.pasting {
			return true, nil
		}
		e.pasting = false
		s := e.pasteBuf.String()
		e.pasteBuf.Reset()
		_, cmd := e.InsertPaste(s)
		return true, cmd
	}
	return false, nil
}

// InsertPaste inserts pasted text: dropped image paths become image chips, long
// text becomes a paste chip (stored via Paste.Store in the returned Cmd),
// anything else is inserted inline as one undo step.
func (e *Editor) InsertPaste(content string) (PasteResult, tea.Cmd) {
	if e.attach >= 0 {
		e.ExitAttachments()
	}
	text := SanitizePaste(content)
	if text == "" {
		return PastedNothing, nil
	}
	if e.Paste.DetectImages {
		exists := e.Paste.FileExists
		if exists == nil {
			exists = fileExists
		}
		if paths := ImagePaths(text, exists); len(paths) > 0 {
			e.checkpoint(editOther)
			for _, p := range paths {
				e.insertImageChip(NewImageChip(e.NextChipID(), &Image{Path: p}))
			}
			e.afterEdit()
			return PastedImages, nil
		}
	}
	if e.shouldCollapse(text) {
		ch := NewPasteChip(e.NextChipID(), text)
		var cmd tea.Cmd
		if st := e.Paste.Store; st != nil && utf8.RuneCountInString(text) > InlinePasteMax {
			ch.Hash = PasteHash(text)
			key := ch.Hash
			cmd = func() tea.Msg {
				if err := st.Put(key, text); err != nil {
					return PasteStoreErrMsg{err}
				}
				return nil
			}
		}
		e.InsertChip(ch)
		return PastedChip, cmd
	}
	e.checkpoint(editOther)
	e.insertFragment(fragment(splitLines(text)))
	e.afterEdit()
	return PastedInline, nil
}

func (e *Editor) shouldCollapse(text string) bool {
	c := e.Paste
	if c.MaxLines > 0 && strings.Count(text, "\n")+1 > c.MaxLines {
		return true
	}
	if c.MaxChars > 0 && len(text) > c.MaxChars && utf8.RuneCountInString(text) > c.MaxChars {
		return true
	}
	return false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// SanitizePaste normalizes newlines and removes terminal control sequences
// and control characters (other than newline and tab) from pasted text, so
// pasted bytes can never drive the terminal.
func SanitizePaste(s string) string {
	s = normalizeNewlines(s)
	if containsC1(s) {
		// Some terminals act on UTF-8 encoded C1 controls (U+009B is CSI):
		// rewrite them as their 7-bit forms so they are stripped as
		// sequences, not left as parameter debris.
		var b strings.Builder
		for _, r := range s {
			if r >= 0x80 && r < 0xa0 {
				b.WriteByte(0x1b)
				b.WriteRune(r - 0x40)
				continue
			}
			b.WriteRune(r)
		}
		s = b.String()
	}
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = ansi.Strip(s)
	}
	clean := true
	for i := 0; i < len(s); i++ {
		if b := s[i]; (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
			clean = false
			break
		}
	}
	if clean && !containsC1(s) && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == utf8.RuneError:
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func containsC1(s string) bool {
	// C1 controls are U+0080..U+009F, encoded as 0xC2 0x80..0x9F.
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xc2 && s[i+1] >= 0x80 && s[i+1] <= 0x9f {
			return true
		}
	}
	return false
}

// ---- invisible characters ----

// IsInvisible reports whether r is a character Claude Code strips before
// sending: zero-width characters, bidirectional controls and Unicode tag
// characters.
func IsInvisible(r rune) bool {
	switch {
	case r >= 0x200b && r <= 0x200f: // ZWSP, ZWNJ, ZWJ, LRM, RLM
		return true
	case r >= 0x202a && r <= 0x202e: // bidi embeddings and overrides
		return true
	case r >= 0x2060 && r <= 0x2064: // word joiner, invisible operators
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	case r == 0xfeff, r == 0x061c, r == 0x00ad, r == 0x180e:
		return true
	case r >= 0xe0000 && r <= 0xe007f: // tag characters
		return true
	}
	return false
}

// StripInvisible removes invisible characters from s. Emoji sequences that
// legitimately use ZWJ or tag characters (family emoji, subdivision flags)
// are kept intact. It returns the cleaned text and how many characters went.
func StripInvisible(s string) (string, int) {
	if !hasInvisible(s) {
		return s, 0
	}
	var b strings.Builder
	n := 0
	for len(s) > 0 {
		g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			g = s[:1]
		}
		s = s[len(g):]
		if isEmojiSequence(g) {
			b.WriteString(g)
			continue
		}
		for _, r := range g {
			if IsInvisible(r) {
				n++
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String(), n
}

func hasInvisible(s string) bool {
	for _, r := range s {
		if IsInvisible(r) {
			return true
		}
	}
	return false
}

// isEmojiSequence reports whether a grapheme cluster is a multi-rune emoji
// (it then may contain ZWJ or tag characters on purpose).
func isEmojiSequence(g string) bool {
	if utf8.RuneCountInString(g) < 2 {
		return false
	}
	for _, r := range g {
		if IsInvisible(r) || unicode.Is(unicode.Variation_Selector, r) {
			continue
		}
		return r >= 0x1f000 || (r >= 0x2600 && r <= 0x27bf) || (r >= 0x2190 && r <= 0x21ff) ||
			(r >= 0x2b00 && r <= 0x2bff) || r == 0x00a9 || r == 0x00ae || r == 0x203c || r == 0x2049
	}
	return false
}

// StripInvisible removes invisible characters from the buffer and from the
// text of paste chips. It returns how many were removed; Claude Code then
// asks for a second Enter before sending. The change is undoable.
func (e *Editor) StripInvisible() int {
	total := 0
	var lines [][]cell
	changed := false
	for _, l := range e.lines {
		var out []cell
		lineChanged := false
		for _, c := range l.cells {
			switch {
			case c.chip != nil && c.chip.Kind == ChipPaste:
				t, n := StripInvisible(c.chip.Text)
				if n > 0 {
					total += n
					nc := *c.chip
					nc.Text, nc.Lines, nc.label = t, strings.Count(t, "\n"), ""
					c = chipCell(&nc)
					lineChanged = true
				}
				out = append(out, c)
			case c.chip == nil:
				t, n := StripInvisible(c.g)
				if n > 0 {
					total += n
					lineChanged = true
					out = append(out, toCells(t)...)
					continue
				}
				out = append(out, c)
			default:
				out = append(out, c)
			}
		}
		if lineChanged {
			changed = true
		} else {
			out = l.cells
		}
		lines = append(lines, out)
	}
	if !changed {
		return 0
	}
	e.checkpoint(editOther)
	cur := e.cur
	e.restore(snapshot{lines: lines, cur: cur})
	e.afterEdit()
	return total
}

// ---- dropped image paths ----

var imageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// ImagePaths returns the image files named by a paste when the whole paste
// is one or more image paths, as terminals produce when files are dragged
// in: absolute or ~ paths, optionally quoted, with spaces backslash-escaped
// or not, separated by spaces or newlines. It returns nil otherwise.
func ImagePaths(text string, exists func(string) bool) []string {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 64<<10 {
		return nil
	}
	// One path, possibly with unescaped spaces.
	if !strings.Contains(text, "\n") {
		p := expandHome(unescape(unquote(text)))
		if _, ok := imageExts[strings.ToLower(filepath.Ext(p))]; ok && filepath.IsAbs(p) && exists(p) {
			return []string{p}
		}
	}
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return nil
	}
	var out []string
	for _, t := range tokens {
		p := expandHome(t)
		if _, ok := imageExts[strings.ToLower(filepath.Ext(p))]; !ok {
			return nil
		}
		if !filepath.IsAbs(p) || !exists(p) {
			return nil
		}
		out = append(out, p)
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	esc := false
	for _, r := range s {
		if r == '\\' && !esc {
			esc = true
			continue
		}
		esc = false
		b.WriteRune(r)
	}
	return b.String()
}

func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	var quote rune
	esc, have := false, false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc, have = false, true
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, have = r, true
		case unicode.IsSpace(r):
			if have {
				toks = append(toks, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if have {
		toks = append(toks, cur.String())
	}
	return toks
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	if strings.HasPrefix(p, "file://") {
		return strings.TrimPrefix(p, "file://")
	}
	return p
}
