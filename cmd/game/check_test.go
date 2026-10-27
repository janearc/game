package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSplitRapidReadsGoList(t *testing.T) {
	t.Parallel()
	listing := "x/a [fmt testing] []\n" +
		"x/b [testing pgregory.net/rapid] []\n" +
		"x/c [] [pgregory.net/rapid testing]\n" +
		"x/d [] []\n"
	plain, rapid := splitRapid(listing)
	if strings.Join(plain, " ") != "x/a x/d" {
		t.Errorf("plain: %v", plain)
	}
	if strings.Join(rapid, " ") != "x/b x/c" {
		t.Errorf("rapid: %v", rapid)
	}
	if plain, rapid := splitRapid(""); plain != nil || rapid != nil {
		t.Errorf("nothing listed: %v %v", plain, rapid)
	}
}

func TestSayTestsKeepsTheError(t *testing.T) {
	t.Parallel()
	failed := errors.New("go test: exit status 1")
	passed := "ok\tx/a\t0.1s\n?\tx/b\t[no test files]\n"
	if err := sayTests(io.Discard, passed, nil); err != nil {
		t.Errorf("passing tests: %v", err)
	}
	said := "--- FAIL: TestX\nFAIL\tx/a\n"
	if err := sayTests(io.Discard, said, failed); !errors.Is(err, failed) {
		t.Errorf("failing tests: %v", err)
	}
	out := "ok\tx/a\t0.1s\n\n?\tx/b\t[no test files]\n--- FAIL: TestX\n"
	got := strings.Join(worthSaying(out), "|")
	if got != "ok\tx/a\t0.1s|--- FAIL: TestX" {
		t.Errorf("worth saying: %q", got)
	}
	if lines := worthSaying("?\tx/b\t[no test files]\n"); lines != nil {
		t.Errorf("nothing worth saying: %q", lines)
	}
}
