package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

// TestAslrGoWithNoCmdSaysSoAndBuildsNothing: no cmd/ is refused by
// saying so, with no bin/ made and no tool run.
func TestAslrGoWithNoCmdSaysSoAndBuildsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := NewMockcommandRunner(gomock.NewController(t))
	err := aslrGoBy(dir, nil, stand.Run, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no cmd/ directory") {
		t.Errorf("no cmd: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bin")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("a bin/ was made for nothing: %v", statErr)
	}
}

// TestAslrGoWithABinThatIsAFileBuildsNothing: a bin/ that cannot be made
// is the error of making it, before any build.
func TestAslrGoWithABinThatIsAFileBuildsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "bin"), "a file\n")
	stand := NewMockcommandRunner(gomock.NewController(t))
	err := aslrGoBy(dir, nil, stand.Run, &bytes.Buffer{})
	if !errors.Is(err, syscall.EEXIST) && !errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("bin that is a file: %v", err)
	}
}

// TestAslrCWithAFlagItDoesNotKnowBuildsNothing: the flag's error is the
// answer, and nothing is built from what followed it.
func TestAslrCWithAFlagItDoesNotKnowBuildsNothing(t *testing.T) {
	t.Parallel()
	stand := NewMockcommandRunner(gomock.NewController(t))
	var out, errs bytes.Buffer
	err := aslrCBy(t.TempDir(), []string{"-nonesuch", "t.c"}, stand.Run, noFetch, &out, &errs)
	if !errors.Is(err, errForeignFlags) {
		t.Errorf("a bad flag: %v", err)
	}
}

// visited is what visit says of an entry at path that is a directory or
// not, and whether it kept the path.
func visited(t *testing.T, bin, path string, isDir bool) (error, bool) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := fs.FileInfoToDirEntry(info)
	if entry.IsDir() != isDir {
		t.Fatalf("%s: directory is %t, should be %t", path, entry.IsDir(), isDir)
	}
	find := &findingMachO{bin: bin, relative: filepath.Rel}
	got := find.visit(path, entry, nil)
	return got, count.Positive(len(find.out))
}

// TestVisitSkipsAHiddenFolderWholeAndPassesAHiddenFile: a hidden folder
// is SkipDir; a hidden file is passed, whatever it holds.
func TestVisitSkipsAHiddenFolderWholeAndPassesAHiddenFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	hidden := filepath.Join(bin, ".deps")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	file := writeMach(t, dir, "bin/.hidden-file")
	if got, kept := visited(t, bin, hidden, true); got != filepath.SkipDir || kept {
		t.Errorf("hidden folder: %v, kept %t", got, kept)
	}
	if got, kept := visited(t, bin, file, false); got != nil || kept {
		t.Errorf("hidden file: %v, kept %t", got, kept)
	}
}

// TestVisitKeepsAMachOFileAndPassesAFolder: a Mach-O file is kept, and a
// folder is entered, not kept, even where its path names a Mach-O file.
func TestVisitKeepsAMachOFileAndPassesAFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	file := writeMach(t, dir, "bin/app")
	if got, kept := visited(t, bin, file, false); got != nil || !kept {
		t.Errorf("a Mach-O file: %v, kept %t", got, kept)
	}
	find := &findingMachO{bin: bin, relative: filepath.Rel}
	folder := fs.FileInfoToDirEntry(dirInfo{file})
	if got := find.visit(file, folder, nil); got != nil || count.Positive(len(find.out)) {
		t.Errorf("a folder at a Mach-O path: %v, kept %v", got, find.out)
	}
}

// dirInfo is the info of a folder that stands at a path.
type dirInfo struct{ path string }

func (dirInfo) Name() string       { return "app" }
func (dirInfo) Size() int64        { return count.Nothing }
func (dirInfo) Mode() fs.FileMode  { return fs.ModeDir | dirBits }
func (dirInfo) ModTime() time.Time { return time.Time{} }
func (dirInfo) IsDir() bool        { return true }
func (dirInfo) Sys() any           { return nil }

// dirBits is the permission bits a folder is shown with, as 0755 is defined
var dirBits = fs.FileMode(0o755) // defined by sys/stat.h: the owner writes, all read and enter
