package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// parsed is src read as the dotfile, or as a repository's .game.
func parsed(src string, dotfile bool) (config.Config, error) {
	var cfg config.Config
	err := cfg.Parse(strings.NewReader(src), "the.file", "/h", dotfile)
	return cfg, err
}

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
	cfg, err := parsed(src, true)
	if err != nil {
		t.Fatal(err)
	}
	roads, dists := cfg.Remotes(false), cfg.Remotes(true)
	second := count.Next(count.First)
	if cfg.Author() != "Ada" || cfg.Words() != "/h/w.txt" ||
		roads["x"] != "../x-private.git" ||
		dists["x"] != "https://example.com/x-dist.git" ||
		len(cfg.Exclude()) != count.Next(count.One) ||
		cfg.Exclude()[second] != "internal/bridge/" {
		t.Fatalf("got %+v", cfg)
	}
	rules := cfg.Lint()
	if len(rules) != count.Next(count.One) ||
		rules[count.First].Name() != "width" ||
		rules[count.First].Args()[count.First] != "80" ||
		len(rules[second].Args()) != count.Next(count.One) {
		t.Fatalf("lint: %+v", rules)
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
		_, err := parsed(src, true)
		if err == nil {
			t.Errorf("%q parsed", src)
			continue
		}
		if !strings.HasPrefix(err.Error(), "the.file:") {
			t.Errorf("%q: error does not name the file and line: %v",
				src, err)
		}
	}
}

