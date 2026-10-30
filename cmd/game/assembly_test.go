package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// walked is a runner that notes the directory and the command of every
// call, and refuses the calls that hold fail.
type walked struct {
	said []string
	dirs []string
	fail string
}

// run notes the call.
func (stand *walked) run(dir, name string, args ...string) (string, error) {
	line := filepath.Base(dir) + ": " + name + " " + strings.Join(args, " ")
	stand.said = append(stand.said, line)
	stand.dirs = append(stand.dirs, dir)
	if stand.fail != "" && strings.Contains(line, stand.fail) {
		return "", errors.New("no: " + line)
	}
	return "", nil
}

// assembled is a repository with one commit, as the assembly of a stack is.
func assembled(t *testing.T) (string, walked) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "x")
	gitTree(t, dir, map[string]string{"puzzlemouth/a.txt": "a\n",
		"okapi/sub/b.txt": "b\n", "a/c.txt": "c\n", "b/d.txt": "d\n"})
	return dir, walked{}
}

// TestStackBuildsVetsAndTestsEveryModuleOfTheAssembly: vet then test, in
// the directory of each module of the unpacked commit.
func TestStackBuildsVetsAndTestsEveryModuleOfTheAssembly(t *testing.T) {
	t.Parallel()
	dir, stand := assembled(t)
	modules := []string{"puzzlemouth", "okapi/sub"}
	err := stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
		"HEAD", modules)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"puzzlemouth: go vet ./...", "puzzlemouth: go test ./...",
		"sub: go vet ./...", "sub: go test ./...",
	}
	if !slices.Equal(stand.said, want) {
		t.Errorf("asked %q, want %q", stand.said, want)
	}
	for at, module := range []string{"puzzlemouth", "okapi/sub"} {
		if !strings.HasSuffix(stand.dirs[at*2], module) {
			t.Errorf("%s was run in %s", module, stand.dirs[at*2])
		}
	}
}

// TestStackBuildsStopsAtAModuleThatDoesNotVet: nothing runs after it.
func TestStackBuildsStopsAtAModuleThatDoesNotVet(t *testing.T) {
	t.Parallel()
	dir, stand := assembled(t)
	stand.fail = "go vet"
	err := stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
		"HEAD", []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "a: go vet") {
		t.Fatalf("got %v, want the vet refusal", err)
	}
	if want := []string{"a: go vet ./..."}; !slices.Equal(stand.said, want) {
		t.Errorf("asked %q, want %q", stand.said, want)
	}
}

// TestStackBuildsStopsAtAModuleWhoseTestsFail: the next module is not run.
func TestStackBuildsStopsAtAModuleWhoseTestsFail(t *testing.T) {
	t.Parallel()
	dir, stand := assembled(t)
	stand.fail = "go test"
	err := stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
		"HEAD", []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "a: go test") {
		t.Fatalf("got %v, want the test refusal", err)
	}
	want := []string{"a: go vet ./...", "a: go test ./..."}
	if !slices.Equal(stand.said, want) {
		t.Errorf("asked %q, want %q", stand.said, want)
	}
}

// TestStackBuildsAsksNothingOfAStackWithNoModule: no unpack, no go.
func TestStackBuildsAsksNothingOfAStackWithNoModule(t *testing.T) {
	t.Parallel()
	stand := &walked{}
	err := stackBuilds(repoAt(t.TempDir()), blankConfig(), stand.run,
		io.Discard, "abc", nil)
	if err != nil || len(stand.said) != 0 {
		t.Fatalf("err %v, asked %q", err, stand.said)
	}
}

// TestStackBuildsRefusesACommitItCannotUnpack: nothing is vetted.
func TestStackBuildsRefusesACommitItCannotUnpack(t *testing.T) {
	t.Parallel()
	dir, stand := assembled(t)
	missing := strings.Repeat("0", len(fakeObject))
	err := stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
		missing, []string{"a"})
	if err == nil || !strings.HasPrefix(err.Error(), "the assembly "+missing) {
		t.Fatalf("got %v, want the archive refusal", err)
	}
	if len(stand.said) != 0 {
		t.Errorf("asked %q of a commit that was not there", stand.said)
	}
}

// TestStackBuildsLeavesNoDirectoryBehind: gone, passed or refused.
func TestStackBuildsLeavesNoDirectoryBehind(t *testing.T) {
	t.Parallel()
	for _, fail := range []string{"", "go test"} {
		dir, stand := assembled(t)
		stand.fail = fail
		_ = stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
			"HEAD", []string{"a"})
		laid := filepath.Dir(stand.dirs[0])
		if _, err := os.Stat(laid); err == nil {
			t.Errorf("failing %q: %s was left", fail, laid)
		}
	}
}

// TestStackBuildsRefusesWithoutATempDirectory: nothing is unpacked.
func TestStackBuildsRefusesWithoutATempDirectory(t *testing.T) {
	dir, stand := assembled(t)
	gone := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TMPDIR", gone)
	err := stackBuilds(repoAt(dir), blankConfig(), stand.run, io.Discard,
		"HEAD", []string{"a"})
	if err == nil {
		t.Fatal("a stack was checked with nowhere to unpack it")
	}
}
