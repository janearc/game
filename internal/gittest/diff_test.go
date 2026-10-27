package gittest_test

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/gittest"
)

// chain is the hashes of the three commits threeCommits makes.
type chain struct{ first, second, third string }

// threeCommits is three commits. The first holds a.txt. The second adds
// a line to it. The third holds b.txt.
func threeCommits(t *testing.T) (*gittest.Mem, chain) {
	t.Helper()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	var made chain
	made.first = commit(t, mem, map[string]string{"a.txt": "x\n"})
	made.second = commit(t, mem, map[string]string{"a.txt": "x\nsecond\n"})
	made.third = commit(t, mem, map[string]string{"b.txt": "y\n"})
	return mem, made
}

// commit is a commit of files, whose hash it gives.
func commit(t *testing.T, mem *gittest.Mem, files map[string]string) string {
	t.Helper()
	hash, err := mem.Commit("step", files)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// ok stops the test on an error.
func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// aside makes a branch at rev, commits files on it and comes back to
// main.
func aside(t *testing.T, mem *gittest.Mem, rev string, files map[string]string) {
	t.Helper()
	ok(t, mem.Branch("side", rev))
	ok(t, mem.Switch("side"))
	commit(t, mem, files)
	ok(t, mem.Switch("main"))
}

// held is the files of the tree HEAD holds, as one string.
func held(t *testing.T, mem *gittest.Mem) string {
	t.Helper()
	tree, err := ask(mem, "", nil, "rev-parse", "HEAD^{tree}")
	ok(t, err)
	names, err := mem.Names(strings.TrimSpace(tree))
	ok(t, err)
	return strings.Join(names, " ")
}

// added is the lines a diff adds, joined.
func added(diff string) string {
	var found []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			found = append(found, line)
		}
	}
	return strings.Join(found, "|")
}

func TestShowIsWhatOneCommitAdded(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	got, err := ask(mem, "", nil, "show", "-U0", "--format=", made.second)
	if err != nil || added(got) != "+second" {
		t.Errorf("the second commit added %q, %v", got, err)
	}
	if strings.Contains(got, "\n x") {
		t.Errorf("-U0 gave context: %q", got)
	}
	first, _ := ask(mem, "", nil, "show", "-U0", made.first)
	if added(first) != "+x" {
		t.Errorf("the first commit added %q", first)
	}
}

func TestShowWithoutAFlagGivesContext(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	got, _ := ask(mem, "", nil, "show", made.second)
	if !strings.Contains(got, "\n x\n") {
		t.Errorf("no context in %q", got)
	}
}

func TestADiffOfARangeIsTheNetChange(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	for _, row := range []struct{ rng, want string }{
		{"HEAD~2..HEAD", "+second|+y"},
		{made.second + "..HEAD", "+y"},
		{"HEAD~2...HEAD", "+second|+y"},
		{"HEAD..HEAD", ""},
	} {
		got, err := ask(mem, "", nil, "diff", "-U0", row.rng)
		if err != nil || added(got) != row.want {
			t.Errorf("%s added %q, %v", row.rng, got, err)
		}
	}
}

func TestADiffNeedsRevisionsThatAreThere(t *testing.T) {
	t.Parallel()
	mem, _ := threeCommits(t)
	for _, rng := range []string{"none", "none..HEAD", "HEAD..none",
		"none...HEAD"} {
		if _, err := ask(mem, "", nil, "diff", "-U0", rng); err == nil {
			t.Errorf("%s was not refused", rng)
		}
	}
	if _, err := ask(mem, "", nil, "show", "none"); err == nil {
		t.Error("show of a commit that is not there")
	}
	if _, err := ask(mem, "", nil, "show", "-Ux", "HEAD"); err == nil {
		t.Error("a context that is no number")
	}
}

func TestCachedIsWhatIsStagedAndNothingElse(t *testing.T) {
	t.Parallel()
	mem, _ := threeCommits(t)
	ok(t, mem.Write("b.txt", "y\nmore\n"))
	if got, _ := ask(mem, "", nil, "diff", "--cached", "-U0"); got != "" {
		t.Errorf("nothing is staged, and %q is", got)
	}
	ok(t, mem.Stage("b.txt"))
	got, err := ask(mem, "", nil, "diff", "--cached", "-U0")
	if err != nil || added(got) != "+more" {
		t.Errorf("staged %q, %v", got, err)
	}
}

func TestCachedBeforeTheFirstCommitIsEverythingStaged(t *testing.T) {
	t.Parallel()
	mem, err := gittest.New()
	ok(t, err)
	ok(t, mem.Write("n.txt", "new\n"))
	ok(t, mem.Stage("n.txt"))
	got, err := ask(mem, "", nil, "diff", "--cached", "-U0")
	if err != nil || added(got) != "+new" {
		t.Errorf("staged %q, %v", got, err)
	}
}

