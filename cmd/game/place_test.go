package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// Xcode measures a folder unless it is under the folders the system gives
// each user for temporary files. names that only begin the same are other
// folders.
func TestWhereXcodeMeasuresIsEveryFolderButTheSystemsTempFolders(t *testing.T) {
	t.Parallel()
	for each, want := range map[string]bool{
		"/opt/mtmp":                     true,
		"/tmp":                          true,
		"/var/foldersnot/x":             true,
		"/var/folder/x":                 true,
		"/private/var/foldersnot/x":     true,
		"/var/folders/ab/xyz/T":         false,
		"/var/folders/ab/xyz/T/":        false,
		"/private/var/folders/ab/xyz/T": false,
		"/var/folders":                  false,
		"":                              false,
	} {
		if got := measured(each); got != want {
			t.Errorf("measured(%q) = %v, want %v", each, got, want)
		}
	}
}

// the copies go in TMPDIR when it is set to a folder Xcode measures, and
// the cache lookup is not even asked: the person who set it is obeyed
// with no home to find a cache in.
func TestAnExplicitTempFolderIsHonoured(t *testing.T) {
	explicit := filepath.Join(userFolder(t), "mtmp")
	t.Setenv("TMPDIR", explicit)
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	got, err := chosenPlace()
	if err != nil || got != explicit {
		t.Errorf("TMPDIR %s gave %s, %v", explicit, got, err)
	}
}

// userFolder is a folder outside the system's temp folders, made for the
// test and removed with it.
func userFolder(t *testing.T) string {
	t.Helper()
	return stopPlace(t)
}

// the copies go in the user's cache folder under game/mutants. they do
// when TMPDIR is under the system's temp folders or is not set. those are
// the folders whose sources Xcode does not instrument.
func TestTheSystemsTempFolderGivesWayToTheCache(t *testing.T) {
	home := userFolder(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, "game", "mutants")
	if !strings.HasPrefix(want, home) {
		t.Fatalf("the cache %s is not under the home %s", want, home)
	}
	for _, tmp := range []string{"/var/folders/ab/xyz/T/",
		"/private/var/folders/ab/xyz/T", ""} {
		t.Setenv("TMPDIR", tmp)
		got, err := chosenPlace()
		if err != nil || got != want {
			t.Errorf("TMPDIR %q gave %s, %v, want %s", tmp, got, err, want)
		}
	}
}

// with TMPDIR under the system's folders and no cache to be found the run
// cannot choose. it says what to do.
func TestNoCacheAndNoTempFolderIsRefusedWithWhatToDo(t *testing.T) {
	t.Setenv("TMPDIR", "/var/folders/ab/xyz/T")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	_, err := chosenPlace()
	if err == nil || !strings.Contains(err.Error(), "set TMPDIR to a folder") {
		t.Errorf("no place to be found: %v", err)
	}
	_, err = mutantOptions("", "", count.One, time.Second)
	if err == nil || !strings.Contains(err.Error(), "TMPDIR") {
		t.Errorf("options with no place: %v", err)
	}
}

// the options carry the place chosen, so the run makes its copies there.
func TestTheOptionsCarryTheChosenPlace(t *testing.T) {
	explicit := filepath.Join(userFolder(t), "mtmp")
	t.Setenv("TMPDIR", explicit)
	opts, err := mutantOptions("", "", count.One, time.Second)
	if err != nil || !strings.Contains(opts.String(), ", copies in "+explicit) {
		t.Errorf("the options say %q, %v, want copies in %s", opts.String(),
			err, explicit)
	}
}
