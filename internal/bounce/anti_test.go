package bounce_test

import (
	"testing"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/gittest"
	"github.com/janearc/game/internal/repo"
)

// stagedHits is what Staged finds when file goes from before to after in
// a repository and the change is staged. before is committed first when
// there is one. The repository is in memory for as long as the test.
func stagedHits(t *testing.T, file, before, after, author string) []bounce.Hit {
	t.Helper()
	mem := newMem(t)
	if before != "" {
		commit(t, mem, "before", map[string]string{file: before})
	}
	if err := mem.Write(file, after); err != nil {
		t.Fatal(err)
	}
	if err := mem.Stage(file); err != nil {
		t.Fatal(err)
	}
	hits, err := bounce.Staged(workIn(mem), author)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// newMem is an empty repository in memory.
func newMem(t *testing.T) *gittest.Mem {
	t.Helper()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	return mem
}

// commit makes a commit of files whole and gives its hash.
func commit(t *testing.T, mem *gittest.Mem, message string,
	files map[string]string) string {
	t.Helper()
	hash, err := mem.Commit(message, files)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// workIn is the repository whose git is run on mem.
func workIn(mem *gittest.Mem) repo.Repo {
	return repo.At("/work", mem)
}

func noBounceHits(t *testing.T, file, before, after, author string) {
	t.Helper()
	if hits := stagedHits(t, file, before, after, author); len(hits) != count.Nothing {
		t.Fatalf("unexpected hits: %v", hits)
	}
}

func TestAntiBounceEmptyAuthor(t *testing.T) {
	t.Parallel()
	noBounceHits(t, "a.go", "", "// Ada's call\n", "")
}

func TestAntiBounceRemovedLine(t *testing.T) {
	t.Parallel()
	noBounceHits(t, "a.go", "// Ada's call\n", "", "Ada")
}

func TestAntiBounceHeader(t *testing.T) {
	t.Parallel()
	noBounceHits(t, "Ada", "", "x\n", "Ada")
}

func TestAntiBounceSubstring(t *testing.T) {
	t.Parallel()
	noBounceHits(t, "a.go", "", "// adage is a saying\n", "Ada")
}

func TestAntiBounceOtherAuthor(t *testing.T) {
	t.Parallel()
	noBounceHits(t, "a.go", "", "// Grace's call\n", "Ada")
}

// ok stops the test on an error.
func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
