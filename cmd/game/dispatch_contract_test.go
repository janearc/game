package main

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

type dispatched struct {
	calls int
	err   error
}

func (seen *dispatched) run(repo.Repo, config.Config) error {
	seen.calls++
	return seen.err
}

func TestWordlessDispatchRunsItsHandler(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	seen := &dispatched{err: errors.New("handler refused")}
	ignore := withoutWords(seen.run)
	if err := ignore(work, blankConfig(), []string{"ignored"}); !errors.Is(err, seen.err) {
		t.Fatalf("handler error lost: %v", err)
	}
	bare := bareVerb("sample", "sample handler", seen.run)
	if err := bare(work, blankConfig(), nil); !errors.Is(err, seen.err) {
		t.Fatalf("bare handler error lost: %v", err)
	}
	if seen.calls != count.Next(count.One) {
		t.Fatalf("handlers called %d times", seen.calls)
	}
}

func TestSettleExportsOnlyDeclaredValues(t *testing.T) {
	t.Setenv("GAME_CONTRACT_VALUE", "old")
	t.Setenv("GAME_CONTRACT_UNUSED", "untouched")
	cfg := configOf(t, []string{"set GAME_CONTRACT_VALUE new",
		"set GAME_CONTRACT_UNUSED not exported"},
		[]string{"needs GAME_CONTRACT_VALUE fixture setting"})
	if err := settle(cfg, "build", io.Discard); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GAME_CONTRACT_VALUE") != "new" ||
		os.Getenv("GAME_CONTRACT_UNUSED") != "untouched" {
		t.Fatal("settle did not respect declared environment boundaries")
	}
}

func TestReadOnlyDispatchHandlersAreWired(t *testing.T) {
	work := freshTree(t, "sample", map[string]string{"notes": "plain\n"})
	cfg := blankConfig()
	if err := helpVerb(work, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := treesVerb(work, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := whoVerb(work, cfg, nil); err != nil {
		t.Fatal(err)
	}
	if err := patchVerb(work, cfg, nil); err == nil {
		t.Fatal("patch accepted an unspecified member")
	}
	if err := releaseVerb(work, cfg, []string{"diff", "v1"}); err == nil {
		t.Fatal("release diff accepted missing project remotes")
	}
}

func TestBuildDispatchRejectsUnknownTarget(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	if err := buildVerb(work, blankConfig(), []string{"missing"}); err == nil {
		t.Fatal("unknown build target accepted")
	}
}

func TestCleanDispatchRemovesOnlyBuildProducts(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "sample", map[string]string{"notes": "source\n"})
	write(t, work.Path("bin", "built"), "product")
	if err := cleanVerb(work, blankConfig(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work.Path("bin", "built")); !os.IsNotExist(err) {
		t.Fatalf("build product remains: %v", err)
	}
	if body, err := os.ReadFile(work.Path("notes")); err != nil || string(body) != "source\n" {
		t.Fatalf("clean changed source: %q %v", body, err)
	}
}

func TestLintAndVerifyDispatchRejectMalformedSource(t *testing.T) {
	standTools(t, "staticcheck")
	work := repoAt(t.TempDir())
	write(t, work.Path("broken.go"), "package {\n")
	cfg := gamed(t, "lint comments")
	if err := lintVerb(work, cfg, nil); err == nil {
		t.Fatal("lint dispatch accepted malformed source")
	}
	if err := verifyVerb(work, cfg, nil); err == nil {
		t.Fatal("verify dispatch accepted malformed source")
	}
}

func TestSweepDispatchRejectsMissingRevision(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "sample", map[string]string{"notes": "source\n"})
	if err := sweepVerb(work, blankConfig(), []string{"missing-ref"}); err == nil {
		t.Fatal("sweep dispatch accepted an absent revision")
	}
}
