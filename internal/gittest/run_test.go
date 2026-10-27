package gittest_test

import (
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/gittest"
	"slices"
	"strings"
	"testing"
)

func seeded(t *testing.T) (*gittest.Mem, string) {
	t.Helper()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	head, err := mem.Commit("seed", map[string]string{
		"a.txt": "a\n", "docs/b.txt": "b\n"})
	if err != nil {
		t.Fatal(err)
	}
	return mem, head
}

func ask(mem *gittest.Mem, stdin string, env []string, args ...string) (string, error) {
	return mem.Run("/x", stdin, env, args)
}

func TestACommandItDoesNotKnowIsRefused(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if _, err := ask(mem, "", nil, "push"); err == nil ||
		!strings.Contains(err.Error(), "git push") {
		t.Errorf("an unknown command: %v", err)
	}
	if _, err := ask(mem, "", nil); err == nil {
		t.Error("no command was accepted")
	}
}

func TestRevParseGivesACommitAndItsTree(t *testing.T) {
	t.Parallel()
	mem, head := seeded(t)
	if got, _ := ask(mem, "", nil, "rev-parse", "HEAD"); strings.TrimSpace(got) != head {
		t.Errorf("HEAD is %q, want %q", got, head)
	}
	tree, err := ask(mem, "", nil, "rev-parse", "HEAD^{tree}")
	if err != nil || strings.TrimSpace(tree) == head {
		t.Errorf("tree %q, %v", tree, err)
	}
	if got, _ := ask(mem, "", nil, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("branch %q", got)
	}
	if _, err := ask(mem, "", nil, "rev-parse", "none^{tree}"); err == nil {
		t.Error("a revision that is not there has a tree")
	}
}

func TestShowAndCatFileGiveWhatWasCommitted(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	got, err := ask(mem, "", nil, "show", "HEAD:docs/b.txt")
	if err != nil || got != "b\n" {
		t.Errorf("show %q, %v", got, err)
	}
	if _, err := ask(mem, "", nil, "show", "none:a.txt"); err == nil {
		t.Error("show of a revision that is not there")
	}
	if _, err := ask(mem, "", nil, "show", "HEAD:none"); err == nil {
		t.Error("show of a path that is not there")
	}
	listed, _ := ask(mem, "", nil, "ls-tree", "-r", "-z", "HEAD")
	object := strings.Fields(listed)[hashWord]
	if body, err := ask(mem, "", nil, "cat-file", "blob", object); err != nil || body != "a\n" {
		t.Errorf("cat-file blob %q, %v", body, err)
	}
	batch, err := ask(mem, object+"\n", nil, "cat-file", "--batch")
	if want := object + " blob 2\na\n\n"; err != nil || batch != want {
		t.Errorf("batch %q, %v, want %q", batch, err, want)
	}
	if _, err := ask(mem, "0123456789012345678901234567890123456789\n", nil,
		"cat-file", "--batch"); err == nil {
		t.Error("batch of an object that is not there")
	}
	if _, err := ask(mem, "", nil, "cat-file", "blob", "dead"); err == nil {
		t.Error("blob that is not there")
	}
}

const (
	hashWord  = 2 // defined by ls-tree: mode kind object, then the path
	fileCount = 2 // defined by the fixture, which seeds two files
)

