package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// treeGone is a tree removed when the test ends.
type treeGone string

// remove is the cleanup.
func (gone treeGone) remove() { _ = os.RemoveAll(string(gone)) }

// tree writes files into a new tree and gives its folder.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o600)
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// xcodeTree is tree, made under game's own bin/.e2e rather than the
// system temp: Xcode 27 leaves source targets under the system temp out
// of its coverage. bin/ is ignored by git, and the tree is removed after.
func xcodeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "bin", ".e2e"))
	if err != nil {
		t.Fatal(err)
	}
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(root, openDir); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "tree")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(treeGone(dir).remove)
	openFile := os.FileMode(0o600)
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// needs skips a test under -short, or on a machine without one of tools.
func needs(t *testing.T, tools ...string) {
	t.Helper()
	if testing.Short() {
		t.Skip("-short: no real tools")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("no " + tool + " on this machine")
		}
	}
}

// sign is a C test program whose tests run every line of it.
const sign = "static int sign(int x) {\n\treturn x < 0 ? -1 : 1;\n}\n" +
	"int main(void) {\n\treturn sign(3) == 1 && sign(-3) == -1 ? 0 : 1;\n}\n"

// half is a C test program with a branch its test never takes.
const half = "static int sign(int x) {\n\tif (x < 0) {\n\t\treturn -1;\n\t}\n" +
	"\treturn 1;\n}\nint main(void) {\n\treturn sign(3) == 1 ? 0 : 1;\n}\n"

// End to end with clang's coverage: a C program its tests run whole is at
// 100%, and one with a branch never taken is reported and fails.
func TestCoverCMeasuresRealPrograms(t *testing.T) {
	needs(t, "cc", "xcrun")
	dir := tree(t, map[string]string{"sign.c": sign, "half.c": half})
	if err := coverVerb(repoAt(dir), blankConfig(),
		[]string{"c", "sign.c"}); err != nil {
		t.Errorf("sign.c: %v", err)
	}
	if err := coverVerb(repoAt(dir), blankConfig(),
		[]string{"c", "half.c"}); !errors.Is(err, errNotFull) {
		t.Errorf("half.c: %v", err)
	}
}

// End to end with theft fetched: the property program is at 100%, and
// theft's own sources are not counted.
func TestCoverCMeasuresATheftProperty(t *testing.T) {
	needs(t, "cc", "xcrun")
	if _, err := httpFetch("https://codeload.github.com/"); err != nil &&
		!strings.Contains(err.Error(), "answered") {
		t.Skip("GitHub cannot be reached: " + err.Error())
	}
	prop := "#include <stdint.h>\n#include \"theft.h\"\n" +
		"static enum theft_trial_res prop(struct theft *t, void *arg) {\n" +
		"\t(void)t;\n" +
		"\treturn (*(uint32_t *)arg ^ *(uint32_t *)arg) == 0 ? " +
		"THEFT_TRIAL_PASS : THEFT_TRIAL_FAIL;\n}\n" +
		"int main(void) {\n\tstruct theft_run_config config = {\n" +
		"\t\t.name = \"self xor is zero\", .prop1 = prop,\n" +
		"\t\t.type_info = {theft_get_builtin_type_info(" +
		"THEFT_BUILTIN_uint32_t)},\n" +
		"\t\t.seed = theft_seed_of_time(), .trials = 100,\n\t};\n" +
		"\treturn theft_run(&config) == THEFT_RUN_PASS ? 0 : 1;\n}\n"
	dir := tree(t, map[string]string{"prop.c": prop})
	if err := coverVerb(repoAt(dir), blankConfig(),
		[]string{"c", "-with", "theft", "prop.c"}); err != nil {
		t.Errorf("prop.c: %v", err)
	}
}

// pair is a package with a Swift target and an Objective-C one, tested
// from Swift so that every line of both runs.
var pair = map[string]string{
	"Package.swift": "// swift-tools-version:5.9\nimport PackageDescription\n" +
		"let package = Package(name: \"Pair\", targets: [\n" +
		"  .target(name: \"Sign\"), .target(name: \"Abs\"),\n" +
		"  .testTarget(name: \"PairTests\", dependencies: [\"Sign\", \"Abs\"]),\n])\n",
	"Sources/Sign/Sign.swift": "public func sign(_ x: Int) -> Int {\n" +
		"    x < 0 ? -1 : 1\n}\n",
	"Sources/Abs/include/Abs.h": "#import <Foundation/Foundation.h>\n" +
		"@interface Abs : NSObject\n+ (NSInteger)of:(NSInteger)x;\n@end\n",
	"Sources/Abs/Abs.m": "#import \"Abs.h\"\n@implementation Abs\n" +
		"+ (NSInteger)of:(NSInteger)x {\n    return x < 0 ? -x : x;\n}\n@end\n",
	"Tests/PairTests/PairTests.swift": "import XCTest\nimport Abs\n" +
		"@testable import Sign\nfinal class PairTests: XCTestCase {\n" +
		"    func testSign() {\n        XCTAssertEqual(sign(3), 1)\n" +
		"        XCTAssertEqual(sign(-3), -1)\n    }\n" +
		"    func testAbs() {\n        XCTAssertEqual(Abs.of(-3), 3)\n" +
		"        XCTAssertEqual(Abs.of(3), 3)\n    }\n}\n",
}

// End to end with xcodebuild and xccov: Swift and Objective-C, tested
// whole, are both at 100%.
func TestCoverXcodeMeasuresSwiftAndObjectiveC(t *testing.T) {
	needs(t, "xcodebuild", "xcrun")
	dir := xcodeTree(t, pair)
	if err := coverVerb(repoAt(dir), blankConfig(),
		[]string{"xcode", "-scheme", "Pair-Package"}); err != nil {
		t.Errorf("Pair: %v", err)
	}
}
