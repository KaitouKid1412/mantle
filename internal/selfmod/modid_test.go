package selfmod

import "testing"

func TestValidModID(t *testing.T) {
	for _, id := range []string{"demo", "spinner-blue", "a1", "x-2"} {
		if !ValidModID(id) {
			t.Errorf("ValidModID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "Demo", "1abc", "a_b", "a/b", "a-", "a--b", "../x", "a b", "abcdefghijabcdefghijabcdefghijabcdefghijx"} {
		if ValidModID(id) {
			t.Errorf("ValidModID(%q) = true", id)
		}
	}
}

func TestNewModID(t *testing.T) {
	cases := map[string]string{
		"make the spinner blue":                     "spinner-blue",
		"Add a /weather command that shows the sky": "weather-command-shows",
		"!!!":             "mod",
		"2 column layout": "mod-2-column-layout",
		"please":          "mod",
		"supercalifragilisticexpialidocious word": "supercalifragilisticexpialidocio",
	}
	for req, want := range cases {
		got := NewModID(req, nil)
		if got != want {
			t.Errorf("NewModID(%q) = %q, want %q", req, got, want)
		}
		if !ValidModID(got) {
			t.Errorf("NewModID(%q) = %q is not a valid id", req, got)
		}
	}
	taken := map[string]bool{"spinner-blue": true, "spinner-blue-2": true}
	if got := NewModID("spinner blue", func(id string) bool { return taken[id] }); got != "spinner-blue-3" {
		t.Errorf("got %q", got)
	}
}
