package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// heldC is a C program that allocates and frees what it held.
const heldC = "#include <stdlib.h>\nint main(void) {\n\tvoid *p = malloc(16);\n" +
	"\tfree(p);\n\treturn 0;\n}\n"

// drips is a C program that allocates and never frees it. the pointer is
// volatile so an optimised build cannot elide the allocation.
const drips = "#include <stdlib.h>\nint main(void) {\n" +
	"\tvolatile int *p = malloc(sizeof *p);\n\t*p = 7;\n" +
	"\treturn *p == 7 ? 0 : 1;\n}\n"

// End to end with otool: a C program built as build-c builds it is PIE.
func TestAslrCMeasuresARealProgram(t *testing.T) {
	needs(t, "cc", "otool")
	dir := tree(t, map[string]string{"held.c": heldC})
	if err := game(dir, "aslr", "c", "held.c"); err != nil {
		t.Errorf("aslr c: %v", err)
	}
}

// End to end with leaks: the program that frees passes; the one that
// drips is found and fails.
func TestLeaksCFindsWhatIsLeftBehind(t *testing.T) {
	needs(t, "cc", "leaks", "xcrun")
	dir := tree(t, map[string]string{"held.c": heldC, "drips.c": drips})
	if err := game(dir, "leaks", "c", "held.c"); err != nil {
		t.Errorf("held.c: %v", err)
	}
	err := game(dir, "leaks", "c", "drips.c")
	if err == nil || !strings.Contains(err.Error(), "drips") {
		t.Errorf("drips.c: %v", err)
	}
}

// End to end: the leaks verb refuses a word it is not, and so does aslr.
func TestHardeningRefusesAnUnknownWord(t *testing.T) {
	t.Parallel()
	for word, want := range map[string]error{
		"aslr":  errASLRLanguage,
		"leaks": errLeakLanguage,
	} {
		err := game(t.TempDir(), word, "rust")
		if !errors.Is(err, want) {
			t.Errorf("%s: %v", word, err)
		}
	}
}

// End to end with otool and codesign: a C program as build-c builds it
// is read back by the Mach-O pass and found hardened. PIE, sane
// segments, signed for the hardened runtime, and dyld's environment
// sealed by __RESTRICT: build-c makes what harden demands.
func TestHardenReadsWhatBuildCMade(t *testing.T) {
	needs(t, "cc", "otool", "codesign")
	dir := tree(t, map[string]string{"held.c": heldC})
	if err := game(dir, "build-c", "held.c"); err != nil {
		t.Fatal(err)
	}
	found, err := hardenMach(dir, run)
	if err != nil {
		t.Fatal(err)
	}
	if count.Positive(len(found)) {
		t.Errorf("build-c's binary is not hardened: %v", found)
	}
}

// the seal holds: dyld ignores DYLD_INSERT_LIBRARIES in a program
// build-c made, so the library a test plants is never loaded. the
// program reports whether the planted constructor ran.
func TestBuildCSealsDyldsEnvironment(t *testing.T) {
	needs(t, "cc", "codesign")
	dir := tree(t, map[string]string{
		"probe.c": "#include <stdio.h>\nint main(void) { puts(\"ran\");" +
			" return 0; }\n",
		"plant.c": "#include <stdio.h>\n" +
			"__attribute__((constructor)) static void hi(void) " +
			"{ puts(\"planted\"); }\n",
	})
	if err := game(dir, "build-c", "probe.c"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(dir, "cc", "-dynamiclib", "-o", "bin/plant.dylib",
		"plant.c"); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("./bin/probe")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"DYLD_INSERT_LIBRARIES="+filepath.Join(dir, "bin", "plant.dylib"))
	said, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(said), "planted") {
		t.Errorf("the planted library ran: %q", said)
	}
	if !strings.Contains(string(said), "ran") {
		t.Errorf("the program did not: %q", said)
	}
}
