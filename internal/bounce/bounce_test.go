package bounce_test

import (
	"testing"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/gittest"
)

// added lines that name the author are hits; removed lines, headers and
// other names are not; the possessive counts.
func TestCheck(t *testing.T) {
	t.Parallel()
	before := "// Ada wanted this\n"
	after := "// the operation as asked for\n// Ada's call\n// adage is a saying\n"
	hits := stagedHits(t, "a.go", before, after, "Ada")
	if len(hits) != count.One || hits[count.First].Line() != "+// Ada's call" {
		t.Fatalf("got %+v", hits)
	}
}

// history is a repository with three commits, the second adding a line
// that names Ada, and it gives the second's hash.
func history(t *testing.T) (*gittest.Mem, string) {
	t.Helper()
	mem := newMem(t)
	commit(t, mem, "x", map[string]string{"a.txt": "x\n"})
	second := commit(t, mem, "// Ada's call",
		map[string]string{"a.txt": "x\n// Ada's call\n"})
	commit(t, mem, "y", map[string]string{"b.txt": "y\n"})
	return mem, second
}

// one commit is its own added lines: not the commits after it, and not
// what is in the working tree now.
func TestRangeOfOneCommitIsThatCommitsLines(t *testing.T) {
	mem, second := history(t)
	if err := mem.Write("b.txt", "y\n// Ada, uncommitted\n"); err != nil {
		t.Fatal(err)
	}
	hits, err := bounce.Range(workIn(mem), second, "Ada")
	if err != nil || len(hits) != count.One ||
		hits[count.First].Line() != "+// Ada's call" {
		t.Errorf("the commit that names Ada: %+v, %v", hits, err)
	}
	hits, err = bounce.Range(workIn(mem), "HEAD", "Ada")
	if err != nil || count.Positive(len(hits)) {
		t.Errorf("the commit after it: %+v, %v", hits, err)
	}
}

// a range is the lines added across it.
func TestRangeOfARangeIsItsLines(t *testing.T) {
	mem, second := history(t)
	for _, row := range []struct {
		rng  string
		want int
	}{
		{"HEAD~2..HEAD", count.One},
		{second + "..HEAD", count.Nothing},
		{"HEAD~2...HEAD", count.One},
	} {
		hits, err := bounce.Range(workIn(mem), row.rng, "Ada")
		if err != nil || len(hits) != row.want {
			t.Errorf("%s: %+v, %v, want %d", row.rng, hits, err, row.want)
		}
	}
}

// staged is what is staged and nothing else.
func TestStagedIsWhatIsStaged(t *testing.T) {
	mem, _ := history(t)
	if err := mem.Write("b.txt", "y\n// Ada, not staged\n"); err != nil {
		t.Fatal(err)
	}
	if hits, err := bounce.Staged(workIn(mem), "Ada"); err != nil ||
		count.Positive(len(hits)) {
		t.Errorf("nothing staged: %+v, %v", hits, err)
	}
	if err := mem.Stage("b.txt"); err != nil {
		t.Fatal(err)
	}
	if hits, err := bounce.Staged(workIn(mem), "Ada"); err != nil ||
		len(hits) != count.One {
		t.Errorf("staged: %+v, %v", hits, err)
	}
}

// a revision that is not there, or no repository at all, is an error,
// not a pass with nothing found.
func TestBounceRefusesWhatGitCannotRead(t *testing.T) {
	mem, _ := history(t)
	for _, rng := range []string{"no-such-rev", "no-such-rev..HEAD"} {
		if _, err := bounce.Range(workIn(mem), rng, "Ada"); err == nil {
			t.Errorf("%s was not refused", rng)
		}
	}
	nowhere := workIn(gittest.Nowhere())
	if _, err := bounce.Staged(nowhere, "Ada"); err == nil {
		t.Error("staged, outside a repository, was not refused")
	}
}

// a range is what it changes, net: a line added and taken out again
// inside it is not in it, as a release flattened from it would not be.
func TestARangeIsItsNetChange(t *testing.T) {
	mem, _ := history(t)
	commit(t, mem, "taken out", map[string]string{"a.txt": "x\n"})
	hits, err := bounce.Range(workIn(mem), "HEAD~3..HEAD", "Ada")
	if err != nil || count.Positive(len(hits)) {
		t.Errorf("added and taken out again: %+v, %v", hits, err)
	}
}

// a merge is the lines it brought in, against its first parent: a clean
// merge's combined diff is empty, and would pass what it brings.
func TestAMergeIsWhatItBroughtIn(t *testing.T) {
	mem, _ := history(t)
	ok(t, mem.Branch("side", "HEAD~2"))
	ok(t, mem.Switch("side"))
	commit(t, mem, "side", map[string]string{"side.txt": "// written by Ada\n"})
	ok(t, mem.Switch("main"))
	_, err := mem.Merge("side")
	ok(t, err)
	hits, err := bounce.Range(workIn(mem), "HEAD", "Ada")
	if err != nil || len(hits) != count.One ||
		hits[count.First].Line() != "+// written by Ada" {
		t.Errorf("a clean merge: %+v, %v", hits, err)
	}
}
