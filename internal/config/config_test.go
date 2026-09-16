package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// every form a line may take, and every key.
func TestParseForms(t *testing.T) {
	src := `# a comment
author Ada
words = ~/w.txt
project x
	road ../x-private.git
	dist	"https://example.com/x-dist.git"
	set CONTACT ada@example.com
exclude spec-docs/
exclude 'internal/bridge/'
lint width 80
lint words foo bar
`
	var c Config
	if err := c.Parse(strings.NewReader(src), "f", "/h", true); err != nil {
		t.Fatal(err)
	}
	if c.Author != "Ada" || c.Words != "/h/w.txt" ||
		c.Projects["x"].Road != "../x-private.git" ||
		c.Projects["x"].Dist != "https://example.com/x-dist.git" ||
		c.Projects["x"].Set["CONTACT"] != "ada@example.com" ||
		len(c.Exclude) != 2 ||
		c.Exclude[1] != "internal/bridge/" {
		t.Fatalf("got %+v", c)
	}
	if len(c.Lint) != 2 || c.Lint[0].Name != "width" ||
		c.Lint[0].Args[0] != "80" ||
		len(c.Lint[1].Args) != 2 {
		t.Fatalf("lint: %+v", c.Lint)
	}
}

// an unknown key, a key with no value, and a bad line each fail with
// the file and line named.
func TestParseRefuses(t *testing.T) {
	for _, src := range []string{
		"auther Ada\n",
		"author\n",
		"\n\n=\n",
		"lint dance\n",
		"public ../x.git\n",
		// a remote outside a block has no project to belong to
		"road ../x.git\n",
		"dist ../x.git\n",
		// a block wants a name
		"project\n",
		// and holds only a project's keys
		"project x\n\tauthor Ada\n",
		// the block already names the project
		"project x\n\troad x ../x.git\n",
	} {
		var c Config
		err := c.Parse(strings.NewReader(src), "the.file", "/h", true)
		if err == nil {
			t.Errorf("%q parsed", src)
			continue
		}
		if !strings.HasPrefix(err.Error(), "the.file:") {
			t.Errorf(
				"%q: error does not name the file and line: %v",
				src,
				err,
			)
		}
	}
}

// the dotfile then the repository's .game; the repository wins, except
// the word list, which it may not set; missing files are fine.
func TestLoadOrder(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	c, err := Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Projects) != 0 ||
		!strings.HasSuffix(c.Words, "sweep.words") {
		t.Fatalf("defaults: %+v", c)
	}
	os.MkdirAll(filepath.Join(home, ".config", "game"), 0o755)
	os.WriteFile(
		filepath.Join(home, ".config", "game", "config"),
		[]byte(
			"author Ada\nproject a\n\tdist ../a.git\nwords "+
				"~/mine\nexclude one/\nlint width 80\nlint "+
				"comments\n",
		),
		0o644,
	)
	os.WriteFile(
		filepath.Join(repo, ".game"),
		[]byte("name a\nwords ~/theirs\nexclude two/\n"),
		0o644,
	)
	c, err = Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	mine, held := c.Mine()
	if c.Author != "Ada" || c.Name != "a" || !held ||
		mine.Dist != filepath.Join(home, "..", "a.git") &&
			mine.Dist != "../a.git" ||
		c.Words != filepath.Join(home, "mine") ||
		len(c.Exclude) != 2 {
		t.Fatalf("merged: %+v", c)
	}
	if len(c.Lint) != 2 {
		t.Fatalf(
			"a repository that describes no lint should "+
				"get the dotfile's two, got %v",
			c.Lint,
		)
	}
	os.WriteFile(
		filepath.Join(repo, ".game"),
		[]byte("lint print\n"),
		0o644,
	)
	c, _ = Load(home, repo)
	if len(c.Lint) != 1 || c.Lint[0].Name != "print" {
		t.Fatalf(
			"a repository's lint should replace the "+
				"dotfile's, got %v",
			c.Lint,
		)
	}
	os.WriteFile(
		filepath.Join(repo, ".game"),
		[]byte("colour blue\n"),
		0o644,
	)
	if _, err := Load(home, repo); err == nil ||
		!strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("an unknown key in .game loaded: %v", err)
	}
}

// a target is a name, a directory and steps in order; a second line
// with the same name is the next step; a line without :: is refused.
func TestTargets(t *testing.T) {
	var c Config
	src := "target ghostty . :: sh tools/toolchain\ntarget ghostty " +
		"~/src/g :: ~/zig build -Doptimize=ReleaseFast\n"
	if err := c.Parse(strings.NewReader(src), "f", "/h", true); err != nil {
		t.Fatal(err)
	}
	if len(c.Targets) != 1 || len(c.Targets[0].Steps) != 2 {
		t.Fatalf("got %+v", c.Targets)
	}
	if a, b := c.Targets[0].Steps[0], c.Targets[0].Steps[1]; a.Dir != "." ||
		b.Dir != "/h/src/g" ||
		b.Args[0] != "/h/zig" {
		t.Fatalf("each step keeps its own directory: %+v %+v", a, b)
	}
	var bad Config
	if err := bad.Parse(strings.NewReader("target x ~/y zig build\n"), "f", "/h", true); err == nil {
		t.Error("a target without :: parsed")
	}
	var run Config
	if err := run.Parse(strings.NewReader("run ghostty . :: open -na build/app/T.app --args -e bin/daffy\n"), "f", "/h", true); err != nil ||
		len(run.Runs) != 1 ||
		run.Runs[0].Name != "ghostty" {
		t.Fatalf("run: %+v %v", run.Runs, err)
	}
}

