package release_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

// aPatchedMember is a stack of one member called x. The stack has a patch
// of its own that changes one of the member's files.
func aPatchedMember(
	t *testing.T, files map[string]string, file, body string,
) (repo.Repo, string, []release.Member) {
	t.Helper()
	own, dist, members := stackOf(t, "x", files)
	beside := filepath.Dir(own.Dir())
	at := filepath.Join(beside, "x")
	patch := diffOf(t, at, file, body)
	addFiles(t, own.Dir(), map[string]string{"patches/x/0001-it.patch": patch})
	return own, dist, members
}

// TestTheWorkspaceIsWrittenFromWhatThePatchesLeave: a patch of the
// stack's own that changes a member's go.mod changes the go.work. The go
// version is that of the file in the stack and not the one the member
// published.
func TestTheWorkspaceIsWrittenFromWhatThePatchesLeave(t *testing.T) {
	t.Parallel()
	own, dist, members := aPatchedMember(t, map[string]string{
		"go.mod": "module example.com/x\n\ngo 1.25\n",
		"x.go":   "package x\n",
	}, "go.mod", "module example.com/y\n\ngo 1.27\n")
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Work(), "go 1.27\n") {
		t.Errorf("the go.work has the old go version:\n%s", out.Work())
	}
}

// TestAPatchedGameFileIsTheOneThatCounts: a patch that changes a member's
// .game changes what the stack does with the member. Its omit lines apply
// and its text is the one handed back.
func TestAPatchedGameFileIsTheOneThatCounts(t *testing.T) {
	t.Parallel()
	own, dist, members := aPatchedMember(t, map[string]string{
		".game":      "name x\n",
		"secret.txt": "kept out\n",
		"x.txt":      "x\n",
	}, ".game", "name x\nomit secret.txt\n")
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	files := filesAt(t, own, out.Commit())
	if strings.Contains(strings.Join(files, " "), "secret.txt") {
		t.Errorf("the patched omit line was not applied: %v", files)
	}
	if !strings.Contains(out.Games()["x"], "omit secret.txt") {
		t.Errorf("the game handed back is %q", out.Games()["x"])
	}
}

// TestAModuleAMemberOmitsIsNotInTheWorkspace: the go.work uses what the
// stack keeps. Modules under a directory the member omits are not used.
// Go could not load a workspace that names a directory not there.
func TestAModuleAMemberOmitsIsNotInTheWorkspace(t *testing.T) {
	t.Parallel()
	own, dist, members := stackOf(t, "x", map[string]string{
		".game":         "name x\nomit drafts\n",
		"go.mod":        "module example.com/x\n\ngo 1.25\n",
		"x.go":          "package x\n",
		"drafts/go.mod": "module example.com/drafts\n\ngo 1.25\n",
	})
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Work(), "drafts") ||
		!strings.Contains(out.Work(), "\t./x\n") {
		t.Errorf("the go.work:\n%s", out.Work())
	}
}

// twoVersions is the go versions of two members and the one the
// go.work is to name.
type twoVersions struct {
	first, second, want string
}

// check is the pair as a subtest: the go.work names the higher.
func (pair twoVersions) check(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	for name, version := range map[string]string{
		"a": pair.first, "b": pair.second} {
		addFiles(t, memberDir(own, name), map[string]string{
			"go.mod": "module example.com/" + name + "\n\ngo " + version +
				"\n// " + name + "\n"})
	}
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if want := "go " + pair.want + "\n"; !strings.Contains(
		out.Work(), want) {
		t.Errorf("the go.work lacks %q:\n%s", want, out.Work())
	}
}

// TestTheHighestGoVersionIsTheOneGoCallsHighest: go puts 1.26 above 1.9
// and 1.25rc1 above 1.25. Comparing the strings gets the first wrong.
func TestTheHighestGoVersionIsTheOneGoCallsHighest(t *testing.T) {
	t.Parallel()
	pairs := []twoVersions{
		{"1.9", "1.26", "1.26"}, {"1.26", "1.9", "1.26"},
		{"1.25", "1.25rc1", "1.25rc1"}, {"1.25rc1", "1.25", "1.25rc1"},
		{"1.25", "1.25.3", "1.25.3"}, {"1.25.3", "1.25", "1.25.3"},
	}
	for place, pair := range pairs {
		t.Run(fmt.Sprintf("%d", place), pair.check)
	}
}

// TestAMemberGameFileThatDoesNotParseRefusesTheStack: one typo does not
// drop the member's omit lines and lint rules without a word. The refusal
// names the member and the line.
func TestAMemberGameFileThatDoesNotParseRefusesTheStack(t *testing.T) {
	t.Parallel()
	own, dist, members := stackOf(t, "x", map[string]string{
		".game": "name x\nomit secret.txt\nlint nonsense 3\n",
		"x.txt": "x\n",
	})
	_, err := release.Stack(own, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "x: its .game: .game:3:") {
		t.Errorf("err %v, want x's .game refused at line 3", err)
	}
	if kept, _ := own.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// aGameSpelling is a member's .game that says the same two things in one
// way of writing them: exclude notes/, and allow paths in the README.
type aGameSpelling struct {
	name, game string
}

// someGameSpellings are the ways game reads a line.
func someGameSpellings() []aGameSpelling {
	return []aGameSpelling{
		{"plain", "name x\nexclude notes/\nsweep allow README.md paths\n"},
		{"equals signs", "name x\nexclude = notes/\nsweep = allow README.md paths\n"},
		{"tabs", "name x\nexclude\tnotes/\nsweep\tallow\tREADME.md\tpaths\n"},
		{"quoted", "name x\nexclude \"notes/\"\nsweep allow README.md paths\n"},
	}
}

// check is the spelling as a subtest: the stack is cut with the leak in
// notes/ left unread and the leak in the README allowed.
func (spelling aGameSpelling) check(t *testing.T) {
	t.Parallel()
	own, dist, members := stackOf(t, "x", map[string]string{
		".game":          spelling.game,
		"README.md":      "x: " + stackLeak + "\n",
		"notes/leak.txt": stackLeak + "\n",
	})
	if _, err := release.Stack(own, stackOptions(dist), members); err != nil {
		t.Fatalf("%s: %v", spelling.name, err)
	}
}

// TestEverySpellingOfAMembersGameCounts: the stack reads a member's .game
// as game reads any .game, so a line counts however it is written.
func TestEverySpellingOfAMembersGameCounts(t *testing.T) {
	t.Parallel()
	for _, spelling := range someGameSpellings() {
		t.Run(spelling.name, spelling.check)
	}
}