func TestAMergeShowsWhatItBroughtIn(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	aside(t, mem, made.first, map[string]string{"s.txt": "side\n"})
	merged, err := mem.Merge("side")
	ok(t, err)
	got, _ := ask(mem, "", nil, "show", "-U0", "--format=",
		"--diff-merges=first-parent", merged)
	if added(got) != "+side" {
		t.Errorf("the merge brought in %q", got)
	}
	if names := held(t, mem); names != "a.txt b.txt s.txt" {
		t.Errorf("the merge holds %v", names)
	}
}

func TestAMergeShownWithoutFirstParentIsEmpty(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	aside(t, mem, made.first, map[string]string{"s.txt": "side\n"})
	merged, err := mem.Merge("side")
	ok(t, err)
	got, err := ask(mem, "", nil, "show", "-U0", "--format=", merged)
	if err != nil || got != "" {
		t.Errorf("a clean merge's combined diff is %q, %v", got, err)
	}
}

func TestAMergeDropsAFileOneSideDeleted(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	ok(t, mem.Branch("side", made.third))
	ok(t, mem.Switch("side"))
	_, err := mem.Delete("a.txt")
	ok(t, err)
	ok(t, mem.Switch("main"))
	commit(t, mem, map[string]string{"s.txt": "s\n"})
	_, err = mem.Merge("side")
	ok(t, err)
	if names := held(t, mem); names != "b.txt s.txt" {
		t.Errorf("the merge holds %v", names)
	}
	if _, err := mem.Delete("none"); err == nil {
		t.Error("deleted a file that is not there")
	}
}

func TestAMergeOfOneFileChangedBothWaysIsRefused(t *testing.T) {
	t.Parallel()
	mem, made := threeCommits(t)
	aside(t, mem, made.first, map[string]string{"a.txt": "other\n"})
	if _, err := mem.Merge("side"); err == nil ||
		!strings.Contains(err.Error(), "a.txt was changed on both sides") {
		t.Errorf("a conflict: %v", err)
	}
	if _, err := mem.Merge("none"); err == nil {
		t.Error("a merge of a branch that is not there")
	}
	if err := mem.Switch("none"); err == nil {
		t.Error("a switch to a branch that is not there")
	}
	if err := mem.Branch("x", "none"); err == nil {
		t.Error("a branch at a revision that is not there")
	}
}

func TestNowhereRefusesEveryCommand(t *testing.T) {
	t.Parallel()
	if _, err := ask(gittest.Nowhere(), "", nil, "status"); err == nil ||
		!strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("nowhere: %v", err)
	}
}

func TestLogGivesEachMessageNewestFirst(t *testing.T) {
	t.Parallel()
	mem, err := gittest.New()
	ok(t, err)
	_, err = mem.Commit("one", map[string]string{"a": "1"})
	ok(t, err)
	_, err = mem.Commit("two\n\nbody", map[string]string{"a": "2"})
	ok(t, err)
	got, err := ask(mem, "", nil, "log", "HEAD", "--format=%B")
	if want := "two\n\nbody\n\none\n\n"; err != nil || got != want {
		t.Errorf("log %q, %v, want %q", got, err, want)
	}
}

func TestLogStopsAtTheCountItIsGiven(t *testing.T) {
	t.Parallel()
	mem, _ := threeCommits(t)
	got, err := ask(mem, "", nil, "log", "-1", "--format=%B")
	if want := "step\n\n"; err != nil || got != want {
		t.Errorf("log -1 %q, %v, want %q", got, err, want)
	}
	if _, err := ask(mem, "", nil, "log", "-x", "--format=%B"); err == nil {
		t.Error("a count that is no number")
	}
}

func TestLogNeedsARevisionThatIsThere(t *testing.T) {
	t.Parallel()
	mem, _ := threeCommits(t)
	if _, err := ask(mem, "", nil, "log", "none", "--format=%B"); err == nil {
		t.Error("log of a revision that is not there")
	}
}

func TestStagedIsTheTreeOfTheIndex(t *testing.T) {
	t.Parallel()
	mem, _ := threeCommits(t)
	ok(t, mem.Write("n.txt", "new\n"))
	ok(t, mem.Stage("n.txt"))
	tree, err := mem.Staged()
	ok(t, err)
	names, err := mem.Names(tree)
	ok(t, err)
	if strings.Join(names, " ") != "a.txt b.txt n.txt" {
		t.Errorf("the index holds %v", names)
	}
}