// a repository declares what it needs and the dotfile gives values; a
// value in a repository and a declaration in the dotfile are refused;
// Env carries only what was declared, and Missing names what was not
// given, with its reason.
func TestNeedsAndSet(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config", "game"), 0o755)
	dot := "set CONTACT ada@example.com\nset UNRELATED secret\n"
	os.WriteFile(
		filepath.Join(home, ".config", "game", "config"),
		[]byte(dot),
		0o644,
	)
	game := "needs CONTACT who the providers can reach\n" +
		"needs REGION where it runs\n"
	os.WriteFile(filepath.Join(repo, ".game"), []byte(game), 0o644)
	c, err := Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	env := c.Env()
	if len(env) != 1 || env[0] != "CONTACT=ada@example.com" {
		t.Errorf(
			"env should carry only the declared, given setting: %v",
			env,
		)
	}
	miss := c.Missing()
	if len(miss) != 1 || miss[0].Name != "REGION" ||
		miss[0].Why != "where it runs" {
		t.Errorf("missing: %+v", miss)
	}
	os.WriteFile(
		filepath.Join(repo, ".game"),
		[]byte("set CONTACT me\n"),
		0o644,
	)
	if _, err := Load(home, repo); err == nil {
		t.Error("a value in a repository loaded")
	}
	os.WriteFile(filepath.Join(repo, ".game"), []byte(""), 0o644)
	os.WriteFile(
		filepath.Join(home, ".config", "game", "config"),
		[]byte("needs X why\n"),
		0o644,
	)
	if _, err := Load(home, repo); err == nil {
		t.Error("a declaration in the dotfile loaded")
	}
}

// remotes are the builder's: a repository that names one is refused, and
// a dotfile that names a project in .game is refused.
func TestRemotesAreTheBuilders(t *testing.T) {
	var repo Config
	if err := repo.Parse(strings.NewReader("dist x ../x.git\n"), ".game", "/h", false); err == nil {
		t.Error("a repository declared a remote")
	}
	var dot Config
	if err := dot.Parse(strings.NewReader("name x\n"), "config", "/h", true); err == nil {
		t.Error("the dotfile named a project")
	}
	var bad Config
	if err := bad.Parse(strings.NewReader("road x\n"), "config", "/h", true); err == nil {
		t.Error("a road with no url parsed")
	}
}

// a block's own value sits over the dotfile's, and a tree whose project
// the dotfile does not describe has no block at all, which is what makes
// it a local build.
func TestProjectBlocksAndLocalOnly(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config", "game"), 0o755)
	os.WriteFile(
		filepath.Join(home, ".config", "game", "config"),
		[]byte("set CONTACT ada@example.com\n"+
			"project a\n\tdist ../a.git\n"+
			"\tset CONTACT a@example.com\n"+
			"project b\n\troad ../b.git\n"),
		0o644,
	)
	os.WriteFile(filepath.Join(repo, ".game"), []byte("name a\n"), 0o644)
	c, err := Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Set["CONTACT"] != "a@example.com" {
		t.Errorf("the block's value did not win: %q", c.Set["CONTACT"])
	}
	if len(c.Remotes(false)) != 1 || len(c.Remotes(true)) != 1 {
		t.Errorf("remotes: %v %v", c.Remotes(false), c.Remotes(true))
	}

	// the same dotfile, a repository the builder does not describe
	other := t.TempDir()
	os.WriteFile(filepath.Join(other, ".game"), []byte("name c\n"), 0o644)
	c, err = Load(home, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, held := c.Mine(); held {
		t.Error("an undescribed project has a block")
	}
	if c.Set["CONTACT"] != "ada@example.com" {
		t.Errorf("the dotfile's value was lost: %q", c.Set["CONTACT"])
	}
}

// a stack holds every member to every member's rule: the smallest number
// any of them asked for, every rule any of them has, and the union of the
// words none of them will say.
func TestStrictest(t *testing.T) {
	// the words are built from pieces: this file is swept too, and a
	// listed word in it is a finding like any other.
	one, two := "hyper"+"scaler", "blazing"+"-fast"
	bender := []Rule{{Name: "width", Args: []string{"80"}},
		{Name: "words", Args: []string{one}}}
	flipr := []Rule{{Name: "width", Args: []string{"72"}},
		{Name: "shout", Args: nil},
		{Name: "words", Args: []string{two, one}}}
	game := []Rule{{Name: "width", Args: []string{"100"}},
		{Name: "paragraph", Args: []string{"4"}}}
	got := Strictest(bender, flipr, game)
	held := map[string]Rule{}
	for _, r := range got {
		held[r.Name] = r
	}
	if held["width"].Args[0] != "72" {
		t.Errorf("width is %v, want the narrowest", held["width"].Args)
	}
	if _, on := held["shout"]; !on {
		t.Error("a rule one member holds was dropped")
	}
	if held["paragraph"].Args[0] != "4" {
		t.Errorf("paragraph is %v", held["paragraph"].Args)
	}
	if len(held["words"].Args) != 2 {
		t.Errorf("words is %v, want the union", held["words"].Args)
	}
	if len(got) != 4 {
		t.Errorf("got %d rules, want 4: %v", len(got), got)
	}
}
