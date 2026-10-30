package release_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

// the leak is assembled at run time so this file spells no machine
// path; the sweep reads this tree too.
var stackLeak = "kept at /Us" + "ers/ada/work"

// gitIn runs git in a directory and fails the test if git does.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitRepo is a repository at dir with one commit of the files given.
func gitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(dir, openDir); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	gitIn(t, dir, "config", "user.email", "stack@example.invalid")
	gitIn(t, dir, "config", "user.name", "stack")
	gitIn(t, dir, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	addFiles(t, dir, files)
}

// addFiles writes files into a repository and commits them.
func addFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "fixture")
}

// stackFixture is the stack's own repository, an empty bare dist, and
// two members: a, a module whose .game excludes notes/ and allows a path
// in its README; and b, a module that requires a at a version.
func stackFixture(t *testing.T) (repo.Repo, string, []release.Member) {
	t.Helper()
	root := t.TempDir()
	own := filepath.Join(root, "own")
	gitRepo(t, own, map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitIn(t, root, "init", "-q", "--bare", dist)
	atA := filepath.Join(root, "a")
	gitRepo(t, atA, map[string]string{
		"go.mod":         "module example.com/a\n\ngo 1.25\n",
		"a.go":           "package a\n",
		".game":          "name a\nexclude notes/\nsweep allow README.md paths\n",
		"README.md":      "a: " + stackLeak + "\n",
		"notes/leak.txt": stackLeak + "\n",
	})
	atB := filepath.Join(root, "b")
	gitRepo(t, atB, map[string]string{
		"go.mod": "module example.com/b\n\ngo 1.26\n\n" +
			"require example.com/a v0.1.0\n",
		"b.go": "package b\n",
	})
	members := []release.Member{
		release.Taken("a", "main", atA, false),
		release.Taken("b", "main", atB, false),
	}
	return repoAt(own), dist, members
}

// stackOptions is what every stack here is cut with.
func stackOptions(dist string) release.Options {
	return release.For("stack", dist, "v1").
		Swept(sworn, nil, nil).
		By("Ada Lovelace")
}

// the members land beside the root under their names, the workspace
// uses both and points b's requirement at a's copy here, each member's
// .game is kept for the caller, and the commit waits at the release
// ref with the project and tag as its message.
func TestStackLaysMembersBesideTheRootAndWritesAWorkspace(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Members(), " ") != "a b" || parentsOf(t, own, out.Commit()) != "" ||
		out.Skipped() {
		t.Errorf("stacked: %+v", out)
	}
	files := filesAt(t, own, out.Commit())
	want := "README.md a/.game a/README.md a/a.go a/go.mod " +
		"a/notes/leak.txt b/b.go b/go.mod go.work"
	if strings.Join(files, " ") != want {
		t.Errorf("files %v", files)
	}
	for _, line := range []string{
		"go 1.26", "\t./a\n", "\t./b\n",
		"replace example.com/a v0.1.0 => ./a\n",
	} {
		if !strings.Contains(out.Work(), line) {
			t.Errorf("go.work lacks %q:\n%s", line, out.Work())
		}
	}
	work, _ := own.Show(out.Commit(), "go.work")
	if work != strings.TrimSpace(out.Work()) {
		t.Errorf("the go.work in the commit differs:\n%s", work)
	}
	if !strings.Contains(out.Games()["a"], "exclude notes/") ||
		out.Games()["b"] != "" {
		t.Errorf("games %q", out.Games())
	}
	if kept, _ := own.Git("rev-parse", refOf("v1")); kept != out.Commit() {
		t.Errorf("ref %q, commit %q", kept, out.Commit())
	}
	msg, _ := own.Git("log", "-1", "--format=%s", out.Commit())
	if msg != "stack v1" {
		t.Errorf("message %q", msg)
	}
}

// with the same tree already on dist's main, the stack reuses that
// commit and says so; with a different one there, it parents on it.
func TestStackParentsOnDistOrSkipsWhenDistHasTheTree(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	first, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, own.Dir(), "push", "-q", dist,
		first.Commit()+":refs/heads/main")
	again, err := release.Stack(own, stackOptions(dist), members)
	if err != nil || !again.Skipped() || again.Commit() != first.Commit() {
		t.Errorf("the same tree again: %+v %v", again, err)
	}
	other := gitIn(t, own.Dir(), "rev-parse", "HEAD")
	gitIn(t, own.Dir(), "push", "-q", "-f", dist,
		other+":refs/heads/main")
	third, err := release.Stack(own, stackOptions(dist), members)
	parent := parentsOf(t, own, third.Commit())
	if err != nil || third.Skipped() || parent != other {
		t.Errorf("a different tree on dist: %+v %v", third, err)
	}
}

// withValues is the stack held to a setting's value.
func withValues(dist string) release.Options {
	return stackOptions(dist).Refusing([]string{"hunter2-not-a-real-one"})
}

// withExclude is the stack leaving the member's files out.
func withExclude(dist string) release.Options {
	return stackOptions(dist).Swept(sworn, []string{"b/"}, nil)
}

