package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// asked is a stand-in for run that keeps each command it is given,
// answers one holding a key of gives with its value, and fails the one
// named in fail.
type asked struct {
	said  []string
	fail  string
	gives map[string]string
}

// run is the stand-in's run.
func (stand *asked) run(dir, name string, args ...string) (string, error) {
	line := filepath.Base(dir) + ": " + name + " " + strings.Join(args, " ")
	stand.said = append(stand.said, line)
	answer := ""
	for key, value := range stand.gives {
		if strings.Contains(line, key) {
			answer = value
		}
	}
	if stand.fail != "" && strings.Contains(line, stand.fail) {
		return answer, errors.New("no: " + line)
	}
	return answer, nil
}

// cleaned is a git repository at dir/name holding files, untracked.
func cleaned(t *testing.T, name string, files map[string]string) repo.Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	gitAt(t, filepath.Dir(dir), "init", "-q", dir)
	for file, body := range files {
		write(t, filepath.Join(dir, file), body)
	}
	return repoAt(dir)
}

// write is a file at path, its directories made.
func write(t *testing.T, path, body string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(body), openFile); err != nil {
		t.Fatal(err)
	}
}

// shutDir is a directory shut until the test ends, and the mode that
// opens it again.
type shutDir struct {
	dir  string
	open os.FileMode
}

// reopen opens the directory again, so the tree can be removed.
func (shut shutDir) reopen() {
	_ = os.Chmod(shut.dir, shut.open)
}

// a module is cleaned of its build, and of go's caches only when asked;
// each bin/ of the root and its members is swept, and the count said.
func TestCleanAsksGoForWhatItWasAskedAndCountsTheRest(t *testing.T) {
	t.Parallel()
	for _, cache := range []bool{false, true} {
		work := cleaned(t, "root", map[string]string{
			"go.mod":       "module x\n",
			"bin/a":        "built",
			"bin/b":        "built",
			"member/f":     "source",
			"member/bin/c": "built",
			"other/f":      "a member with no bin",
		})
		stand := &asked{}
		var out bytes.Buffer
		cfg := gamed(t, "stack member v1", "stack other v1", "stack gone v1")
		if err := cleanBy(work, cfg, cache, stand.run, &out); err != nil {
			t.Fatal(err)
		}
		want := []string{"root: go clean ./..."}
		said := "clean: 3 files of build output away\n"
		if cache {
			want = append(want, "root: go clean -cache -testcache")
			said = "clean: 3 files of build output away, caches " +
				"forgotten\n"
		}
		if !reflect.DeepEqual(stand.said, want) {
			t.Errorf("cache %v: asked %q, want %q", cache, stand.said,
				want)
		}
		if out.String() != said {
			t.Errorf("cache %v: said %q, want %q", cache, out.String(),
				said)
		}
		for _, bin := range []string{"bin", "member/bin"} {
			_, err := os.Stat(work.Path(bin))
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("cache %v: %s, emptied, is still there: %v",
					cache, bin, err)
			}
		}
	}
}

// go refusing either clean is the error, and nothing is swept after.
func TestCleanStopsWhereGoRefuses(t *testing.T) {
	t.Parallel()
	for _, fail := range []string{"clean ./...", "-cache"} {
		work := cleaned(t, "root", map[string]string{
			"go.mod": "module x\n", "bin/a": "built"})
		stand := &asked{fail: fail}
		err := cleanBy(work, blankConfig(), true, stand.run, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), fail) {
			t.Errorf("%s: %v", fail, err)
		}
		if _, err := os.Stat(work.Path("bin", "a")); err != nil {
			t.Errorf("%s: swept after go refused: %v", fail, err)
		}
	}
}

// a bin that cannot be read is the error.
func TestCleanRefusesABinItCannotRead(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"bin": "a file, not a dir"})
	err := cleanBy(work, blankConfig(), false, (&asked{}).run,
		&bytes.Buffer{})
	if err == nil {
		t.Error("a bin that is a file was swept")
	}
}

// go.work.sum goes, and says so; one that will not go is the error.
func TestCleanTakesGoWorkSum(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"go.work.sum": "h1:x\n"})
	var out bytes.Buffer
	if err := cleanBy(work, blankConfig(), false, (&asked{}).run,
		&out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work.Path("go.work.sum")); err == nil {
		t.Error("go.work.sum is still there")
	}
	if !strings.HasPrefix(out.String(), "clean: go.work.sum away;") {
		t.Errorf("said %q", out.String())
	}
	stuck := cleaned(t, "stuck", map[string]string{"go.work.sum/x": "x"})
	if err := cleanBy(stuck, blankConfig(), false, (&asked{}).run,
		&bytes.Buffer{}); err == nil {
		t.Error("a go.work.sum that would not go was not an error")
	}
}

