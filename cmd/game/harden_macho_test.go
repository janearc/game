package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// stackExecHeader is a PIE executable that still lets the stack run.
const stackExecHeader = "MH_MAGIC_64    ARM64        ALL  0x00     EXECUTE    " +
	"16        744   NOUNDEFS DYLDLINK TWOLEVEL PIE ALLOW_STACK_EXECUTION"

// rwxSegs is otool -l with one segment writable and executable at once.
const rwxSegs = "segname __TEXT\n   vmaddr 0x0\n" +
	"  maxprot 0x00000007\n initprot 0x00000005\n"

// cleanSegs is otool -l as a hardened binary keeps its segments.
const cleanSegs = "segname __TEXT\n  maxprot 0x00000005\n" +
	"segname __DATA\n  maxprot 0x00000003\n"

func TestHeaderFindingsReadTheFlag(t *testing.T) {
	t.Parallel()
	if got := headerFindings(pieHeader, "bin/app"); count.Positive(len(got)) {
		t.Errorf("pie: %v", got)
	}
	got := headerFindings(noPieHeader, "bin/app")
	if !onlySaying(got, "executable is not position-independent") {
		t.Errorf("no pie: %v", got)
	}
	got = headerFindings(stackExecHeader, "bin/app")
	if !onlySaying(got, "allows code on the stack") {
		t.Errorf("stack exec: %v", got)
	}
}

func TestSegmentFindingsFindWritableExecutable(t *testing.T) {
	t.Parallel()
	if got := segmentFindings(cleanSegs, "bin/app"); count.Positive(len(got)) {
		t.Errorf("clean: %v", got)
	}
	got := segmentFindings(rwxSegs, "bin/app")
	if !onlySaying(got, "__TEXT is writable and executable") {
		t.Errorf("rwx: %v", got)
	}
}

func TestEntitlementFindingsFindTheWeakeningOnes(t *testing.T) {
	t.Parallel()
	if got := entitlementFindings("<dict/>\n", "bin/app"); count.Positive(len(got)) {
		t.Errorf("none: %v", got)
	}
	got := entitlementFindings(
		"<dict><key>com.apple.security.get-task-allow</key><true/></dict>\n",
		"bin/app")
	if !onlySaying(got,
		"entitlement get-task-allow weakens the hardened runtime") {
		t.Errorf("one: %v", got)
	}
	denied := "<dict>\n\t<key>com.apple.security.cs.allow-jit</key>\n" +
		"\t<false/>\n</dict>\n"
	if got := entitlementFindings(denied, "bin/app"); count.Positive(len(got)) {
		t.Errorf("set false: %v", got)
	}
	spaced := "<key>com.apple.security.cs.allow-jit</key>\n\t<true/>\n"
	if got := entitlementFindings(spaced, "bin/app"); !onlyHolding(got,
		"allow-jit") {
		t.Errorf("set true across lines: %v", got)
	}
}

func TestHardenOneCollectsTheLayers(t *testing.T) {
	t.Parallel()
	wants := []string{
		"not position-independent", "writable and executable", "get-task-allow",
		"not the hardened runtime", "__RESTRICT",
	}
	got := hardenOne(weakBinary{}.run, "dir", "bin/app")
	if len(got) != len(wants) {
		t.Errorf("got %d findings: %v", len(got), got)
	}
	for _, want := range wants {
		if !hasFinding(got, "bin/app", want) {
			t.Errorf("no %q in %v", want, got)
		}
	}
}

func TestHardenMachWithNoBinIsClean(t *testing.T) {
	t.Parallel()
	got, err := hardenMach(t.TempDir(), (&tools{}).run)
	if err != nil || got != nil {
		t.Errorf("%v %v", got, err)
	}
}

func TestMachoHeadsRefuseJunk(t *testing.T) {
	t.Parallel()
	if _, err := machoHeads("hello\n"); err != errNoHeader {
		t.Errorf("junk: %v", err)
	}
}

// hasFinding helps harden_macho tests reach into lint findings.
func hasFinding(found []lint.Finding, file, text string) bool {
	for _, finding := range found {
		if finding.File() == file && finding.Rule() == "harden" &&
			strings.Contains(finding.Text(), text) {
			return true
		}
	}
	return false
}

// onlySaying is whether found is one finding saying text.
func onlySaying(found []lint.Finding, text string) bool {
	return len(found) == count.One && found[count.First].Text() == text
}

