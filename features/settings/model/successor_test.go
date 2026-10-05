package model

import (
	"reflect"
	"testing"
)

func TestSplitVersion(t *testing.T) {
	cases := map[string]struct {
		fam string
		ver []int
	}{
		"claude-opus-4-6":           {"claude-opus", []int{4, 6}},
		"claude-opus-5-5[1m]":       {"claude-opus", []int{5, 5}},
		"claude-haiku-4-5-20251001": {"claude-haiku", []int{4, 5}},
		"claude-fable-5-1":          {"claude-fable", []int{5, 1}},
		"opus":                      {"opus", nil},
	}
	for in, want := range cases {
		fam, ver := splitVersion(in)
		if fam != want.fam || !reflect.DeepEqual(ver, want.ver) {
			t.Errorf("%s: %s %v", in, fam, ver)
		}
	}
}

func TestSuccessor(t *testing.T) {
	f := loadFixture(t)
	rows := Rows(f.Models, nil, Current{}, EffortInputs{})
	cases := map[string]string{
		"claude-opus-4-6":           "opus",
		"claude-opus-4-6[1m]":       "opus[1m]",
		"claude-sonnet-4-5":         "sonnet",
		"claude-opus-5-5":           "", // already the newest
		"claude-opus-6-0":           "", // newer than anything listed
		"opus":                      "", // aliases follow new models on their own
		"claude-haiku-4-5-20251001": "",
		"":                          "",
	}
	for saved, want := range cases {
		r, ok := Successor(saved, rows)
		got := ""
		if ok {
			got = r.Value
		}
		if got != want {
			t.Errorf("Successor(%q) = %q, want %q", saved, got, want)
		}
	}
}
