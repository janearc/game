package repo_test

import (
	"github.com/janearc/game/internal/gittest"
	"go.uber.org/mock/gomock"
	"os"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// fixtureFiles are the files the fixture commits. One sits at the top.
// One sits under a directory, so exclusion by prefix has work to do.
var fixtureFiles = map[string]string{"a.txt": "a\n", "docs/b.txt": "b\n"}

// fixture is a repository on main with one commit holding the fixture
// files. It lives in memory, for as long as the test.
func fixture(t *testing.T) (repo.Repo, *gittest.Mem) {
	t.Helper()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Commit("seed", fixtureFiles); err != nil {
		t.Fatal(err)
	}
	return repo.At("/work", mem), mem
}

// licence is a file of terms in a directory the test owns. The licence
// is read from a path, so it is the one thing here that is on a disk.
func licence(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "LICENSE")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// anything matches any one argument of a mocked call.
var anything = gomock.Any()

// fileCount is how many files the fixture commits.
const fileCount = 2 // defined by the fixture, which commits two files
