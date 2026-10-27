package repo_test

import (
	"errors"
	"testing"

	"github.com/janearc/game/internal/repo"
	"go.uber.org/mock/gomock"
)

func mocked(t *testing.T) (repo.Repo, *MockRunner) {
	t.Helper()
	runner := NewMockRunner(gomock.NewController(t))
	return repo.At("/work", runner), runner
}

func TestGitAsksTheRunnerInTheRepoDirectory(t *testing.T) {
	t.Parallel()
	here, runner := mocked(t)
	runner.EXPECT().Run("/work", "", nil, []string{"rev-parse", "HEAD"}).Return("abc\n", nil)
	got, err := here.Git("rev-parse", "HEAD")
	if err != nil || got != "abc" {
		t.Fatalf("got %q, %v; want abc, trimmed", got, err)
	}
}

func TestGitCarriesStdinAndEnvToTheRunner(t *testing.T) {
	t.Parallel()
	here, runner := mocked(t)
	runner.EXPECT().Run("/work", "body", []string{"A=b"}, []string{"hash-object"}).Return("h", nil)
	if _, err := here.GitEnvIn([]string{"A=b"}, "body",
		"hash-object"); err != nil {
		t.Fatal(err)
	}
}

func TestGitPassesTheRunnersRefusalOn(t *testing.T) {
	t.Parallel()
	here, runner := mocked(t)
	runner.EXPECT().Run(anything, anything, anything, anything).Return("", errors.New("no"))
	if _, err := here.Git("status"); err == nil {
		t.Fatal("a refusal from the runner was lost")
	}
}

func TestBlobKeepsEveryByteTheRunnerGave(t *testing.T) {
	t.Parallel()
	here, runner := mocked(t)
	runner.EXPECT().Run("/work", "", nil, []string{"cat-file", "blob", "obj"}).Return(" body \n\n", nil)
	got, err := here.Blob("obj")
	if err != nil || got != " body \n\n" {
		t.Fatalf("blob %q, %v; want every byte", got, err)
	}
}

func TestARepoWithNoDirectoryAsksTheRunnerNothing(t *testing.T) {
	t.Parallel()
	runner := NewMockRunner(gomock.NewController(t))
	if _, err := repo.At("", runner).Git("status"); err == nil {
		t.Fatal("git ran with no directory")
	}
}

func TestARepoWithNoRunnerRefusesInsteadOfPanicking(t *testing.T) {
	t.Parallel()
	if _, err := repo.At("/work", nil).Git("status"); err == nil {
		t.Fatal("git ran with no runner")
	}
}