// withAllow is the stack letting the member's file carry a path.
func withAllow(dist string) release.Options {
	return stackOptions(dist).Swept(sworn, nil,
		map[string][]string{"b/leak.txt": {"paths"}})
}

// what the sweep finds refuses the whole stack and leaves no ref: a path,
// a never-word, the author, a setting's value. a member's own exclude
// and allow lines let its marked files by, and so do the caller's. one
// fixture serves every case; b is wound back between them.
func TestStackRefusesWhatTheSweepFindsAndHonoursAllowances(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	cases := []struct {
		name, file, body string
		options          func(string) release.Options
		want             string
	}{
		{"a path", "leak.txt", stackLeak, stackOptions,
			"refused: machine paths in b/leak.txt"},
		{"a never-word", "thanks.md", "with thanks to Zorblax",
			stackOptions, "refused: a never-word in b/thanks.md"},
		{"the author", "AUTHORS", "by ada lovelace", stackOptions,
			"refused: the author Ada Lovelace in b/AUTHORS"},
		{"a value", "config.txt", "token hunter2-not-a-real-one",
			withValues, "refused: a setting's value in b/config.txt"},
		{"a path the caller excludes", "leak.txt", stackLeak,
			withExclude, ""},
		{"a path the caller allows", "leak.txt", stackLeak,
			withAllow, ""},
	}
	for _, each := range cases {
		addFiles(t, memberDir(own, "b"),
			map[string]string{each.file: each.body + "\n"})
		_, err := release.Stack(own, each.options(dist), members)
		kept, _ := own.Git("rev-parse", "--verify", "-q",
			refOf("v1"))
		switch {
		case each.want == "" && (err != nil || kept == ""):
			t.Errorf("%s: err %v, ref %q", each.name, err, kept)
		case each.want != "" && (err == nil ||
			!strings.Contains(err.Error(), each.want) || kept != ""):
			t.Errorf("%s: err %v, ref %q", each.name, err, kept)
		}
		gitIn(t, memberDir(own, "b"), "reset", "-q", "--hard", "HEAD~1")
		own.Git("update-ref", "-d", refOf("v1"))
	}
}

// a stack with no tag, no members, a member with no remote or a ref its
// remote does not have is refused before anything is written.
func TestStackRefusesBeforeItTouchesAnything(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	noTag := release.For("stack", dist, "").Swept(sworn, nil, nil).
		By("Ada Lovelace")
	landless := release.Taken("a", "main", "", false)
	missing := release.Taken("a", "nope",
		memberDir(own, "a"), false)
	cases := []struct {
		name    string
		options release.Options
		members []release.Member
		want    string
	}{
		{"no tag", noTag, members, "needs a tag"},
		{"no members", stackOptions(dist), nil, "no members"},
		{"no remote", stackOptions(dist), []release.Member{landless},
			`a has no remote: open a "project a" block`},
		{"a ref the remote lacks", stackOptions(dist),
			[]release.Member{missing},
			"a at nope from " + memberDir(own, "a")},
	}
	for _, each := range cases {
		_, err := release.Stack(own, each.options, each.members)
		if err == nil || !strings.Contains(err.Error(), each.want) {
			t.Errorf("%s: want %q, got %v", each.name, each.want, err)
		}
		kept, _ := own.Git("rev-parse", "--verify", "-q",
			refOf("v1"))
		if kept != "" {
			t.Errorf("%s: left a ref at %s", each.name, kept)
		}
	}
}