// go cleans a module at the root, each member a go.work uses below it,
// and nothing where there is no go.
func TestCleanableIsWhereGoCanClean(t *testing.T) {
	t.Parallel()
	module := cleaned(t, "module", map[string]string{
		"go.mod":  "module x\n",
		"go.work": "go 1.27\n\nuse ./a\n",
	})
	if got := cleanable(module); !reflect.DeepEqual(got,
		[]string{module.Dir()}) {
		t.Errorf("a module: %q", got)
	}
	work := cleaned(t, "work", map[string]string{
		"go.work": "go 1.27\n\nuse (\n\t./a\n\t./b/c\n)\nuse ./d\n" +
			"toolchain go1.27.0\n",
	})
	want := []string{work.Path("a"),
		work.Path("b", "c"), work.Path("d")}
	if got := cleanable(work); !reflect.DeepEqual(got, want) {
		t.Errorf("a workspace: %q, want %q", got, want)
	}
	if got := cleanable(cleaned(t, "none", nil)); got != nil {
		t.Errorf("no go: %q", got)
	}
}

// a bin git cannot vouch for is left whole: outside a git tree nothing
// in it is known to be build output.
func TestCleanLeavesABinGitCannotSpeakFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin", "a"), "maybe source")
	err := cleanBy(repoAt(dir), blankConfig(), false,
		(&asked{}).run, &bytes.Buffer{})
	if err == nil {
		t.Error("a bin outside git was swept without an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "a")); err != nil {
		t.Errorf("a bin outside git lost a file: %v", err)
	}
}

// superuser is defined by the operating system: the user id that writes
// through a directory no mode lets it write.
const superuser = 0

// asRoot is whether the tests run as that user, for the tests that need a
// mode to refuse them.
func asRoot() bool { return os.Geteuid() == superuser }

// what will not be taken out of a bin, or a bin that will not go once
// empty, is the error.
func TestCleanSaysWhatWouldNotGo(t *testing.T) {
	t.Parallel()
	if asRoot() {
		t.Skip("root writes through a read-only directory")
	}
	for name, locked := range map[string]string{
		"a file in bin":  "bin/sub",
		"an emptied bin": "member",
	} {
		work := cleaned(t, "root", map[string]string{
			"bin/sub/x": "built", "member/bin/c": "built"})
		at := work.Path(locked)
		shut := os.FileMode(0o555)
		if err := os.Chmod(at, shut); err != nil {
			t.Fatal(err)
		}
		open := os.FileMode(0o755)
		t.Cleanup(shutDir{dir: at, open: open}.reopen)
		cfg := gamed(t, "stack member v1")
		err := cleanBy(work, cfg, false, (&asked{}).run, &bytes.Buffer{})
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// Git tracks some go.work.sum files. Those are the repository's own.
// Clean leaves them and says so, as it leaves the tracked files in bin.
func TestCleanLeavesATrackedGoWorkSum(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"go.work.sum": "h1:x\n"})
	gitAt(t, work.Dir(), "add", "go.work.sum")
	var out bytes.Buffer
	if err := cleanBy(work, blankConfig(), false, (&asked{}).run,
		&out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work.Path("go.work.sum")); err != nil {
		t.Errorf("a tracked go.work.sum was removed: %v", err)
	}
	if !strings.HasPrefix(out.String(), "clean: go.work.sum is tracked;") {
		t.Errorf("said %q", out.String())
	}
}

// when git cannot say whether go.work.sum is tracked, that is the error
// and the file is left.
func TestCleanStopsWhenGitCannotSayIfGoWorkSumIsTracked(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"go.work.sum": "h1:x\n"})
	write(t, work.Path(".git", "index"), "not an index")
	err := cleanBy(work, blankConfig(), false, (&asked{}).run,
		&bytes.Buffer{})
	if err == nil {
		t.Error("clean went on when git could not say")
	}
	if _, err := os.Stat(work.Path("go.work.sum")); err != nil {
		t.Errorf("go.work.sum was removed unasked: %v", err)
	}
}
