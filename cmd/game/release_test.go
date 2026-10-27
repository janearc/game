package main

import (
	"strings"
	"testing"
)

// the word and the tag read in either order, and the flags follow.
func TestReleaseFormReadsEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args      string
		form, tag string
		flags     string
	}{
		{"v0.4.1", "", "v0.4.1", ""},
		{"v0.4.1 --i-mean-it", "", "v0.4.1", "--i-mean-it"},
		{"diff v0.4.1", "diff", "v0.4.1", ""},
		{"v0.4.1 diff", "diff", "v0.4.1", ""},
		{"diff", "diff", "", ""},
		{"push v0.4.1 --publish", "push", "v0.4.1", "--publish"},
		{"v0.4.1 push --publish", "push", "v0.4.1", "--publish"},
		{"stack v0.4.1 --from road", "stack", "v0.4.1", "--from road"},
	}
	for _, tc := range cases {
		form, tag, flags, err := releaseForm(strings.Fields(tc.args))
		if err != nil || form != tc.form || tag != tc.tag ||
			strings.Join(flags, " ") != tc.flags {
			t.Errorf(
				"%q: form %q tag %q flags %q err %v",
				tc.args, form, tag, flags, err,
			)
		}
	}
}

// a word release does not know stops it and says what was meant, and a
// form that needs a tag says so. 2026-10-09: "release v0.4.0 diff"
// prepared again in silence, twice.
func TestReleaseFormStopsOnAWordItDoesNotKnow(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"v0.4.0 dif":        "did you mean game release diff v0.4.0",
		"v0.4.0 push extra": `"extra" is not a word release knows`,
		"diff push v0.4.0":  `"push" is not a word release knows`,
		"push":              "release wants a tag: game release push TAG",
		"":                  "release wants a tag: game release TAG",
	}
	for args, want := range cases {
		_, _, _, err := releaseForm(strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", args, want, err)
		}
	}
}
