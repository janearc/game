package repo_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"go.uber.org/mock/gomock"
)

// gathered is what Blobs gave: every file's name and body, in order.
type gathered struct {
	names  []string
	bodies []string
}

func (got *gathered) take(path, body string) error {
	got.names = append(got.names, path)
	got.bodies = append(got.bodies, body)
	return nil
}

func read(t *testing.T, work repo.Repo, exclude ...string) ([]string, []string) {
	t.Helper()
	got := &gathered{}
	if err := work.Blobs("HEAD", exclude, got.take); err != nil {
		t.Fatal(err)
	}
	return got.names, got.bodies
}

type marked struct{ called bool }

func (seen *marked) mark(string, string) error {
	seen.called = true
	return nil
}

type refused struct {
	calls int
	err   error
}

func (stop *refused) refuse(string, string) error {
	stop.calls++
	return stop.err
}

func allowed(string, string) error { return nil }

func TestBlobsAreWhatShowGives(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	if _, err := mem.Commit("kinds", map[string]string{
		"empty": "", "binary": "\x00\x01\xff\n\x00",
		"café.md": "  padded  \n\n",
	}); err != nil {
		t.Fatal(err)
	}
	names, bodies := read(t, work)
	want := []string{"a.txt", "binary", "café.md", "docs/b.txt", "empty"}
	if !slices.Equal(names, want) {
		t.Fatalf("Blobs named %q, want %q", names, want)
	}
	for index, name := range names {
		shown, err := work.Show("HEAD", name)
		if err != nil {
			t.Fatal(err)
		}
		if bodies[index] != strings.TrimSpace(shown) {
			t.Errorf("%s: Blobs gave %q, Show %q", name, bodies[index],
				shown)
		}
	}
}

func TestBlobsLeaveOutTheExcluded(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	all, _ := read(t, work)
	if !slices.Equal(all, []string{"a.txt", "docs/b.txt"}) {
		t.Errorf("all files: %q", all)
	}
	some, _ := read(t, work, "nothing/", "docs/")
	if !slices.Equal(some, []string{"a.txt"}) {
		t.Errorf("with docs/ excluded: %q", some)
	}
}

func TestBlobsOfATreeWithNoFilesGiveNothing(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	names, _ := read(t, work, "a.txt", "docs/")
	if count.Positive(len(names)) {
		t.Errorf("an empty tree gave %q", names)
	}
}

func TestBlobsRefuseARevisionThatIsNotThere(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	seen := &marked{}
	err := work.Blobs("no-such-rev", nil, seen.mark)
	if err == nil || seen.called {
		t.Errorf("a revision that is not there: err %v, called %v", err,
			seen.called)
	}
}

func TestBlobsStopAtTheFirstRefusal(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	stop := &refused{err: errors.New("enough")}
	err := work.Blobs("HEAD", nil, stop.refuse)
	if !errors.Is(err, stop.err) || stop.calls != count.One {
		t.Errorf("err %v after %d calls, want enough after 1", err,
			stop.calls)
	}
}

// listing is an ls-tree -z answer for the given kind and names.
func listing(kind string, names ...string) string {
	var out strings.Builder
	for _, name := range names {
		out.WriteString("100644 " + kind + " 0123\t" + name + "\x00")
	}
	return out.String()
}

func TestBlobsRefuseASubmodule(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run("/work", "", nil, []string{"ls-tree", "-r", "-z", "HEAD"}).
		Return("160000 commit 0123\tvendored\x00", nil)
	err := work.Blobs("HEAD", nil, allowed)
	if err == nil || !strings.Contains(err.Error(), "vendored") {
		t.Errorf("a submodule: %v, want a refusal naming vendored", err)
	}
}

// many is the four names TestBlobsRunGitTwice lists; defined by that test.
const many = 4