// stackOfModules stacks members that are each a go.mod of the text
// given, by name, in name order.
func stackOfModules(
	t *testing.T, mods map[string]string,
) (*release.Stacked, error) {
	t.Helper()
	root := t.TempDir()
	own := filepath.Join(root, "own")
	gitRepo(t, own, map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitIn(t, root, "init", "-q", "--bare", dist)
	var members []release.Member
	for _, name := range slices.Sorted(maps.Keys(mods)) {
		at := filepath.Join(root, name)
		gitRepo(t, at, map[string]string{"go.mod": mods[name]})
		members = append(members, release.Taken(name, "main", at, false))
	}
	return release.Stack(repoAt(own), stackOptions(dist), members)
}

// the workspace takes what it needs from the members' go.mod files and
// skips what it does not understand: the highest go directive, and a
// replace for each version asked of a sibling in a block or on a line,
// not one commented out, indirect or not a version.
func TestWorkspaceReadersTakeWhatTheyNeed(t *testing.T) {
	t.Parallel()
	asking := "module example.com/x\n\ngo 1.26\n\nrequire (\n" +
		"\texample.com/a v0.1.0\n\texample.com/b v0.2.0 // indirect\n" +
		"\t//gone v1.0.0\n\tbad\n)\n" +
		"require example.com/c v0.3.0\nrequire example.com/d latest\n"
	out, err := stackOfModules(t, map[string]string{
		"x":      asking,
		"a":      "module example.com/a\n\ngo 1.25\n",
		"b":      "module example.com/b\n\ngo 1.25\n",
		"c":      "module example.com/c\n\ngo 1.25\n",
		"d":      "module example.com/d\n\ngo 1.25\n",
		"gone":   "module gone\n\ngo 1.25\n",
		"nopath": "go 1.24\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"go 1.26\n", "\t./nopath\n",
		"replace example.com/a v0.1.0 => ./a\n",
		"replace example.com/b v0.2.0 => ./b\n",
		"replace example.com/c v0.3.0 => ./c\n",
	} {
		if !strings.Contains(out.Work(), line) {
			t.Errorf("go.work lacks %q:\n%s", line, out.Work())
		}
	}
	for _, line := range []string{
		"example.com/d", "replace gone", "bad", "go 1.24",
	} {
		if strings.Contains(out.Work(), line) {
			t.Errorf("go.work holds %q:\n%s", line, out.Work())
		}
	}
	plain, err := stackOfModules(t, map[string]string{
		"n": "module example.com/n\n",
	})
	if err != nil || !strings.Contains(plain.Work(), "go 1.25\n") {
		t.Errorf("no member says a go: %v\n%s", err, plain.Work())
	}
}

// the workspace lists the members sorted, and adds a replace for every
// version a member asks of a sibling that lives here, by path and then
// version.
func TestWorkIsSortedAndReplacesSiblings(t *testing.T) {
	t.Parallel()
	out, err := stackOfModules(t, map[string]string{
		"b": "module example.com/b\n",
		"a": "module example.com/a\n",
		"q": "module example.com/q\n\nrequire (\n\texample.com/b v0.1.0\n" +
			"\texample.com/a v0.9.0\n\texample.com/z v1.0.0\n)\n",
		"r": "module example.com/r\n\nrequire example.com/a v0.2.0\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Work(), "use (\n\t./a\n\t./b\n\t./q\n\t./r\n)\n") {
		t.Errorf("members unsorted:\n%s", out.Work())
	}
	replaces := out.Work()[strings.Index(out.Work(), "replace "):]
	if want := "replace example.com/a v0.2.0 => ./a\n" +
		"replace example.com/a v0.9.0 => ./a\n" +
		"replace example.com/b v0.1.0 => ./b\n"; replaces != want {
		t.Errorf("replaces\n%s\nwant\n%s", replaces, want)
	}
	alone, err := stackOfModules(t, map[string]string{
		"a": "module example.com/a\n",
	})
	if err != nil || strings.Contains(alone.Work(), "replace") {
		t.Errorf("replaces with nothing asked: %v\n%s", err, alone.Work())
	}
}

// a member's .game says what the stack's sweep leaves alone and what a
// file may carry: its exclude lines and its sweep allow lines, whatever
// the quotes. Files it says nothing of are held to the sweep.
func TestAMembersGameLinesReachTheSweep(t *testing.T) {
	t.Parallel()
	game := "name m\nexclude notes/\nexclude \"docs/\"\n" +
		"sweep allow README.md paths emails\n"
	address := "write to ada" + "@" + "ada.dev"
	files := map[string]string{
		".game":          game,
		"notes/leak.txt": stackLeak + "\n",
		"docs/leak.txt":  stackLeak + "\n",
		"README.md":      stackLeak + " " + address + "\n",
		"other.txt":      stackLeak + "\n",
		"x":              stackLeak + "\n",
	}
	root := t.TempDir()
	own := filepath.Join(root, "own")
	gitRepo(t, own, map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitIn(t, root, "init", "-q", "--bare", dist)
	at := filepath.Join(root, "m")
	gitRepo(t, at, files)
	members := []release.Member{release.Taken("m", "main", at, false)}
	_, err := release.Stack(repoAt(own), stackOptions(dist), members)
	if err == nil {
		t.Fatal("a leak in other.txt and x was let by")
	}
	for _, refused := range []string{"m/other.txt", "m/x"} {
		if !strings.Contains(err.Error(), "machine paths in "+refused) {
			t.Errorf("%s was not refused: %v", refused, err)
		}
	}
	for _, left := range []string{"m/notes/leak.txt", "m/docs/leak.txt", "m/README.md"} {
		if strings.Contains(err.Error(), left) {
			t.Errorf("%s was refused: %v", left, err)
		}
	}
}

// parentsOf is the parents of a commit, as one line, empty for a commit
// that has none.
func parentsOf(t *testing.T, work repo.Repo, commit string) string {
	t.Helper()
	line, err := work.Git("rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		t.Fatal(err)
	}
	_, parents, _ := strings.Cut(line, " ")
	return parents
}

// TestAMemberWithNoGameHasNoGameHandedBack: the games handed back are
// those the members have; one with no .game is not in them, empty or not.
func TestAMemberWithNoGameHasNoGameHandedBack(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	games := out.Games()
	if _, has := games["b"]; has || len(games) != count.One {
		t.Errorf("games %q: only a has a .game", games)
	}
	if _, has := games["a"]; !has {
		t.Errorf("games %q: a has a .game", games)
	}
}
