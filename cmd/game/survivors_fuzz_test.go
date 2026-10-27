package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"go.uber.org/mock/gomock"
)

// TestFuzzWithNoTimeRunsThirtySeconds: the default is thirty seconds.
func TestFuzzWithNoTimeRunsThirtySeconds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	commands.EXPECT().Run(dir, "xcrun", "--show-sdk-path").Return("/sdk\n", nil)
	commands.EXPECT().Run(dir, "cc", gomock.Any()).Return("", nil)
	commands.EXPECT().Run(dir, "bin/t-fuzz", "-max_total_time=30",
		"-artifact_prefix=bin/.fuzz/t-fuzz/", "bin/.fuzz/t-fuzz/corpus").Return("", nil)
	var out, errs bytes.Buffer
	err := fuzzBy(dir, []string{"c", "-runtime", "/fz.a", "t.c"}, commands.Run,
		noFetch, globs{}.glob, &out, &errs)
	if err != nil || out.String() != "fuzzed bin/t-fuzz for 30 s: nothing found\n" {
		t.Errorf("err %v, said %q", err, out.String())
	}
}

// TestFuzzWithABinThatIsAFileRunsNothing: a bin/ that cannot be made is
// the error, before the SDK is asked for.
func TestFuzzWithABinThatIsAFileRunsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "a file\n")
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := fuzzBy(dir, []string{"c", "-runtime", "/fz.a", "t.c"}, commands.Run,
		noFetch, globs{}.glob, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil {
		t.Error("a bin that is a file was built into")
	}
}

// TestTextWordsWantsTheDashesAfterTheFile: the file, then --, then the
// test command; a word where the dashes belong is no test command, even
// when words follow it.
func TestTextWordsWantsTheDashesAfterTheFile(t *testing.T) {
	t.Parallel()
	file, test, err := textWords([]string{"f.c", "--", "make", "check"})
	if err != nil || file != "f.c" || !slices.Equal(test, []string{"make", "check"}) {
		t.Errorf("a whole command: %q %v %v", file, test, err)
	}
	for _, words := range [][]string{
		nil, {"f.c"}, {"f.c", "--"}, {"f.c", "make", "check"}, {"f.c", "-", "make"},
	} {
		if _, _, err := textWords(words); !errors.Is(err, errNoTestCommand) {
			t.Errorf("%q: %v", words, err)
		}
	}
}
