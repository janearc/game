package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLastReleasePropagatesFetchFailure(t *testing.T) {
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"ls-remote) printf 'abcdef1\\trefs/heads/main\\n' ;;\n"+
		"fetch) exit 1 ;;\n esac\n")
	t.Setenv("PATH", tools)
	here := repoAt(t.TempDir())
	if _, err := lastRelease(here, "dist"); err == nil {
		t.Fatal("release fetch failure was discarded")
	}
}

func TestLastCommitsPropagatesListingFailure(t *testing.T) {
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\nexit 1\n")
	t.Setenv("PATH", tools)
	here := repoAt(t.TempDir())
	if _, err := lastCommits(here, lastThree); err == nil {
		t.Fatal("commit listing failure was discarded")
	}
}

func TestLastCommitsPropagatesDescriptionFailure(t *testing.T) {
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\n"+
		"case \"$2\" in\n"+
		"-n) printf 'abcdef1\\n' ;;\n"+
		"-1) exit 1 ;;\n esac\n")
	t.Setenv("PATH", tools)
	here := repoAt(t.TempDir())
	if _, err := lastCommits(here, lastThree); err == nil {
		t.Fatal("commit description failure was discarded")
	}
}

func TestDescribeRejectsMalformedGitOutput(t *testing.T) {
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\nprintf 'bad\\n'\n")
	t.Setenv("PATH", tools)
	here := repoAt(t.TempDir())
	_, err := describe(here, "abcdef1")
	if err == nil || !strings.Contains(err.Error(), "git log said") {
		t.Fatalf("malformed git output error %v", err)
	}
}

// TestLastReleaseSaysItWasTheFetchThatFailed: the error is the fetch's,
// not the one a read of what was never fetched would give.
func TestLastReleaseSaysItWasTheFetchThatFailed(t *testing.T) {
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"ls-remote) printf 'abcdef1\\trefs/heads/main\\n' ;;\n"+
		"fetch) exit 1 ;;\n esac\n")
	t.Setenv("PATH", tools)
	here := repoAt(t.TempDir())
	_, err := lastRelease(here, "dist")
	if err == nil || !strings.Contains(err.Error(), "fetch") ||
		strings.Contains(err.Error(), "git log said") {
		t.Fatalf("the failure was %v", err)
	}
}