func TestBlobsRunGitTwice(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	list := []string{"ls-tree", "-r", "-z", "HEAD"}
	batch := []string{"cat-file", "--batch"}
	asked := strings.Repeat("0123\n", many)
	answer := strings.Repeat("0123 blob 1\nx\n", many)
	first := runner.EXPECT().Run("/work", "", nil, list).
		Return(listing("blob", "a", "b", "c", "d"), nil)
	then := runner.EXPECT().Run("/work", asked, nil, batch).
		Return(answer, nil)
	gomock.InOrder(first, then)
	names, _ := read(t, work)
	if len(names) != many {
		t.Fatalf("gave %q", names)
	}
}

func TestBlobsRefuseABatchGitRefused(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run(anything, anything, anything, anything).Return(listing("blob", "a"), nil)
	runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", errors.New("batch refused"))
	seen := &marked{}
	err := work.Blobs("HEAD", nil, seen.mark)
	if err == nil || !strings.Contains(err.Error(), "batch refused") ||
		seen.called {
		t.Fatalf("batch failure %v, visitor called %v", err, seen.called)
	}
}

func TestBlobsRefuseAFileTheStoreHasLost(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run(anything, anything, anything, anything).Return(listing("blob", "a.txt"), nil)
	runner.EXPECT().Run(anything, anything, anything, anything).Return("0123 missing\n", nil)
	err := work.Blobs("HEAD", nil, allowed)
	if err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Errorf("a lost blob: %v, want a refusal naming a.txt", err)
	}
}

func TestAnAnswerCutShortIsRefused(t *testing.T) {
	t.Parallel()
	for _, said := range []string{
		"0123 blob many\nbody\n",
		"0123 blob 10\nbody\n",
		"0123 blob 5\nbody",
		"0123 blob 4\nbody",
		"0123 missing\n",
		"0123 blob 4 extra\nbody\n",
		"",
	} {
		work, runner := mocked(t)
		runner.EXPECT().Run(anything, anything, anything, anything).Return(listing("blob", "a.txt"), nil)
		runner.EXPECT().Run(anything, anything, anything, anything).Return(said, nil)
		err := work.Blobs("HEAD", nil, allowed)
		if err == nil || !strings.Contains(err.Error(), "a.txt") {
			t.Errorf("%q: %v, want a refusal naming a.txt", said, err)
		}
	}
}

func TestAnAnswerIsItsBodyAndTheRest(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run(anything, anything, anything, anything).
		Return(listing("blob", "a.txt", "b.txt"), nil)
	runner.EXPECT().Run(anything, anything, anything, anything).
		Return("0123 blob 4\nbody\n4567 blob 4\nnext\n", nil)
	names, bodies := read(t, work)
	if !slices.Equal(names, []string{"a.txt", "b.txt"}) ||
		!slices.Equal(bodies, []string{"body", "next"}) {
		t.Errorf("names %q, bodies %q", names, bodies)
	}
}

func TestAListingLineWithoutATabIsLeftOut(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run(anything, anything, anything, anything).
		Return("100644 blob 0123 a.txt\x00", nil)
	runner.EXPECT().Run(anything, anything, anything, anything).Return("", nil)
	if names, _ := read(t, work); count.Positive(len(names)) {
		t.Errorf("gave %q", names)
	}
}

func TestListedRejectsMetadataWithoutPathSeparator(t *testing.T) {
	t.Parallel()
	kind, object, path, ok := repo.Listed("100644 blob abcdef1")
	if ok || kind != "" || object != "" || path != "" {
		t.Fatalf("malformed record returned %q, %q, %q, %t", kind, object, path, ok)
	}
}

func TestListedReadsAWellFormedRecord(t *testing.T) {
	t.Parallel()
	kind, object, path, ok := repo.Listed("100644 blob abc\tdir/a b")
	if !ok || kind != "blob" || object != "abc" || path != "dir/a b" {
		t.Fatalf("got %q, %q, %q, %t", kind, object, path, ok)
	}
}
