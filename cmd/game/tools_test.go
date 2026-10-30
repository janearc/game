package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// tree writes files into a new tree and gives its folder.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o600)
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// call is one command a stand-in tool was asked to run.
type call struct {
	dir, name string
	args      []string
}

// tools stands in for the tools game runs: it records every call, and
// answers by the tool's name.
type tools struct {
	calls []call
	said  map[string]string
	fail  map[string]error
}

// run is the stand-in's runner.
func (stand *tools) run(dir, name string, args ...string) (string, error) {
	stand.calls = append(stand.calls, call{dir, name, args})
	return stand.said[name], stand.fail[name]
}

// sameCall holds two calls alike.
func sameCall(one, other call) bool {
	return one.dir == other.dir && one.name == other.name &&
		slices.Equal(one.args, other.args)
}

// game mutants in a tree with no Go finds nothing to mutate and says so,
// and succeeds.
func TestMutantsKeepsGoForGame(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	if err := viaPzm(t, "mutants", mutantsVerb, work, blankConfig(), nil); err != nil {
		t.Errorf("game mutants in a tree with no Go: %v", err)
	}
}
