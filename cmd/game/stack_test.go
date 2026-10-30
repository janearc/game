package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// gitAt runs git in a directory and fails the test if git does.
func gitAt(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFiles writes files into a repository and commits them.
func commitFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		write(t, filepath.Join(dir, name), body)
	}
	gitAt(t, dir, "add", ".")
	gitAt(t, dir, "commit", "-q", "-m", "fixture")
}

// failingTest is a config whose test target fails.
func failingTest(t *testing.T) config.Config {
	t.Helper()
	return gamed(t, "target test :: false")
}

// freshTree is a repository named name in a new directory, with one
// commit of the files given.
func freshTree(t *testing.T, name string, files map[string]string) repo.Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	return gitTree(t, dir, files)
}

// gitTree is a repository at dir with one commit of the files given.
func gitTree(t *testing.T, dir string, files map[string]string) repo.Repo {
	t.Helper()
	if err := os.MkdirAll(dir, os.FileMode(0o755)); err != nil {
		t.Fatal(err)
	}
	gitAt(t, dir, "init", "-q", "-b", "main")
	gitAt(t, dir, "config", "commit.gpgsign", "false")
	gitAt(t, dir, "config", "user.email", "stack@example.invalid")
	gitAt(t, dir, "config", "user.name", "stack")
	gitAt(t, dir, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	commitFiles(t, dir, files)
	return repoAt(dir)
}

// lintChild is a stand-in for game verify: it reports a lint count and
// exits as told.
type lintChild struct{ lint, exit int }

// child is the stand-in as a member's child.
func (stand lintChild) child(string) *exec.Cmd {
	return exec.Command("sh", "-c", fmt.Sprintf(
		`echo '{"step":"lint","state":"ok","count":%d}'; exit %d`,
		stand.lint, stand.exit,
	))
}

// mixedVerify is clean for a and has findings for b, so the count of
// what was not clean is neither all nor none.
type mixedVerify struct{ findings int }

// child is the stand-in as a member's child.
func (mixed mixedVerify) child(dir string) *exec.Cmd {
	if filepath.Base(dir) == "a" {
		return lintChild{}.child(dir)
	}
	return lintChild{lint: mixed.findings}.child(dir)
}

// neverRuns is a child that notes it ran, which it should not.
type neverRuns struct{ ran bool }

// child notes the run.
func (never *neverRuns) child(string) *exec.Cmd {
	never.ran = true
	return exec.Command("true")
}

// a stack built here takes the members from this clone and holds each
// to its child's verdict: every member clean, or the count that was
// not, lint findings counting as not clean, and a failed member
// stopping the run there.
func TestBuildStackHereCountsWhatBuiltClean(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		write(t, filepath.Join(dir, name, ".game"), "name "+name+"\n")
	}
	tree := repoAt(dir)
	cfg := gamed(t, "stack a main", "stack b main")
	findings := countHeld(t, "some-findings")
	cases := []struct {
		name  string
		child func(string) *exec.Cmd
		want  string
		err   string
	}{
		{"clean", lintChild{}.child, "stack: 2 of 2 built clean", ""},
		{"lint findings", lintChild{lint: findings}.child,
			"stack: 0 of 2 built clean", "2 did not build clean"},
		{"one of two", mixedVerify{findings}.child,
			"stack: 1 of 2 built clean", "1 did not build clean"},
		{"a failure", lintChild{exit: exitFailed}.child,
			"stack: 0 of 1 built clean", "1 did not build clean"},
	}
	for _, each := range cases {
		var out bytes.Buffer
		err := argsBuildStack(t, tree, cfg, []string{"--here"}, &out, each.child)
		said := out.String()
		if !strings.Contains(said, "stack: 2 members from this clone") ||
			!strings.Contains(said, each.want) {
			t.Errorf("%s: said\n%s", each.name, said)
		}
		if (each.err == "" && err != nil) || (each.err != "" &&
			(err == nil || !strings.Contains(err.Error(), each.err))) {
			t.Errorf("%s: err %v", each.name, err)
		}
	}
}