// the dotfile then the repository's .game; the repository wins, except
// the word list, which it may not set; missing files are fine.
func TestLoadOrder(t *testing.T) {
	home, tree := t.TempDir(), t.TempDir()
	cfg, err := config.Load(home, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	roads := cfg.Remotes(false)
	if count.Positive(len(roads)) ||
		!strings.HasSuffix(cfg.Words(), "sweep.words") {
		t.Fatalf("defaults: %+v", cfg)
	}
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	dotDir := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(dotDir, openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("author Ada\nproject a\n\tdist ../a.git\nwords " +
		"~/mine\nexclude one/\nlint width 80\nlint comments\n")
	if err := os.WriteFile(filepath.Join(dotDir, "config"), dot,
		openFile); err != nil {
		t.Fatal(err)
	}
	game := []byte("name a\nwords ~/theirs\nexclude two/\n")
	if err := os.WriteFile(filepath.Join(tree, ".game"), game,
		openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(home, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	mine, heldBy := cfg.Mine()
	if cfg.Author() != "Ada" || cfg.Named() != "a" || !heldBy ||
		mine.Dist() != filepath.Join(home, "..", "a.git") &&
			mine.Dist() != "../a.git" ||
		cfg.Words() != filepath.Join(home, "mine") ||
		len(cfg.Exclude()) != count.Next(count.One) {
		t.Fatalf("merged: %+v", cfg)
	}
	if len(cfg.Lint()) != count.Next(count.One) {
		t.Fatalf("a repository that describes no lint should "+
			"get the dotfile's two, got %v", cfg.Lint())
	}
}

// dottedHome is a home with the dotfile written, and a tree beside it
// with the .game given.
func dottedHome(t *testing.T, game string) (string, string) {
	t.Helper()
	home, tree := t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	dotDir := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(dotDir, openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("author Ada\nproject a\n\tdist ../a.git\nwords " +
		"~/mine\nexclude one/\nlint width 80\nlint comments\n")
	if err := os.WriteFile(filepath.Join(dotDir, "config"), dot,
		openFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, ".game"),
		[]byte(game), openFile); err != nil {
		t.Fatal(err)
	}
	return home, tree
}

// a repository's lint replaces the dotfile's, and an unknown key in
// the .game refuses the load.
func TestLoadOrderTheRepositoryWins(t *testing.T) {
	home, tree := dottedHome(t, "name a\nwords ~/theirs\nexclude two/\n")
	openFile := os.FileMode(0o644)
	printRule := []byte("lint print\n")
	if err := os.WriteFile(filepath.Join(tree, ".game"), printRule,
		openFile); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(home, home, tree)
	if len(cfg.Lint()) != count.One ||
		cfg.Lint()[count.First].Name() != "print" {
		t.Fatalf("a repository's lint should replace the dotfile's, "+
			"got %v", cfg.Lint())
	}
	unknown := []byte("colour blue\n")
	if err := os.WriteFile(filepath.Join(tree, ".game"), unknown,
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(home, home, tree); err == nil ||
		!strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("an unknown key in .game loaded: %v", err)
	}
}

// a target is a name, a directory and steps in order; a second line
// with the same name is the next step; a line without :: is refused.
func TestTargets(t *testing.T) {
	src := "target ghostty . :: sh tools/toolchain\ntarget ghostty " +
		"~/src/g :: ~/zig build -Doptimize=ReleaseFast\n"
	cfg, err := parsed(src, true)
	if err != nil {
		t.Fatal(err)
	}
	second := count.Next(count.First)
	if len(cfg.Targets()) != count.One ||
		len(cfg.Targets()[count.First].Steps()) != count.Next(count.One) {
		t.Fatalf("got %+v", cfg.Targets())
	}
	steps := cfg.Targets()[count.First].Steps()
	first, next := steps[count.First], steps[second]
	if first.Dir() != "." || next.Dir() != "/h/src/g" ||
		next.Args()[count.First] != "/h/zig" {
		t.Fatalf("each step keeps its own directory: %+v %+v",
			first, next)
	}
	_, err = parsed("target x ~/y zig build\n", true)
	if err == nil {
		t.Error("a target without :: parsed")
	}
	runs, err := parsed("run ghostty . :: open -na build/app/T.app "+
		"--args -e bin/daffy\n", true)
	if err != nil || len(runs.Runs()) != count.One ||
		runs.Runs()[count.First].Name() != "ghostty" {
		t.Fatalf("run: %+v %v", runs.Runs(), err)
	}
}

// a repository declares what it needs and the dotfile gives values; a
// value in a repository and a declaration in the dotfile are refused;
// Env carries only what was declared, and Missing names what was not
// given, with its reason.
func TestNeedsAndSet(t *testing.T) {
	home, tree := t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	dotDir := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(dotDir, openDir); err != nil {
		t.Fatal(err)
	}
	dot := "set CONTACT ada@example.com\nset UNRELATED secret\n"
	if err := os.WriteFile(filepath.Join(dotDir, "config"), []byte(dot),
		openFile); err != nil {
		t.Fatal(err)
	}
	game := "needs CONTACT who the providers can reach\n" +
		"needs REGION where it runs\n"
	if err := os.WriteFile(filepath.Join(tree, ".game"), []byte(game),
		openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	env := cfg.Env()
	if len(env) != count.One || env[count.First] != "CONTACT=ada@example.com" {
		t.Errorf("env should carry only the declared, given setting: %v",
			env)
	}
	miss := cfg.Missing()
	if len(miss) != count.One || miss[count.First].Name() != "REGION" ||
		miss[count.First].Why() != "where it runs" {
		t.Errorf("missing: %+v", miss)
	}
}

// a value in a repository and a declaration in the dotfile are
// refused.
func TestNeedsAndSetRefuseTheWrongSide(t *testing.T) {
	home, tree := t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	dotDir := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(dotDir, openDir); err != nil {
		t.Fatal(err)
	}
	value := []byte("set CONTACT me\n")
	if err := os.WriteFile(filepath.Join(tree, ".game"), value,
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(home, home, tree); err == nil {
		t.Error("a value in a repository loaded")
	}
	empty := []byte("")
	if err := os.WriteFile(filepath.Join(tree, ".game"), empty,
		openFile); err != nil {
		t.Fatal(err)
	}
	declared := []byte("needs X why\n")
	if err := os.WriteFile(filepath.Join(dotDir, "config"), declared,
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(home, home, tree); err == nil {
		t.Error("a declaration in the dotfile loaded")
	}
}

// remotes are the builder's: a repository that names one is refused, and
// a dotfile that names a project in .game is refused.
func TestRemotesAreTheBuilders(t *testing.T) {
	if _, err := parsed("dist x ../x.git\n", false); err == nil {
		t.Error("a repository declared a remote")
	}
	if _, err := parsed("name x\n", true); err == nil {
		t.Error("the dotfile named a project")
	}
	if _, err := parsed("road x\n", true); err == nil {
		t.Error("a road with no url parsed")
	}
}

// a block's own value sits over the dotfile's, and a tree whose project
// the dotfile does not describe has no block at all, which is what makes
// it a local build.
func TestProjectBlocksAndLocalOnly(t *testing.T) {
	home, tree := t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	dotDir := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(dotDir, openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("set CONTACT ada@example.com\n" +
		"project a\n\tdist ../a.git\n" +
		"\tset CONTACT a@example.com\n" +
		"project b\n\troad ../b.git\n")
	if err := os.WriteFile(filepath.Join(dotDir, "config"), dot,
		openFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, ".game"),
		[]byte("name a\n"), openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Set()["CONTACT"] != "a@example.com" {
		t.Errorf("the block's value did not win: %q", cfg.Set()["CONTACT"])
	}
	if len(cfg.Remotes(false)) != count.One ||
		len(cfg.Remotes(true)) != count.One {
		t.Errorf("remotes: %v %v", cfg.Remotes(false),
			cfg.Remotes(true))
	}

	// the same dotfile, a repository the builder does not describe
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, ".game"),
		[]byte("name c\n"), openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(home, home, other)
	if err != nil {
		t.Fatal(err)
	}
	if _, heldBy := cfg.Mine(); heldBy {
		t.Error("an undescribed project has a block")
	}
	if cfg.Set()["CONTACT"] != "ada@example.com" {
		t.Errorf("the dotfile's value was lost: %q", cfg.Set()["CONTACT"])
	}
}

// a stack holds every member to every member's rule: the smallest number
// any of them asked for, every rule any of them has, and the union of the
// words none of them will say.
func TestStrictest(t *testing.T) {
	// the words are built from pieces: this file is swept too, and a
	// listed word in it is a finding like any other.
	one, two := "hyper"+"scaler", "blazing"+"-fast"
	bender := []config.Rule{config.Ruled("width", "80"),
		config.Ruled("words", one)}
	flipr := []config.Rule{config.Ruled("width", "72"),
		config.Ruled("shout"), config.Ruled("words", two, one)}
	game := []config.Rule{config.Ruled("width", "100"),
		config.Ruled("paragraph", "4")}
	got := config.Strictest(bender, flipr, game)
	byName := map[string]config.Rule{}
	for _, rule := range got {
		byName[rule.Name()] = rule
	}
	if byName["width"].Args()[count.First] != "72" {
		t.Errorf("width is %v, want the narrowest",
			byName["width"].Args())
	}
	if _, on := byName["shout"]; !on {
		t.Error("a rule one member holds was dropped")
	}
	if byName["paragraph"].Args()[count.First] != "4" {
		t.Errorf("paragraph is %v", byName["paragraph"].Args())
	}
	if len(byName["words"].Args()) != count.Next(count.One) {
		t.Errorf("words is %v, want the union", byName["words"].Args())
	}
	names := []string{"width", "shout", "words", "paragraph"}
	if len(got) != len(names) {
		t.Errorf("got %d rules, want %d: %v", len(got), len(names), got)
	}
}

// TestLoadReadsOneHomeAndExpandsAnother: an agent's config lives in its
// own directory and names shared roads with a ~, which still means the
// machine's user. without this an agent reading a shared road is sent to
// the same path under its own directory, where nothing is.
func TestLoadReadsOneHomeAndExpandsAnother(t *testing.T) {
	whose, home, tree := t.TempDir(), t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	at := filepath.Join(whose, ".config", "game")
	if err := os.MkdirAll(at, openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project thing\n\troad ~/roads/thing.git\n")
	if err := os.WriteFile(filepath.Join(at, "config"), dot,
		openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(whose, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "roads", "thing.git")
	if got := cfg.Remotes(false)["thing"]; got != want {
		t.Errorf("road %q, want %q", got, want)
	}
}

// the name a tree is looked up by in the builder's config is the one
// its messages use, found in this order: .game's name line, go.mod's
// module, the directory.
func TestNameComesFromOnePlace(t *testing.T) {
	home := t.TempDir()
	dotfile := filepath.Join(home, ".config", "game", "config")
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	if err := os.MkdirAll(filepath.Dir(dotfile), openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project miami\n\troad ../miami-road.git\n" +
		"project kingfisher\n\troad ../k-road.git\n")
	if err := os.WriteFile(dotfile, dot, openFile); err != nil {
		t.Fatal(err)
	}

	// no .game at all: the module names it, and the block is found.
	cfg, err := config.Load(home, home,
		tree(t, "miami", "github.com/x/miami", "", openDir, openFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, heldBy := cfg.Mine(); !heldBy || cfg.Named() != "miami" ||
		!strings.Contains(cfg.NoBlock().Error(), "go.mod") {
		t.Fatalf("no .game: named %q, held %v: %v",
			cfg.Named(), heldBy, cfg.NoBlock())
	}

	// a folder called something else: the module still wins.
	cfg, _ = config.Load(home, home,
		tree(t, "miami-dist", "github.com/x/miami", "", openDir, openFile))
	if _, heldBy := cfg.Mine(); !heldBy || cfg.Named() != "miami" {
		t.Fatalf("folder differs: named %q, held %v", cfg.Named(), heldBy)
	}

	// a module with a major version suffix is still its own name.
	cfg, _ = config.Load(home, home,
		tree(t, "k", "github.com/x/kingfisher/v2", "", openDir, openFile))
	if _, heldBy := cfg.Mine(); !heldBy || cfg.Named() != "kingfisher" {
		t.Fatalf("v2 suffix: named %q, held %v", cfg.Named(), heldBy)
	}

}

// a .game with a name line still wins over the module, since support
// stays for now; and nothing to go on but the folder is named by the
// folder, and said to be.
func TestNameFromDotGameAndFolder(t *testing.T) {
	home := t.TempDir()
	dotfile := filepath.Join(home, ".config", "game", "config")
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	if err := os.MkdirAll(filepath.Dir(dotfile), openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project miami\n\troad ../miami-road.git\n")
	if err := os.WriteFile(dotfile, dot, openFile); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(home, home,
		tree(t, "x", "github.com/x/other", "name miami\n", openDir,
			openFile))
	if _, heldBy := cfg.Mine(); !heldBy || cfg.Named() != "miami" ||
		!strings.Contains(cfg.NoBlock().Error(), ".game") {
		t.Fatalf(".game name: named %q, held %v: %v",
			cfg.Named(), heldBy, cfg.NoBlock())
	}

	// nothing to go on but the folder: it is named, and said to be.
	cfg, _ = config.Load(home, home,
		tree(t, "plain", "", "", openDir, openFile))
	if cfg.Named() != "plain" ||
		!strings.Contains(cfg.NoBlock().Error(), "directory") {
		t.Fatalf("bare folder: named %q: %v", cfg.Named(),
			cfg.NoBlock())
	}
}

// a config with no block for a tree refuses it by name, saying where
// the name came from, which file was read, and what the file does
// have; one with no blocks at all says so.
func TestNoBlockSaysWhatWasTried(t *testing.T) {
	home := t.TempDir()
	dotfile := filepath.Join(home, ".config", "game", "config")
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	if err := os.MkdirAll(filepath.Dir(dotfile), openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project miami\n\troad ../miami-road.git\n" +
		"project kingfisher\n\troad ../k-road.git\n")
	if err := os.WriteFile(dotfile, dot, openFile); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(home, home,
		tree(t, "wren", "github.com/x/wren", "", openDir, openFile))
	if _, heldBy := cfg.Mine(); heldBy {
		t.Fatal("found a block for wren in a config with none")
	}
	err := cfg.NoBlock()
	if err == nil {
		t.Fatal("no error for a project with no block")
	}
	for _, want := range []string{
		"wren", "go.mod", "github.com/x/wren", dotfile,
		"kingfisher", "miami",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}

	// the same, against a config with no blocks at all.
	empty := t.TempDir()
	cfg, _ = config.Load(empty, empty,
		tree(t, "wren", "github.com/x/wren", "", openDir, openFile))
	if err := cfg.NoBlock(); err == nil ||
		!strings.Contains(err.Error(), "no project blocks") {
		t.Fatalf("an empty config should say it has no blocks: %v", err)
	}
}

// tree is a directory named dir, with a go.mod of the module and a
// .game of the text where given.
func tree(
	t *testing.T,
	dir, mod, game string,
	openDir, openFile os.FileMode,
) string {
	t.Helper()
	at := filepath.Join(t.TempDir(), dir)
	if err := os.MkdirAll(at, openDir); err != nil {
		t.Fatal(err)
	}
	if mod != "" {
		module := filepath.Join(at, "go.mod")
		if err := os.WriteFile(module,
			[]byte("module "+mod+"\n\ngo 1.25\n"), openFile); err != nil {
			t.Fatal(err)
		}
	}
	if game != "" {
		dot := filepath.Join(at, ".game")
		if err := os.WriteFile(dot, []byte(game), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return at
}

// a stack member's name is a directory game removes and clones again
// under bin/stack and bin/patch, so it may not climb out or be hidden.
func TestAStackMemberIsOnePlainDirectory(t *testing.T) {
	for _, name := range []string{"..", ".hidden", "a/b", `a\b`, "a*",
		"b?", "c[1]"} {
		_, err := parsed("stack "+name+" main\n", false)
		if err == nil || !strings.Contains(err.Error(), "plain directory") {
			t.Errorf("%q: %v, want it refused", name, err)
		}
	}
	cfg, err := parsed("stack crush v0.98.1\n", false)
	stack := cfg.Stack()
	if err != nil || len(stack) != count.One ||
		stack[count.First].Name() != "crush" {
		t.Errorf("a plain name: %v, %v", stack, err)
	}
}

// a stack member marked theirs is someone else's code: its line keeps its
// name and ref, and the member is known as theirs. one not marked is not.
func TestAStackMemberMayBeTheirs(t *testing.T) {
	cfg, err := parsed("stack crush v0.98.1 theirs\nstack keel main\n",
		false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, member := range cfg.Stack() {
		got = append(got, member.Name()+" "+member.Ref())
	}
	if want := []string{"crush v0.98.1", "keel main"}; !slices.Equal(got, want) {
		t.Errorf("stack: %v, want %v", got, want)
	}
	if !cfg.IsTheirs("crush") || cfg.IsTheirs("keel") ||
		cfg.IsTheirs("nobody") {
		t.Errorf("theirs: %v", cfg.Theirs())
	}
}

// a stack line takes a name, a ref, and theirs or nothing after it.
func TestAStackLineIsRefusedPastItsWords(t *testing.T) {
	for _, src := range []string{
		"stack crush\n", "stack crush v0.98.1 mine\n",
		"stack crush v0.98.1 theirs too\n",
	} {
		_, err := parsed(src, false)
		if err == nil || !strings.Contains(err.Error(), "stack wants") {
			t.Errorf("%q: %v, want it refused", src, err)
		}
	}
}
