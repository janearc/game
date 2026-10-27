package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// pieHeader is otool -hv for an executable with PIE on.
const pieHeader = "Mach header\n" +
	"      magic  cputype cpusubtype  caps    filetype ncmds sizeofcmds      flags\n" +
	"MH_MAGIC_64    ARM64        ALL  0x00     EXECUTE    16        744   " +
	"NOUNDEFS DYLDLINK TWOLEVEL PIE\n"

// noPieHeader is the same executable without PIE.
const noPieHeader = "Mach header\n" +
	"MH_MAGIC_64    ARM64        ALL  0x00     EXECUTE    16        744   " +
	"NOUNDEFS DYLDLINK TWOLEVEL\n"

// dylibHeader is a dylib, which is position-independent without the flag.
const dylibHeader = "MH_MAGIC_64    ARM64        ALL  0x00       DYLIB    16       744   " +
	"DYLDLINK TWOLEVEL NO_REEXPORTED_DYLIBS\n"

// An executable needs PIE; a dylib passes; no header is refused.
func TestPieInReadsTheHeaders(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		hv   string
		pie  bool
		want error
	}{
		"executable": {pieHeader, true, nil},
		"not pie":    {noPieHeader, false, nil},
		"dylib":      {dylibHeader, true, nil},
		"junk":       {"hello\n", false, errNoHeader},
	} {
		got, err := pieIn(each.hv)
		if got != each.pie || !errors.Is(err, each.want) {
			t.Errorf("%s: %v %v, want %v %v", name, got, err, each.pie,
				each.want)
		}
	}
}

// A fat file with one slice not pie is refused.
func TestPieInHoldsEverySlice(t *testing.T) {
	t.Parallel()
	ok, err := pieIn(pieHeader + noPieHeader)
	if err != nil || ok {
		t.Errorf("a non-pie slice passed: %v %v", ok, err)
	}
}

// A Mach-O magic is read from the file's first four bytes.
func TestIsMachOReadsTheMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mach := writeMach(t, dir, "bin")
	shutFile := os.FileMode(0o600)
	text := filepath.Join(dir, "txt")
	if err := os.WriteFile(text, []byte("not a mach-o"), shutFile); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, shutFile); err != nil {
		t.Fatal(err)
	}
	if !isMachO(mach) || isMachO(text) || isMachO(empty) {
		t.Errorf("mach %v, text %v, empty %v", isMachO(mach), isMachO(text),
			isMachO(empty))
	}
}

// writeMach writes a file whose first bytes are a Mach-O magic.
func writeMach(t *testing.T, dir, rel string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	// any of the magics will do, and a byte past it, so the file is
	// longer than the magic alone
	body := append(slices.Clone(machoMagics[count.First]), byte(count.Nothing))
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(path, body, shutFile); err != nil {
		t.Fatal(err)
	}
	return path
}

// machOBinaries finds the Mach-O files and leaves the hidden folders alone.
func TestMachOBinariesFindsOnlyTheBinaries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/app")
	writeMach(t, dir, "bin/App.app/Contents/MacOS/App")
	writeMach(t, dir, "bin/.deps/theft-x/src/a.o")
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(filepath.Join(dir, "bin", "text.txt"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	got, err := machOBinaries(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, found := range got {
		under, _ := filepath.Rel(dir, found)
		rel = append(rel, filepath.ToSlash(under))
	}
	slices.Sort(rel)
	want := []string{"bin/App.app/Contents/MacOS/App", "bin/app"}
	if !slices.Equal(rel, want) {
		t.Errorf("got %v", rel)
	}
}

// no bin/, and a bin/ with no Mach-O, are both refused.
func TestMachOBinariesRefusesNothingToCheck(t *testing.T) {
	t.Parallel()
	if _, err := machOBinaries(t.TempDir()); !errors.Is(err, errNoBinaries) {
		t.Errorf("no bin: %v", err)
	}
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.Mkdir(filepath.Join(dir, "bin"), openDir); err != nil {
		t.Fatal(err)
	}
	if _, err := machOBinaries(dir); !errors.Is(err, errNoBinaries) {
		t.Errorf("empty bin: %v", err)
	}
}

// aslr c builds as build-c does, then reads the header back.
func TestAslrCBuildsThenChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"otool": pieHeader}}
	var out, errs bytes.Buffer
	err := aslrCBy(dir, []string{"t.c"}, stand.run, noFetch, &out, &errs)
	want := []call{
		{dir, "cc", slices.Concat(cFlags, []string{"t.c"}, sealFlags,
			[]string{"-o", "bin/t"})},
		{dir, "codesign", []string{"-s", "-", "-o", "runtime", "-f", "bin/t"}},
		{dir, "otool", []string{"-hv", "bin/t"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
	if out.String() != "aslr: bin/t is PIE\n" {
		t.Errorf("said %q", out.String())
	}
}

// a binary without PIE fails the check.
func TestAslrRefusesANonPieBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"otool": noPieHeader}}
	var out, errs bytes.Buffer
	err := aslrCBy(dir, []string{"t.c"}, stand.run, noFetch, &out, &errs)
	if err == nil || !strings.Contains(err.Error(), "is not position-independent") {
		t.Errorf("err %v", err)
	}
}

// aslr with a word it does not know is refused before any tool runs.
func TestAslrRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"which":    {nil, errNoBinaries},
		"rust":     {[]string{"rust"}, errASLRLanguage},
		"c none":   {[]string{"c"}, errNoSources},
		"c header": {[]string{"c", "t.h"}, errNotC},
		"go word":  {[]string{"go", "extra"}, nil},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		err := aslrBy(t.TempDir(), each.args, stand.run, noFetch, &out, &errs)
		switch name {
		case "go word":
			if err == nil || count.Positive(len(stand.calls)) {
				t.Errorf("go word: %v, calls %+v", err, stand.calls)
			}
		default:
			if !errors.Is(err, each.want) ||
				count.Positive(len(stand.calls)) {
				t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
			}
		}
	}
}

// aslr go builds every cmd/* then checks what is in bin/.
func TestAslrGoBuildsThenChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "hello"), openDir); err != nil {
		t.Fatal(err)
	}
	writeMach(t, dir, "bin/hello")
	stand := &tools{said: map[string]string{"otool": pieHeader}}
	var out bytes.Buffer
	err := aslrGoBy(dir, nil, stand.run, &out)
	want := []call{
		{dir, "go", []string{"build", "-trimpath", "-o", "bin/hello",
			"./cmd/hello"}},
		{dir, "otool", []string{"-hv", "bin/hello"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
}

// isXcodebuild is whether the call was xcodebuild.
func isXcodebuild(asked call) bool { return asked.name == "xcodebuild" }

// readsTool is whether the call read bin/Tool's header.
func readsTool(asked call) bool {
	return asked.name == "otool" &&
		slices.Equal(asked.args, []string{"-hv", "bin/Tool"})
}

// aslr xcode builds the scheme, then checks every Mach-O it left.
func TestAslrXcodeBuildsThenChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/Tool")
	stand := &tools{said: map[string]string{"otool": pieHeader}}
	var out, errs bytes.Buffer
	err := aslrXcodeBy(dir, []string{"-scheme", "Tool"}, stand.run, noFetch,
		&out, &errs)
	if err != nil || !slices.ContainsFunc(stand.calls, isXcodebuild) ||
		!slices.ContainsFunc(stand.calls, readsTool) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
}