// with no stack described, a from that is neither road nor dist, a
// member with no remote of that kind, or a road that is not there,
// build stack says so and the child never runs.
func TestBuildStackRefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tree := repoAt(dir)
	nowhere := filepath.Join(dir, "nowhere.git")
	cfg := configOf(t, block("a", nowhere, ""), []string{"stack a main"})
	never := &neverRuns{}
	cases := []struct {
		name string
		cfg  config.Config
		args []string
		want string
	}{
		{"no stack", blankConfig(), nil, "no stack described"},
		{"a from that is neither", cfg, []string{"--from", "nope"},
			"--from is road or dist"},
		{"no dist for the member", cfg, nil, "1 did not build clean"},
		{"a road that is not there", cfg, []string{"--from", "road"},
			"1 did not build clean"},
	}
	for _, each := range cases {
		var out bytes.Buffer
		err := argsBuildStack(t, tree, each.cfg, each.args, &out, never.child)
		if err == nil || !strings.Contains(err.Error(), each.want) {
			t.Errorf("%s: want %q, got %v\n%s", each.name, each.want, err,
				out.String())
		}
	}
	if never.ran {
		t.Error("the child ran for a member that could not be fetched")
	}
}

// from the road or from the dist, a member is cloned at its ref under
// bin/stack and built; the remote asked for is the one used, the other
// here being nowhere.
func TestBuildStackClonesTheMemberFromTheRemoteAskedFor(t *testing.T) {
	t.Parallel()
	for _, from := range []string{"road", "dist"} {
		root := t.TempDir()
		at := filepath.Join(root, "a")
		member := gitTree(t, at, map[string]string{
			".game": "name a\n",
		})
		nowhere := filepath.Join(root, "nowhere.git")
		remotes := block("a", member.Dir(), nowhere)
		if from == "dist" {
			remotes = block("a", nowhere, member.Dir())
		}
		tree := repoAt(filepath.Join(root, "own"))
		if err := os.MkdirAll(tree.Dir(), os.FileMode(0o755)); err != nil {
			t.Fatal(err)
		}
		cfg := configOf(t, remotes, []string{"stack a main"})
		var out bytes.Buffer
		err := argsBuildStack(t, tree, cfg, []string{"--from", from}, &out,
			lintChild{}.child)
		if err != nil || !strings.Contains(out.String(), "stack: 1 of 1 built clean") {
			t.Errorf("from %s: err %v\n%s", from, err, out.String())
		}
		cloned := tree.Path("bin", "stack", "a", ".game")
		if _, err := os.Stat(cloned); err != nil {
			t.Errorf("from %s: the member was not cloned under bin/stack", from)
		}
	}
}

// a work directory that cannot be made stops the build before any
// member is reached.
func TestBuildStackStopsWhenItCannotMakeItsWorkDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "in the way\n")
	cfg := configOf(t, block("a", "", dir), []string{"stack a main"})
	never := &neverRuns{}
	err := argsBuildStack(t, repoAt(dir), cfg, nil, &bytes.Buffer{}, never.child)
	if err == nil || never.ran {
		t.Errorf("err %v, child ran %v", err, never.ran)
	}
}

// the assembly is held to the strictest of every member's rules: one
// that is not clean is refused unless meant, with the counts said; an
// allow line travels under the member's name; a clean assembly says so;
// no rules, or a .game that does not parse, is nothing to say.
func TestStackLintHoldsTheAssemblyToEveryMembersRules(t *testing.T) {
	t.Parallel()
	tree := freshTree(t, "s", map[string]string{
		"m/README.md": "wow!\n" + pastTheWidth(t) + "\n",
		"n/README.md": "fine\n",
	})
	head := gitAt(t, tree.Dir(), "rev-parse", "HEAD")
	none := blankConfig()
	cases := []struct {
		name  string
		games map[string]string
		mean  bool
		said  string
		err   string
	}{
		{"not clean", map[string]string{"m": "lint exclaim\nlint width 40\n"},
			false, "stack lint: 2 to fix, 0 to look at",
			"not clean by the rules its members keep"},
		{"not clean, but meant",
			map[string]string{"m": "lint exclaim\nlint width 40\n"},
			true, "stack lint: 2 to fix, 0 to look at", ""},
		{"an allowance under the member's name",
			map[string]string{"m": "lint exclaim\nlint allow README.md exclaim\n"},
			false, "stack lint: clean, by every member's rules", ""},
		{"clean", map[string]string{"n": "lint rows 100\n"}, false,
			"stack lint: clean, by every member's rules", ""},
		{"no rules", nil, false, "", ""},
		{"a .game that does not parse", map[string]string{"m": "nonsense\n"},
			false, "", ""},
	}
	for _, each := range cases {
		var out bytes.Buffer
		err := stackLint(tree, none, head, each.games, each.mean, &out)
		if !strings.Contains(out.String(), each.said) ||
			(each.said == "" && count.Positive(out.Len())) {
			t.Errorf("%s: said\n%s", each.name, out.String())
		}
		if (each.err == "" && err != nil) || (each.err != "" &&
			(err == nil || !strings.Contains(err.Error(), each.err))) {
			t.Errorf("%s: err %v", each.name, err)
		}
	}
}

