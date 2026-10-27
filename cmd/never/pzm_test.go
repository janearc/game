package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNeverToolExamples(t *testing.T) {
	if err := neverTool(neverInvocation{}).Examples(os.DirFS(".")); err != nil {
		t.Fatal(err)
	}
}

func TestRunNeverPzmBakesByDefault(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	var out, errs bytes.Buffer
	code := runNeverPzm(nil, home, home, game, strings.NewReader(""), &out, &errs)
	if code != exitOK || !strings.Contains(out.String(), "1 never-words") {
		t.Fatalf("default bake: code %d, out %q, errs %q", code, out.String(), errs.String())
	}
	if hashesIn(game) == "" {
		t.Fatal("default bake wrote no hashes")
	}
}

func TestRunNeverPzmReportsBakeFailure(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	hashesUnwritable(t, home, game)
	var out, errs bytes.Buffer
	code := runNeverPzm([]string{"bake"}, home, home, game,
		strings.NewReader(""), &out, &errs)
	if code != exitFailed || !strings.Contains(errs.String(), "could not be baked") {
		t.Fatalf("bake failure: code %d, errs %q", code, errs.String())
	}
}

func Example_neverTool_bake() {
	var out, errs bytes.Buffer
	code := runNeverPzm([]string{"bake", "--dry-run"}, "", "", "",
		strings.NewReader(""), &out, &errs)
	if code != exitOK {
		panic(errs.String())
	}
	fmt.Print(out.String())
	// Output:
	// would bake never-words
}
