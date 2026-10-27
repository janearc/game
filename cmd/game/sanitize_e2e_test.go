package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// harmless is a fuzz target with nothing to find.
const harmless = "#include <stddef.h>\n#include <stdint.h>\n" +
	"int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {\n" +
	"\t(void)data;\n\t(void)size;\n\treturn 0;\n}\n"

// planted is a fuzz target that crashes on input starting FUZ.
const planted = "#include <stddef.h>\n#include <stdint.h>\n#include <stdlib.h>\n" +
	"int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {\n" +
	"\tif (size >= 3 && data[0] == 0x46 && data[1] == 0x55 && " +
	"data[2] == 0x5a) {\n\t\tabort();\n\t}\n\treturn 0;\n}\n"

// objcTarget is an Objective-C fuzz target with nothing to find.
const objcTarget = "#import <Foundation/Foundation.h>\n#include <stdint.h>\n" +
	"int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {\n" +
	"\t@autoreleasepool {\n\t\tNSData *d = [NSData dataWithBytes:data " +
	"length:size];\n\t\t(void)[[NSString alloc] initWithData:d " +
	"encoding:NSUTF8StringEncoding];\n\t}\n\treturn 0;\n}\n"

// racy has two threads writing one int with nothing between them.
const racy = "#include <pthread.h>\nstatic int shared;\n" +
	"static void *bump(void *arg) {\n\t(void)arg;\n\tshared++;\n" +
	"\treturn NULL;\n}\nint main(void) {\n\tpthread_t a, b;\n" +
	"\tpthread_create(&a, NULL, bump, NULL);\n" +
	"\tpthread_create(&b, NULL, bump, NULL);\n" +
	"\tpthread_join(a, NULL);\n\tpthread_join(b, NULL);\n\treturn 0;\n}\n"

// guarded is racy with a mutex around the write.
const guarded = "#include <pthread.h>\nstatic int shared;\n" +
	"static pthread_mutex_t lock = PTHREAD_MUTEX_INITIALIZER;\n" +
	"static void *bump(void *arg) {\n\t(void)arg;\n" +
	"\tpthread_mutex_lock(&lock);\n\tshared++;\n" +
	"\tpthread_mutex_unlock(&lock);\n\treturn NULL;\n}\n" +
	"int main(void) {\n\tpthread_t a, b;\n" +
	"\tpthread_create(&a, NULL, bump, NULL);\n" +
	"\tpthread_create(&b, NULL, bump, NULL);\n" +
	"\tpthread_join(a, NULL);\n\tpthread_join(b, NULL);\n" +
	"\treturn shared == 2 ? 0 : 1;\n}\n"

// game runs a game verb in dir, as main would.
func game(dir string, args ...string) error {
	verb, rest := count.First, count.Next(count.First)
	return verbs[args[verb]](repoAt(dir), blankConfig(), args[rest:])
}

// End to end with libFuzzer: C and Objective-C targets with nothing to
// find pass; a planted crash is found and its input kept.
func TestFuzzFindsWhatIsPlanted(t *testing.T) {
	needs(t, "cc", "xcrun")
	if fuzzRuntime(filepath.Glob) == "" {
		t.Skip("no libFuzzer runtime on this machine")
	}
	dir := tree(t, map[string]string{"harmless.c": harmless,
		"planted.c": planted, "objc.m": objcTarget})
	for _, source := range []string{"harmless.c", "objc.m"} {
		if err := game(dir, "fuzz", "c", "-time", "2", source); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
	err := game(dir, "fuzz", "c", "-time", "60", "planted.c")
	found, _ := filepath.Glob(filepath.Join(dir, "bin", ".fuzz",
		"planted-fuzz", "crash-*"))
	if err == nil || len(found) != count.One {
		t.Errorf("planted.c: %v, kept %q", err, found)
	}
}

// End to end with the thread sanitizer: a race in C and in Objective-C is
// found; the guarded program passes.
func TestRaceFindsWhatIsPlanted(t *testing.T) {
	needs(t, "cc", "xcrun")
	dir := tree(t, map[string]string{"racy.c": racy, "racy.m": racy,
		"guarded.c": guarded})
	for _, source := range []string{"racy.c", "racy.m"} {
		err := game(dir, "race", "c", "-o", strings.ReplaceAll(source, ".",
			"-"), source)
		if err == nil || !strings.Contains(err.Error(), "data race") {
			t.Errorf("%s: %v", source, err)
		}
	}
	if err := game(dir, "race", "c", "guarded.c"); err != nil {
		t.Errorf("guarded.c: %v", err)
	}
}

// End to end with xcodebuild: Swift and Objective-C tested under the
// thread sanitizer pass.
func TestRaceXcodePassesSwiftAndObjectiveC(t *testing.T) {
	needs(t, "xcodebuild", "xcrun")
	dir := tree(t, pair)
	if err := game(dir, "race", "xcode", "-scheme", "Pair-Package"); err != nil {
		t.Errorf("Pair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin")); errors.Is(err,
		os.ErrNotExist) {
		t.Error("nothing was built")
	}
}
