package selfmod

import (
	"regexp"
	"strconv"
	"strings"
)

var modIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// ValidModID reports whether id can name a mod: lowercase letters, digits
// and dashes, starting with a letter, at most 40 characters. The id is used
// as a package directory (mods/<id>), a branch (mod/<id>) and a trailer.
func ValidModID(id string) bool {
	return modIDRe.MatchString(id) && !strings.HasSuffix(id, "-") && !strings.Contains(id, "--")
}

// stopWords are dropped when deriving an id from a request.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "of": true, "and": true, "or": true,
	"for": true, "in": true, "on": true, "with": true, "make": true, "please": true,
	"add": true, "it": true, "so": true, "that": true, "my": true, "me": true, "i": true,
	"want": true, "would": true, "like": true, "can": true, "you": true, "be": true, "is": true,
}

// NewModID derives a short id from a request ("make the spinner blue" →
// "spinner-blue"), adding a numeric suffix if taken(id) reports a clash.
func NewModID(request string, taken func(string) bool) string {
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(request), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if stopWords[w] {
			continue
		}
		words = append(words, w)
		if len(words) == 3 {
			break
		}
	}
	base := strings.Join(words, "-")
	if len(base) > 32 {
		base = strings.TrimRight(base[:32], "-")
	}
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = "mod-" + base
		base = strings.TrimRight(base, "-")
	}
	id := base
	for n := 2; taken != nil && taken(id); n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}