// pastTheWidth is a line one past the width the lint holds to.
func pastTheWidth(t *testing.T) string {
	t.Helper()
	width := countHeld(t, "lint-width")
	return strings.Repeat("x", count.Next(width))
}

// stackFixture is the stack's own repository, an empty bare dist, one
// member with a module, and the config that names them.
func stackFixture(t *testing.T) (repo.Repo, string, repo.Repo, config.Config) {
	t.Helper()
	root := t.TempDir()
	own := gitTree(t, filepath.Join(root, "own"),
		map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitAt(t, root, "init", "-q", "--bare", dist)
	at := filepath.Join(root, "a")
	member := gitTree(t, at, map[string]string{
		"go.mod": "module example.com/a\n\ngo 1.26\n",
		"a.go":   "package a\n\n// A is a.\nfunc A() {}\n",
		".game":  "name a\n",
	})
	stackBlock := block("s", "", dist)
	memberBlock := block("a", "", member.Dir())
	projects := slices.Concat(stackBlock, memberBlock)
	cfg := loadedAs(t, "s", projects, []string{"stack a main"})
	return own, dist, member, cfg
}

// release stack prepares the assembly from the members' dists and says
// what it prepared and how to send it; an assembly that is not clean by
// its members' rules is refused and its ref deleted, unless meant.
func TestReleaseStackPreparesAndSaysSo(t *testing.T) {
	t.Parallel()
	own, dist, member, cfg := stackFixture(t)
	var out bytes.Buffer
	var err error
	err = releaseStack(own, cfg, "v1", "dist", false, &out, testCommit, testBuilt)
	said := out.String()
	for _, line := range []string{
		"stack s v1: 1 members from their dists, prepared ",
		"  go.work: a\n",
		"send it with: game release push v1, to dist (clean): " + dist,
	} {
		if err != nil || !strings.Contains(said, line) {
			t.Fatalf("err %v, said\n%s", err, said)
		}
	}
	if kept, _ := own.Git("rev-parse", "--verify", "-q", refOf("v1")); kept == "" {
		t.Error("no ref for the prepared stack")
	}
	commitFiles(t, member.Dir(), map[string]string{
		".game":     "name a\nlint exclaim\n",
		"README.md": "wow!\n",
	})
	out.Reset()
	err = releaseStack(own, cfg, "v2", "dist", false, &out, testCommit, testBuilt)
	kept, _ := own.Git("rev-parse", "--verify", "-q", refOf("v2"))
	if err == nil || !strings.Contains(err.Error(), "not clean") || kept != "" {
		t.Errorf("not clean: err %v, ref %q\n%s", err, kept, out.String())
	}
	err = releaseStack(own, cfg, "v2", "dist", true, &out, testCommit, testBuilt)
	if err != nil {
		t.Errorf("meant: %v", err)
	}
}

// release stack refuses before it starts: an unstamped game, no block
// for the stack, a block with no dist, no members, a from that is
// neither road nor dist.
func TestReleaseStackRefusesBeforeItStarts(t *testing.T) {
	t.Parallel()
	own, dist, member, cfg := stackFixture(t)
	members := []string{"stack a main"}
	memberBlock := block("a", "", member.Dir())
	noBlock := loadedAs(t, "s", memberBlock, members)
	roadOnly := block("s", "road.git", "")
	noDistBlocks := slices.Concat(roadOnly, memberBlock)
	noDist := loadedAs(t, "s", noDistBlocks, members)
	distOnly := block("s", "", dist)
	noStackBlocks := slices.Concat(distOnly, memberBlock)
	noStack := loadedAs(t, "s", noStackBlocks, nil)
	var out bytes.Buffer
	err := releaseStack(own, cfg, "v1", "dist", false, &out, devCommit, "")
	if err == nil || !strings.Contains(err.Error(), "built by go rather than by game") {
		t.Errorf("unstamped: %v", err)
	}
	cases := []struct {
		name string
		cfg  config.Config
		from string
		want string
	}{
		{"no block", noBlock, "dist", noBlock.NoBlock().Error()},
		{"no dist", noDist, "dist", "s has no dist in its block"},
		{"no members", noStack, "dist", "no members"},
		{"a from that is neither", cfg, "nope", "--from is road or dist"},
	}
	for _, each := range cases {
		err = releaseStack(own, each.cfg, "v1", each.from, false, &out, testCommit, testBuilt)
		if err == nil || !strings.Contains(err.Error(), each.want) {
			t.Errorf("%s: want %q, got %v", each.name, each.want, err)
		}
	}
}

// a stack's members carry their remotes, and a member the .game marks
// theirs is passed on as theirs; the others are not.
func TestStackMembersCarryTheirs(t *testing.T) {
	t.Parallel()
	cfg := gamed(t, "stack crush v0.98.1 theirs", "stack keel main")
	got := stackMembers(cfg, map[string]string{
		"crush": "/roads/crush.git", "keel": "/roads/keel.git",
	})
	want := []release.Member{
		release.Taken("crush", "v0.98.1", "/roads/crush.git", true),
		release.Taken("keel", "main", "/roads/keel.git", false),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("members %v, want %v", got, want)
	}
}

// what a stack release says: the commit it prepared, its workspace, the
// patches laid on each member that had any, and how to send it; or, when
// dist has the tree already, that alone.
func TestTellStackedSaysWhatWasLaid(t *testing.T) {
	t.Parallel()
	members := []string{"crush", "keel", "okapi"}
	patched := map[string]int{
		"crush": count.Next(count.One), "keel": count.One}
	res := release.NewStacked("0123456789abcdef", false, members, nil,
		"go 1.27\n", nil, patched)
	var out bytes.Buffer
	tellStacked(&out, "fancy", "v1", "road", "/dist.git", res)
	want := "stack fancy v1: 3 members from their roads, prepared 0123456\n" +
		"  go.work: crush keel okapi\n" +
		"  crush: 2 patches laid\n" +
		"  keel: 1 patch laid\n" +
		"send it with: game release push v1, to dist (clean): /dist.git\n"
	if out.String() != want {
		t.Errorf("said:\n%s\nwant:\n%s", out.String(), want)
	}
	res = release.NewStacked("0123456789abcdef", true, members, nil,
		"go 1.27\n", nil, patched)
	out.Reset()
	tellStacked(&out, "fancy", "v1", "road", "/dist.git", res)
	if got := out.String(); got !=
		"stack fancy v1: dist already has this tree at 0123456\n" {
		t.Errorf("skipped said %q", got)
	}
}

// a finding in a member marked theirs is left to that member's own
// rules, staticcheck's by the path its text begins with; one in a
// directory that only begins with the member's name is not theirs.
func TestNotTheirsLeavesTheirFindingsOut(t *testing.T) {
	t.Parallel()
	none := count.Nothing
	found := []lint.Finding{
		lint.Report("crush/app.go", none, "width", ""),
		lint.Report("crushed/app.go", none, "width", ""),
		lint.Report("keel/keel.go", none, "uncommented", ""),
		lint.Report("README.md", none, "paragraph", ""),
		lint.Report("staticcheck", none, "staticcheck",
			"crush/internal/app/app.go:12:3: unused (U1000)"),
		lint.Report("staticcheck", none, "staticcheck",
			"keel/keel.go:4:1: unused (U1000)"),
	}
	kept, left := notTheirs(found, []string{"crush", "bubbles"})
	var files []string
	for _, finding := range kept {
		files = append(files, finding.File()+" "+finding.Text())
	}
	want := "crushed/app.go ,keel/keel.go ,README.md ," +
		"staticcheck keel/keel.go:4:1: unused (U1000)"
	// the two that are crush's: its own file, and its staticcheck line
	if strings.Join(files, ",") != want ||
		left != len(found)-len(kept) {
		t.Errorf("kept %q, left %d", files, left)
	}
}

// a member marked theirs is held to its own rules only: our findings in
// it are left out and their count said. a finding only to look at is
// counted as one, and a commit that cannot be exported is an error.
func TestStackLintLeavesTheirsToTheirOwnRules(t *testing.T) {
	t.Parallel()
	tree := freshTree(t, "s", map[string]string{
		"m/README.md": "wow!\n" + pastTheWidth(t) + "\n",
		"n/n.go": "package n\n\n// Say is LOUDLY about it.\n" +
			"func Say() {}\n",
	})
	head := gitAt(t, tree.Dir(), "rev-parse", "HEAD")
	games := map[string]string{
		"m": "lint exclaim\nlint width 40\n",
		"n": "lint shout\n",
	}
	var out bytes.Buffer
	err := stackLint(tree, gamed(t, "stack m main theirs"), head,
		games, false, &out)
	for _, said := range []string{
		"stack lint: 2 in members marked theirs, left to their own rules",
		"stack lint: 0 to fix, 1 to look at",
	} {
		if !strings.Contains(out.String(), said) {
			t.Errorf("did not say %q:\n%s", said, out.String())
		}
	}
	if err == nil {
		t.Error("a finding to look at was not refused")
	}
	out.Reset()
	if err := stackLint(tree, gamed(t, "stack m main theirs", "stack n main theirs"),
		head, games, false, &out); err != nil ||
		!strings.Contains(out.String(), "3 in members marked theirs") {
		t.Errorf("all theirs: %v\n%s", err, out.String())
	}
	out.Reset()
	_ = stackLint(tree, gamed(t, "stack n main theirs"), head, games,
		false, &out)
	if !strings.Contains(out.String(), "stack lint: 1 in members marked") {
		t.Errorf("one theirs:\n%s", out.String())
	}
	out.Reset()
	_ = stackLint(tree, blankConfig(), head, games, false, &out)
	if strings.Contains(out.String(), "marked theirs") {
		t.Errorf("none theirs said so:\n%s", out.String())
	}
	nowhere := strings.Repeat("0", len(fakeObject))
	if err := stackLint(tree, blankConfig(), nowhere, games, false,
		&bytes.Buffer{}); err == nil {
		t.Error("a commit that is not there was linted")
	}
}

// release stack on a claimed root is refused before anything is
// assembled, as a release is: the claim would reach dist.
func TestReleaseStackRefusesAClaimedRoot(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	_, _, _, cfg := stackFixture(t)
	if _, err := sticky.Stick(one, road, "partner"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	err := releaseStack(one, cfg, "v1", "dist", false, io.Discard, testCommit, testBuilt)
	if err == nil || !strings.Contains(err.Error(), "unstick first") {
		t.Fatalf("want the unstick refusal, got %v", err)
	}
	kept, _ := one.Git("rev-parse", "--verify", "-q", refOf("v1"))
	if kept != "" {
		t.Errorf("a refused stack kept %q", kept)
	}
}

// Release stack holds the root to what a release holds its tree to.
// Needs unset refuse it before it assembles. The refusal says which
// setting.
func TestReleaseStackChecksTheRootAsAReleaseDoes(t *testing.T) {
	t.Parallel()
	own, dist, member, _ := stackFixture(t)
	members := block("a", "", member.Dir())
	projects := slices.Concat(block("s", "", dist), members)
	needing := loadedAs(t, "s", projects,
		[]string{"stack a main", "needs TOKEN a test"})
	var out bytes.Buffer
	err := releaseStack(own, needing, "v1", "dist", false, &out, testCommit, testBuilt)
	if err == nil || !strings.Contains(err.Error(),
		"set the needed settings") ||
		!strings.Contains(out.String(), "TOKEN is needed: a test") {
		t.Fatalf("a setting unset: %v, said %q", err, out.String())
	}
	kept, _ := own.Git("rev-parse", "--verify", "-q", refOf("v1"))
	if kept != "" {
		t.Errorf("a refused stack kept %q", kept)
	}
}

// release stack refused for lint leaves the stack an earlier release
// stack kept at the same tag where it was.
func TestReleaseStackKeepsAnEarlierStackWhenItRefuses(t *testing.T) {
	t.Parallel()
	own, _, member, cfg := stackFixture(t)
	if err := releaseStack(own, cfg, "v1", "dist", false, io.Discard,
		testCommit, testBuilt); err != nil {
		t.Fatal(err)
	}
	earlier, _ := own.Git("rev-parse", "--verify", refOf("v1"))
	commitFiles(t, member.Dir(), map[string]string{
		".game":     "name a\nlint exclaim\n",
		"README.md": "wow!\n",
	})
	err := releaseStack(own, cfg, "v1", "dist", false, io.Discard, testCommit, testBuilt)
	if err == nil || !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("not clean: %v", err)
	}
	kept, _ := own.Git("rev-parse", "--verify", refOf("v1"))
	if kept != earlier || kept == "" {
		t.Errorf("the earlier stack is %q, want %q", kept, earlier)
	}
}

// with --here the members are in this clone and --from is not asked, so
// a from that is neither road nor dist is no error.
func TestBuildStackHereDoesNotAskFrom(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", ".game"), "name a\n")
	tree := repoAt(dir)
	cfg := gamed(t, "stack a main")
	var out bytes.Buffer
	args := []string{"--here", "--from", "nope"}
	if err := argsBuildStack(t, tree, cfg, args, &out, lintChild{}.child); err != nil {
		t.Errorf("%v\n%s", err, out.String())
	}
}

// release stack on a root whose claim cannot be read is refused: it will
// not say the root is free of a claim it could not look at.
func TestReleaseStackRefusesAClaimItCannotRead(t *testing.T) {
	t.Parallel()
	one, _, _, _ := stickyFixture(t)
	_, _, _, cfg := stackFixture(t)
	stuckUnreadably(t, one)
	err := releaseStack(one, cfg, "v1", "dist", false, io.Discard, testCommit, testBuilt)
	if err == nil || !strings.Contains(err.Error(), ".sticky") {
		t.Errorf("an unreadable claim: %v, want .sticky named", err)
	}
	if kept, _ := one.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %q", kept)
	}
}

