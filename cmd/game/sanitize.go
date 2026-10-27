package main

// game fuzz and game race: C and Objective-C programs built by cc with a
// sanitizer and run, and race for Xcode schemes.
//
// Xcode's clang has the address and thread sanitizers and libFuzzer's
// coverage instrumentation, but not libFuzzer itself, so fuzz links in
// Homebrew LLVM's libFuzzer runtime: the first found, unless -runtime
// names another.
//
// A sanitizer runtime only runs on a macOS it was built for: with a
// macOS seed, use the Xcode beta that matches it.

import (
	"errors"
	"fmt"
	"github.com/janearc/game/internal/count"
	"path/filepath"
	"slices"
	"strings"
)

// fuzzRuntimes are where game looks for libFuzzer's runtime, in order.
var fuzzRuntimes = []string{
	"/opt/homebrew/opt/llvm@20/lib/clang/*/lib/darwin/" + fuzzerArchive,
	"/opt/homebrew/opt/llvm/lib/clang/*/lib/darwin/" + fuzzerArchive,
}

// fuzzerArchive is libFuzzer's runtime as LLVM names it on a Mac.
const fuzzerArchive = "libclang_rt.fuzzer_osx.a"

// fuzzRuntime is the first libFuzzer runtime found by glob, or nothing.
func fuzzRuntime(glob func(string) ([]string, error)) string {
	for _, pattern := range fuzzRuntimes {
		found, _ := glob(pattern)
		if first, _, said := verbWord(found); said {
			return first
		}
	}
	return ""
}

// fuzzSanitize is what a fuzz build is compiled with: the address
// sanitizer, and the coverage libFuzzer steers by.
var fuzzSanitize = []string{"-fsanitize=address",
	"-fsanitize-coverage=inline-8bit-counters,indirect-calls,trace-cmp," +
		"pc-table"}

// raceSanitize is what a race build is compiled with.
var raceSanitize = []string{"-fsanitize=thread"}

// errNotCOrObjC is a source that is neither .c nor .m.
var errNotCOrObjC = errors.New("is not a .c or .m source")

// objcFlags are the flags a program with Objective-C in it is built with:
// every warning an error, and ARC. -std and -pedantic are left to C alone.
var objcFlags = []string{"-Wall", "-Wextra", "-Werror", "-fobjc-arc"}

// sanitized is what a sanitized build is asked to make.
type sanitized struct {
	name, suffix, sdk string
	sanitize, sources []string
	add               linked
}

// sanitizedArgv is clang's words for ask, into bin/ under its name and
// suffix, and that path. C alone is built with cFlags; with any .m,
// objcFlags and Foundation.
//
// Libraries' headers follow the flags, and their objects and libraries
// follow the sources. The name is the first source's, without its
// extension, unless ask names it.
func sanitizedArgv(ask sanitized) ([]string, string, error) {
	first, _, said := verbWord(ask.sources)
	if !said {
		return nil, "", errNoSources
	}
	objc := false
	for _, source := range ask.sources {
		switch filepath.Ext(source) {
		case ".c":
		case ".m":
			objc = true
		default:
			return nil, "", fmt.Errorf("%s %w", source,
				errNotCOrObjC)
		}
	}
	name := ask.name
	if name == "" {
		base := filepath.Base(first)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return nil, "", fmt.Errorf("%q %w", name, errBadName)
	}
	program := filepath.Join("bin", name+"-"+ask.suffix)
	flags := withoutOptimising(cFlags)
	var frameworks []string
	if objc {
		flags = objcFlags
		frameworks = []string{"-framework", "Foundation"}
	}
	return slices.Concat(flags, []string{"-g", "-O1"}, ask.sanitize,
		[]string{"-isysroot", ask.sdk}, ask.add.includes, ask.sources,
		ask.add.objects, ask.add.libs, frameworks,
		[]string{"-o", program}), program, nil
}

// withoutOptimising is flags without -O2, since a sanitized build sets
// its own level.
func withoutOptimising(flags []string) []string {
	kept := make([]string, count.Nothing, len(flags))
	for _, flag := range flags {
		if flag != "-O2" {
			kept = append(kept, flag)
		}
	}
	return kept
}
