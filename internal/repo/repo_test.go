package repo_test

import (
	"github.com/janearc/game/internal/repo"
	"strings"
	"testing"
)

func TestGitTrimsItsOutput(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	got, err := work.Git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || got != "main" {
		t.Errorf("got %q, %v, want main", got, err)
	}
}

func TestGitErrorNamesTheCommandAndGitsWords(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	_, err := work.Git("rev-parse", "no-such-rev")
	if err == nil || !strings.HasPrefix(err.Error(),
		"git rev-parse no-such-rev: ") {
		t.Errorf("the error does not name the command: %v", err)
	}
}

func TestGitInGivesGitItsStdin(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	got, err := work.GitIn("hello\n", "hash-object", "--stdin")
	// the blob hash of "hello\n", which git computes the same everywhere.
	if want := "ce013625030ba8dba906f756967f9e9ca394464a"; err != nil ||
		got != want {
		t.Errorf("got %q, %v, want %q", got, err, want)
	}
}

func TestGitInDoesNotHashWhatItWasNotGiven(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	got, _ := work.GitIn("other\n", "hash-object", "--stdin")
	if got == "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Error("two bodies hashed alike")
	}
}

func TestGitInAndEnvRefuseTheWayGitDoes(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	_, in := work.GitIn("", "rev-parse", "no-such-rev")
	_, env := work.GitEnv(nil, "rev-parse", "no-such-rev")
	_, both := work.GitEnvIn(nil, "", "no-such-verb")
	for name, err := range map[string]error{"GitIn": in, "GitEnv": env,
		"GitEnvIn": both} {
		if err == nil || !strings.HasPrefix(err.Error(), "git ") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestARepoWithADirectoryStillAsksGit(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	if work.Dir() != "/work" {
		t.Fatalf("directory %q", work.Dir())
	}
	if _, err := work.Git("rev-parse", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if got := work.Path("a", "b"); got != "/work/a/b" {
		t.Errorf("path %q", got)
	}
}

// shortLength is defined by git: seven characters of a hash by default.
const shortLength = 7

func TestShortKeepsTheDefaultGitPrefix(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("abcdef", len("commit"))
	short := repo.Short(commit)
	if short == commit || !strings.HasPrefix(commit, short) ||
		len(short) != shortLength {
		t.Fatalf("short hash %q of %q", short, commit)
	}
}
