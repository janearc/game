package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func runPzmExample(args ...string) {
	var out, errs bytes.Buffer
	code := runPzm(args, "", "", "", strings.NewReader(""), &out, &errs)
	if code != exitOK {
		panic(errs.String())
	}
	line, _, _ := strings.Cut(out.String(), "\n")
	fmt.Println(line)
}

func TestGameToolExamples(t *testing.T) {
	invocation := pzmInvocation{dir: t.TempDir(), whose: t.TempDir(), home: t.TempDir()}
	if err := gameTool(invocation).Examples(os.DirFS(".")); err != nil {
		t.Fatal(err)
	}
}

func Example_gameTool_check() {
	runPzmExample("check", "--help")
	// Output:
	// game check [ARGS]: runs gofmt, vet and tests
}

func Example_gameTool_mutants() {
	runPzmExample("mutants", "--dry-run")
	// Output:
	// would run game mutants
}

func Example_gameTool_build() {
	runPzmExample("build", "--dry-run")
	// Output:
	// would run game build
}

func Example_gameTool_cover() {
	runPzmExample("cover", "--dry-run")
	// Output:
	// would run game cover
}

func Example_gameTool_aslr() {
	runPzmExample("aslr", "--dry-run")
	// Output:
	// would run game aslr
}

func Example_gameTool_leaks() {
	runPzmExample("leaks", "--dry-run")
	// Output:
	// would run game leaks
}

func Example_gameTool_harden() {
	runPzmExample("harden", "--help")
	// Output:
	// game harden [ARGS]: audits Go source and modules
}

func Example_gameTool_verify() {
	runPzmExample("verify", "--dry-run")
	// Output:
	// would run game verify
}

func Example_gameTool_patch() {
	runPzmExample("patch", "--dry-run")
	// Output:
	// would run game patch
}

func Example_gameTool_run() {
	runPzmExample("run", "--dry-run")
	// Output:
	// would run game run
}

func Example_gameTool_lint() {
	runPzmExample("lint", "--help")
	// Output:
	// game lint [ARGS]: runs the configured lint rules
}

func Example_gameTool_clean() {
	runPzmExample("clean", "--dry-run")
	// Output:
	// would run game clean
}

func Example_gameTool_trees() {
	runPzmExample("trees", "--help")
	// Output:
	// game trees [ARGS]: shows the road and dist
}

func Example_gameTool_history() {
	runPzmExample("history", "--help")
	// Output:
	// game history [ARGS]: shows the latest release and commits
}

func Example_gameTool_push() {
	runPzmExample("push", "--dry-run")
	// Output:
	// would run game push
}

func Example_gameTool_stick() {
	runPzmExample("stick", "--dry-run")
	// Output:
	// would run game stick
}

func Example_gameTool_sticky() {
	runPzmExample("sticky", "--dry-run")
	// Output:
	// would run game sticky
}

func Example_gameTool_unstick() {
	runPzmExample("unstick", "--dry-run")
	// Output:
	// would run game unstick
}

func Example_gameTool_release() {
	runPzmExample("release", "--dry-run")
	// Output:
	// would run game release
}

func Example_gameTool_bounce() {
	runPzmExample("bounce", "--help")
	// Output:
	// game bounce [ARGS]: checks third-person author references
}

func Example_gameTool_sweep() {
	runPzmExample("sweep", "--help")
	// Output:
	// game sweep [ARGS]: checks a tree for what must not ship
}

func Example_gameTool_gargleballsdario() {
	runPzmExample("gargle-balls-dario", "--help")
	// Output:
	// game gargle-balls-dario [ARGS]: answers the hidden verb
}
