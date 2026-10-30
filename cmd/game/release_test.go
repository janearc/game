package main

import (
	"strings"
	"testing"
)

// the word and the tag read in either order, and the flags, which pzm has
// taken out, are in the call.
func TestReleaseFormReadsEitherOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args      string
		form, tag string
		flag      string
	}{
		{"v0.4.1", "", "v0.4.1", ""},
		{"v0.4.1 --i-mean-it", "", "v0.4.1", "i-mean-it"},
		{"diff v0.4.1", "diff", "v0.4.1", ""},
		{"v0.4.1 diff", "diff", "v0.4.1", ""},
		{"diff", "diff", "", ""},
		{"push v0.4.1 --publish", "push", "v0.4.1", "publish"},
		{"v0.4.1 push --publish", "push", "v0.4.1", "publish"},
		{"stack v0.4.1 --from road", "stack", "v0.4.1", "from"},
	}
	for _, tc := range cases {
		call := mustCall(t, "release", strings.Fields(tc.args))
		form, tag, err := releaseForm(call.Args())
		if err != nil || form != tc.form || tag != tc.tag {
			t.Errorf("%q: form %q tag %q err %v", tc.args, form, tag, err)
		}
		if tc.flag != "" && !(tc.flag == "from" && call.Given("from") ||
			tc.flag != "from" && call.On(tc.flag)) {
			t.Errorf("%q: pzm did not take --%s", tc.args, tc.flag)
		}
		if err := releaseFlagsFit(form, call); err != nil {
			t.Errorf("%q: %v", tc.args, err)
		}
	}
}

// a flag the form does not take is refused. anti-test: --publish belongs
// to push and --from to stack, and diff takes none.
func TestReleaseRefusesAFlagTheFormDoesNotTake(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]string{
		"v1 --publish":        "release: --publish is a flag it does not take",
		"push v1 --i-mean-it": "release push: --i-mean-it is a flag it does not take",
		"diff v1 --from road": "release diff: --from is a flag it does not take",
		"v1 --from road":      "release: --from is a flag it does not take",
	} {
		call := mustCall(t, "release", strings.Fields(args))
		form, _, err := releaseForm(call.Args())
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		if err := releaseFlagsFit(form, call); err == nil ||
			err.Error() != want {
			t.Errorf("%q: %v, want %q", args, err, want)
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
		_, _, err := releaseForm(strings.Fields(args))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", args, want, err)
		}
	}
}
