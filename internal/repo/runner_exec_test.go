package repo_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// whatGitWouldBe is the name this test binary answers to when it is
// started as git: TestMain makes the binary behave as the git command
// for the tests of Exec, so no git is started.
const whatGitWouldBe = "git"

// exitStatus is defined by unix: any non-zero status is a refusal.
const exitStatus = 1

// stdinSize is defined by the stand-in: it echoes at most this much.
const stdinSize = 64

func TestMain(suite *testing.M) {
	if filepath.Base(os.Args[count.First]) == whatGitWouldBe {
		standIn(os.Args[count.One:])
	}
	os.Exit(suite.Run())
}

// standIn says and does what its arguments ask, then ends the process.
func standIn(args []string) {
	switch args[count.First] {
	case "fail":
		os.Stderr.WriteString("it said no\n")
		os.Exit(exitStatus)
	case "env":
		os.Stdout.WriteString(os.Getenv("ASKED") + "|" + os.Getenv("HOME"))
	case "dir":
		here, _ := os.Getwd()
		os.Stdout.WriteString(here)
	default:
		in := make([]byte, stdinSize)
		got, _ := os.Stdin.Read(in)
		os.Stdout.Write(in[:got])
	}
	os.Exit(count.Nothing)
}

// asGit puts this test binary first on PATH under the name git.
func asGit(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, whatGitWouldBe)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestExecGivesStdinAndReturnsStdout(t *testing.T) {
	asGit(t)
	out, err := repo.Exec().Run("", "hello", nil, []string{"echo"})
	if err != nil || out != "hello" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestExecAddsEnvironmentToItsOwn(t *testing.T) {
	asGit(t)
	out, err := repo.Exec().Run("", "", []string{"ASKED=yes"},
		[]string{"env"})
	if err != nil || out != "yes|"+os.Getenv("HOME") {
		t.Fatalf("got %q, %v; want yes and the rest of the environment",
			out, err)
	}
}

func TestExecStartsInTheDirectoryGiven(t *testing.T) {
	asGit(t)
	dir := t.TempDir()
	out, err := repo.Exec().Run(dir, "", nil, []string{"dir"})
	if err != nil || !strings.HasSuffix(out, strings.TrimPrefix(dir, "/private")) {
		t.Fatalf("ran in %q, %v, want %q", out, err, dir)
	}
}

func TestExecRefusalNamesTheCommandAndStderr(t *testing.T) {
	asGit(t)
	_, err := repo.Exec().Run("", "", nil, []string{"fail", "x"})
	if err == nil || err.Error() != "git fail x: it said no" {
		t.Fatalf("got %v", err)
	}
}

func TestExecWithNoGitOnThePathIsRefused(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := repo.Exec().Run("", "", nil, []string{"status"}); err == nil {
		t.Fatal("a git that is not there ran")
	}
}
