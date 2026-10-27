package main

import (
	"archive/tar"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// TestPlaceSaysWhenItCannotMakeTheFolder: a file's folder that cannot be
// made is the error of making it, not of opening the file in it.
func TestPlaceSaysWhenItCannotMakeTheFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "under"), "a file\n")
	err := place(strings.NewReader("x"), &tar.Header{Typeflag: tar.TypeReg},
		filepath.Join(dir, "under", "deeper", "f.c"))
	var failed *fs.PathError
	if !errors.As(err, &failed) || failed.Op != "mkdir" {
		t.Errorf("a folder under a file: %#v", err)
	}
}

// TestPlaceSaysWhenItCannotOpenTheFile: a file whose place is a folder is
// the error of opening it.
func TestPlaceSaysWhenItCannotOpenTheFile(t *testing.T) {
	t.Parallel()
	err := place(strings.NewReader(""), &tar.Header{Typeflag: tar.TypeReg},
		t.TempDir())
	if !errors.Is(err, syscall.EISDIR) {
		t.Errorf("a file where a folder is: %v", err)
	}
}

// TestPlaceWritesTheFileAndItsFolders: the anti-test: a file in a folder
// not yet there is written whole.
func TestPlaceWritesTheFileAndItsFolders(t *testing.T) {
	t.Parallel()
	to := filepath.Join(t.TempDir(), "a", "b", "f.c")
	if err := place(strings.NewReader("int x;\n"), &tar.Header{Typeflag: tar.TypeReg}, to); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(to); err != nil || string(body) != "int x;\n" {
		t.Errorf("wrote %q, %v", body, err)
	}
}

// TestADownloadHasATimeoutOfNoMoreThanTwoMinutes: a fetch that waits for
// ever is a build that never ends, and two minutes is the most it waits.
func TestADownloadHasATimeoutOfNoMoreThanTwoMinutes(t *testing.T) {
	t.Parallel()
	if !count.Positive(int(fetchTimeout)) || fetchTimeout > twoMinutes {
		t.Errorf("a download may take %v", fetchTimeout)
	}
}

// twoMinutes is the longest a download may wait.
const twoMinutes = 2 * time.Minute // defined here: the most one download may take
