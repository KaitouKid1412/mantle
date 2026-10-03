package fullscreen

import (
	"strings"
	"unicode"
)

// Match is one search hit: a document line and the cell columns [Col, End).
type Match struct {
	Line, Col, End int
}

// Search finds a query in the rendered transcript. It is smart-case: an all-lowercase
// query ignores case, any capital letter makes it case-sensitive.
type Search struct {
	Query   string
	Matches []Match
	Current int // index into Matches; -1 when none
}

// Run searches lines [0, n) of the document and selects the first match at or after
// line from (wrapping around).
func (s *Search) Run(query string, lines LineSource, n, from int) {
	s.Query, s.Matches, s.Current = query, nil, -1
	if query == "" {
		return
	}
	fold := !hasUpper(query)
	q := []rune(query)
	if fold {
		q = []rune(strings.ToLower(query))
	}
	for l := range n {
		cs := cells(lines(l))
		for c := 0; c+len(q) <= len(cs); c++ {
			if matchAt(cs, c, q, fold) {
				end := c
				for k := 0; k < len(q); k++ {
					end++
					for end < len(cs) && cs[end] == "" { // skip the right half of wide cells
						end++
					}
				}
				s.Matches = append(s.Matches, Match{Line: l, Col: c, End: end})
			}
		}
	}
	for i, m := range s.Matches {
		if m.Line >= from {
			s.Current = i
			return
		}
	}
	if len(s.Matches) > 0 {
		s.Current = 0
	}
}

// matchAt compares query runes against cells starting at c, skipping the empty right
// halves of wide characters.
func matchAt(cs []string, c int, q []rune, fold bool) bool {
	i := c
	for _, r := range q {
		for i < len(cs) && cs[i] == "" {
			i++
		}
		if i >= len(cs) {
			return false
		}
		cell := []rune(cs[i])
		if len(cell) == 0 {
			return false
		}
		got := cell[0]
		if fold {
			got = unicode.ToLower(got)
		}
		if got != r {
			return false
		}
		i++
	}
	return true
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// Next moves to the next match (n), wrapping; Prev (N) to the previous one.
func (s *Search) Next() (Match, bool) { return s.step(1) }

// Prev moves to the previous match.
func (s *Search) Prev() (Match, bool) { return s.step(-1) }

func (s *Search) step(d int) (Match, bool) {
	if len(s.Matches) == 0 {
		return Match{}, false
	}
	s.Current = ((s.Current+d)%len(s.Matches) + len(s.Matches)) % len(s.Matches)
	return s.Matches[s.Current], true
}

// At returns the current match.
func (s *Search) At() (Match, bool) {
	if s.Current < 0 || s.Current >= len(s.Matches) {
		return Match{}, false
	}
	return s.Matches[s.Current], true
}

// OnLine returns the matches on a line, for highlighting.
func (s *Search) OnLine(line int) []Match {
	var out []Match
	for _, m := range s.Matches {
		if m.Line == line {
			out = append(out, m)
		}
	}
	return out
}