// onlyHolding is whether found is one finding holding text.
func onlyHolding(found []lint.Finding, text string) bool {
	return len(found) == count.One &&
		strings.Contains(found[count.First].Text(), text)
}

// verbOf is the first word a tool is asked, or none.
func verbOf(args []string) string {
	if count.None(len(args)) {
		return ""
	}
	return args[count.First]
}

// weakBinary answers otool and codesign as a binary that is weak in
// every layer.
type weakBinary struct{}

// run is the runner.
func (weakBinary) run(_, name string, args ...string) (string, error) {
	switch {
	case name == "otool" && verbOf(args) == "-hv":
		return noPieHeader, nil
	case name == "otool" && verbOf(args) == "-l":
		return rwxSegs, nil
	case name == "codesign":
		return "<dict><key>get-task-allow</key><true/></dict>\n", nil
	}
	return "", nil
}

// adhocBinary answers as a clean binary signed ad hoc, and keeps which
// binary codesign was asked about.
type adhocBinary struct{ signed string }

// run is the runner.
func (binary *adhocBinary) run(_, name string, args ...string) (string,
	error) {
	switch {
	case name == "otool" && verbOf(args) == "-hv":
		return pieHeader, nil
	case name == "otool" && verbOf(args) == "-l":
		return cleanSegs, nil
	case name == "codesign" && slices.Contains(args, "--entitlements"):
		return "<dict/>\n", nil
	case name == shell:
		binary.signed = args[count.Last(len(args))]
		return "flags=0x20002(adhoc,linker-signed)", nil
	}
	return "", nil
}

func TestRuntimeFindingsReadTheSignature(t *testing.T) {
	t.Parallel()
	if got := runtimeFindings("flags=0x10000(runtime) hashes=1", "bin/app"); count.Positive(len(got)) {
		t.Errorf("runtime: %v", got)
	}
	adhocRuntime := "CodeDirectory v=20500 size=270 " +
		"flags=0x10002(adhoc,runtime) hashes=2+2 location=embedded\n"
	if got := runtimeFindings(adhocRuntime, "bin/app"); count.Positive(len(got)) {
		t.Errorf("ad hoc with the runtime: %v", got)
	}
	if got := runtimeFindings("no flags here", "bin/app"); !onlySaying(got,
		"not the hardened runtime") {
		t.Errorf("no flags: %v", got)
	}
	got := runtimeFindings("flags=0x20002(adhoc,linker-signed)", "bin/app")
	if !onlyHolding(got, "ad-hoc") {
		t.Errorf("adhoc: %v", got)
	}
}

func TestRestrictFindingReadsTheSegment(t *testing.T) {
	t.Parallel()
	restricted := "segname __RESTRICT\n  maxprot 0x00000000\n"
	if got := restrictFinding(restricted, "bin/app"); count.Positive(len(got)) {
		t.Errorf("restricted: %v", got)
	}
	if got := restrictFinding(cleanSegs, "bin/app"); !onlyHolding(got,
		"__RESTRICT") {
		t.Errorf("not restricted: %v", got)
	}
}

// hardenOne holds a native binary to the runtime and dyld checks, and
// lets a Go binary off those two. the Go binary is this test's own,
// which carries build info; the native one is a path that is not there,
// which carries none.
func TestHardenOneNativeAndGo(t *testing.T) {
	t.Parallel()
	signing := &adhocBinary{}
	got := hardenOne(signing.run, "dir", "bin/app")
	for _, want := range []string{"ad-hoc", "__RESTRICT"} {
		if !hasFinding(got, "bin/app", want) {
			t.Errorf("native: no %q in %v", want, got)
		}
	}
	if signing.signed != "bin/app" {
		t.Errorf("codesign was asked about %q", signing.signed)
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	got = hardenOne(signing.run, filepath.Dir(self), filepath.Base(self))
	if count.Positive(len(got)) {
		t.Errorf("go binary was held to native checks: %v", got)
	}
}

// isGoBinary reads build info rather than asking go version, which
// exits 0 for any file at all.
func TestIsGoBinaryReadsBuildInfo(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if !isGoBinary(filepath.Dir(self), filepath.Base(self)) {
		t.Error("the test binary is not Go")
	}
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(dir, "text"), []byte("hi\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	if isGoBinary(dir, "text") {
		t.Error("a text file is Go")
	}
	if isGoBinary(dir, "missing") {
		t.Error("a missing file is Go")
	}
}