// a stack whose member does not vet is refused, and no ref is kept. The
// stack of the same member before it broke was prepared.
func TestReleaseStackRefusesAMemberThatDoesNotVet(t *testing.T) {
	t.Parallel()
	own, _, member, cfg := stackFixture(t)
	if err := releaseStack(own, cfg, "v1", "dist", false, io.Discard,
		testCommit, testBuilt); err != nil {
		t.Fatalf("the member as it was: %v", err)
	}
	commitFiles(t, member.Dir(), map[string]string{
		"a.go": "package a\n\nimport \"fmt\"\n\n" +
			"// A is a.\nfunc A() { fmt.Printf(\"%d\\n\", \"a\") }\n",
	})
	err := releaseStack(own, cfg, "v2", "dist", false, io.Discard, testCommit, testBuilt)
	kept, _ := own.Git("rev-parse", "--verify", "-q", refOf("v2"))
	if err == nil || !strings.Contains(err.Error(), "vet") || kept != "" {
		t.Errorf("a member that does not vet: err %v, ref %q", err, kept)
	}
}

// a stack whose member's tests fail is refused, and no ref is kept.
func TestReleaseStackRefusesAMemberWhoseTestsFail(t *testing.T) {
	t.Parallel()
	own, _, member, cfg := stackFixture(t)
	commitFiles(t, member.Dir(), map[string]string{
		"a_test.go": "package a\n\nimport \"testing\"\n\n" +
			"func TestA(t *testing.T) { t.Fatal(\"no\") }\n",
	})
	err := releaseStack(own, cfg, "v1", "dist", false, io.Discard, testCommit, testBuilt)
	kept, _ := own.Git("rev-parse", "--verify", "-q", refOf("v1"))
	if err == nil || !strings.Contains(err.Error(), "FAIL") || kept != "" {
		t.Errorf("a member whose tests fail: err %v, ref %q", err, kept)
	}
}