func TestLsTreeListsFilesAsGitDoesAndNotDirectories(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	out, err := ask(mem, "", nil, "ls-tree", "-r", "-z", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	records := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(records) != fileCount || !strings.HasPrefix(records[count.First], "100644 blob ") ||
		!strings.HasSuffix(records[count.One], "\tdocs/b.txt") {
		t.Errorf("listing %q", records)
	}
	if _, err := ask(mem, "", nil, "ls-tree", "-r", "-z", "none"); err == nil {
		t.Error("listing of a revision that is not there")
	}
}

func TestAScratchIndexIsApartFromTheRepositoryAndFromOthers(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	one := []string{"GIT_INDEX_FILE=one"}
	two := []string{"GIT_INDEX_FILE=two"}
	if _, err := ask(mem, "", one, "read-tree", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(mem, "", one, "rm", "-r", "--cached", "-q",
		"--ignore-unmatch", "--", "docs", "a.txt"); err != nil {
		t.Fatal(err)
	}
	blob, _ := ask(mem, "new\n", nil, "hash-object", "-w", "--stdin")
	if _, err := ask(mem, "", one, "update-index", "--add", "--cacheinfo",
		"100644,"+blob+",x/y.txt"); err != nil {
		t.Fatal(err)
	}
	tree, err := ask(mem, "", one, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := mem.Names(tree); !slices.Equal(got, []string{"x/y.txt"}) {
		t.Errorf("index one holds %v", got)
	}
	empty, _ := ask(mem, "", two, "write-tree")
	if got, _ := mem.Names(empty); count.Positive(len(got)) {
		t.Errorf("index two holds %v", got)
	}
	if _, err := ask(mem, "", one, "update-index", "--add", "--cacheinfo",
		"nonsense"); err == nil {
		t.Error("a bad cacheinfo")
	}
	if _, err := ask(mem, "", one, "update-index", "--add", "--cacheinfo",
		"zz,aa,b"); err == nil {
		t.Error("a bad mode")
	}
	if _, err := ask(mem, "", two, "read-tree", "none"); err == nil {
		t.Error("read-tree of a revision that is not there")
	}
}

func TestRmLeavesAFileThatOnlySharesAPrefix(t *testing.T) {
	t.Parallel()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Commit("s", map[string]string{"docs/b": "b\n",
		"docs.md": "x\n"}); err != nil {
		t.Fatal(err)
	}
	env := []string{"GIT_INDEX_FILE=i"}
	ask(mem, "", env, "read-tree", "HEAD")
	ask(mem, "", env, "rm", "-r", "--cached", "--", "docs")
	tree, _ := ask(mem, "", env, "write-tree")
	if got, _ := mem.Names(tree); !slices.Equal(got, []string{"docs.md"}) {
		t.Errorf("holds %v", got)
	}
}

func TestStatusSeesWhatChanged(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if got, _ := ask(mem, "", nil, "status", "--porcelain"); got != "" {
		t.Fatalf("fresh status %q", got)
	}
	if err := mem.Write("new", "x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ask(mem, "", nil, "status", "--porcelain"); !strings.Contains(got, "?? new") {
		t.Errorf("status %q", got)
	}
}

func TestRemotesAnswerLsRemoteAndGetURL(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	road, head := seeded(t)
	mem.Remote("road", "/road.git", road)
	got, err := ask(mem, "", nil, "ls-remote", "road", "refs/heads/main")
	if want := head + "\trefs/heads/main\n"; err != nil || got != want {
		t.Errorf("ls-remote %q, %v", got, err)
	}
	if _, err := ask(mem, "", nil, "ls-remote", "none", "refs/heads/main"); err == nil {
		t.Error("ls-remote of no remote")
	}
	if url, _ := ask(mem, "", nil, "remote", "get-url", "--push", "road"); url != "/road.git\n" {
		t.Errorf("url %q", url)
	}
	if _, err := ask(mem, "", nil, "remote", "get-url", "--push", "none"); err == nil {
		t.Error("url of no remote")
	}
}

func TestConfigListsKeysWithTheLastPartLowered(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	mem.Config("url.x/.insteadOf", "/a/")
	mem.Config("url.y/.insteadOf", "/b/")
	got, _ := ask(mem, "", nil, "config", "--null", "--list")
	if want := "url.x/.insteadof\n/a/\x00url.y/.insteadof\n/b/"; got != want {
		t.Errorf("config %q, want %q", got, want)
	}
}

func TestACommitOnAnotherBranchMovesTheBranchName(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if err := mem.Checkout("work"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ask(mem, "", nil, "rev-parse", "--abbrev-ref", "HEAD"); got != "work" {
		t.Errorf("branch %q", got)
	}
	if err := mem.Write("n", "n"); err != nil {
		t.Fatal(err)
	}
	if err := mem.Stage("n"); err != nil {
		t.Fatal(err)
	}
	if err := mem.Stage("none"); err == nil {
		t.Error("staged a file that is not there")
	}
	if _, err := mem.Names("0123456789012345678901234567890123456789"); err == nil {
		t.Error("names of a tree that is not there")
	}
}

func TestAnIndexNeedNotBeNamed(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if _, err := ask(mem, "", nil, "read-tree", "HEAD"); err != nil {
		t.Fatal(err)
	}
	tree, err := ask(mem, "", nil, "write-tree")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := mem.Names(tree); !slices.Equal(got,
		[]string{"a.txt", "docs/b.txt"}) {
		t.Errorf("the unnamed index holds %v", got)
	}
}

func TestRmWithNoPathsLeavesTheIndexAlone(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	env := []string{"GIT_INDEX_FILE=i"}
	ask(mem, "", env, "read-tree", "HEAD")
	if _, err := ask(mem, "", env, "rm", "-r", "--cached"); err != nil {
		t.Fatal(err)
	}
	tree, _ := ask(mem, "", env, "write-tree")
	if got, _ := mem.Names(tree); len(got) != fileCount {
		t.Errorf("rm with no paths left %v", got)
	}
}

func TestATreeKeepsEachDirectoryApart(t *testing.T) {
	t.Parallel()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Commit("s", map[string]string{"a/x": "x\n",
		"b/y": "y\n", "c/z/w": "w\n"}); err != nil {
		t.Fatal(err)
	}
	env := []string{"GIT_INDEX_FILE=i"}
	ask(mem, "", env, "read-tree", "HEAD")
	tree, _ := ask(mem, "", env, "write-tree")
	if got, _ := mem.Names(tree); !slices.Equal(got,
		[]string{"a/x", "b/y", "c/z/w"}) {
		t.Errorf("holds %v", got)
	}
}

func TestASubmoduleIsListedAsACommit(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if _, err := mem.Gitlink("sub", "0123456789012345678901234567890123456789"); err != nil {
		t.Fatal(err)
	}
	out, _ := ask(mem, "", nil, "ls-tree", "-r", "-z", "HEAD")
	if !strings.Contains(out, "160000 commit 0123456789012345678901234567890123456789\tsub\x00") {
		t.Errorf("listing %q", out)
	}
}

func TestARemoteWithNothingOnMainSaysNothing(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	empty, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	mem.Remote("empty", "/empty.git", empty)
	got, err := ask(mem, "", nil, "ls-remote", "empty", "refs/heads/main")
	if err != nil || got != "" {
		t.Errorf("ls-remote %q, %v", got, err)
	}
}

func TestAnIndexNobodyReadStartsEmpty(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	env := []string{"GIT_INDEX_FILE=fresh"}
	blob, _ := ask(mem, "new\n", nil, "hash-object", "-w", "--stdin")
	if _, err := ask(mem, "", env, "update-index", "--add", "--cacheinfo",
		"100644,"+blob+",only.txt"); err != nil {
		t.Fatal(err)
	}
	tree, _ := ask(mem, "", env, "write-tree")
	if got, _ := mem.Names(tree); !slices.Equal(got, []string{"only.txt"}) {
		t.Errorf("holds %v", got)
	}
}

func TestAnObjectListsByItsTreeHashToo(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	tree, _ := ask(mem, "", nil, "rev-parse", "HEAD^{tree}")
	out, err := ask(mem, "", nil, "ls-tree", "-r", "-z", strings.TrimSpace(tree))
	if err != nil || !strings.Contains(out, "\ta.txt\x00") {
		t.Errorf("listing %q, %v", out, err)
	}
}
