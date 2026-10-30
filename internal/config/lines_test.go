package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// TestEachLineIsRefusedInItsOwnWords: every line that cannot stand is
// refused with the reason that belongs to it, not with whatever a later
// check happens to say.
func TestEachLineIsRefusedInItsOwnWords(t *testing.T) {
	for _, line := range []struct {
		name, src string
		dotfile   bool
		want      string
	}{
		{"a project in a repository", "project x\n", false,
			"a project block is the builder's"},
		{"a project with no name", "project \"\"\n", true,
			"project wants \"project NAME\""},
		{"a remote in a repository", "road ../x.git\n", false,
			"road is the builder's"},
		{"a remote with no url", "project x\n\troad \"\"\n", true,
			"road wants a url"},
		{"a stack in the dotfile", "stack a main\n", true,
			"a stack is a repository's"},
		{"a stack with no ref", "stack a\n", false,
			"stack: this key takes 2 to 3 words, and this line has 1"},
		{"a stack with a third word that is not theirs",
			"stack a main mine\n", false,
			"stack wants \"stack NAME REF\""},
		{"a target with no command", "target build . ::\n", false,
			"target: this key takes a tail after \"::\""},
		{"a target with no command or directory", "target build\n", false,
			"target: this key takes a tail after \"::\""},
		{"a target with a command and no directory",
			"target build :: go build\ntarget build x y :: go build\n",
			false, "target: this key takes 1 to 2 words"},
		{"sweep that is not allow", "sweep a.md b.md emails\n", false,
			"sweep wants allow FILE KIND"},
		{"sweep of an unknown kind", "sweep allow a.md nonsense\n",
			false, "unknown sweep kind \"nonsense\""},
		{"lint with no rule", "lint \"\"\n", false, "lint wants a rule"},
		{"lint allow with no lint", "lint allow a.md\n", false,
			"lint allow wants a file and a lint"},
		{"lint allow of an unknown lint",
			"lint allow a.md width nonsense\n", false,
			"unknown lint \"nonsense\""},
		{"lint allow of an unknown lint first",
			"lint allow a.md nonsense width\n", false,
			"unknown lint \"nonsense\""},
		{"a key with no value", "author\n", true,
			"author: this key takes at least 1 word, and this line has 0"},
	} {
		_, err := parsed(line.src, line.dotfile)
		if err == nil || !strings.Contains(err.Error(), line.want) {
			t.Errorf("%s: %v, want %q", line.name, err, line.want)
		}
	}
}

// TestPzmRefusesALineBeforeGameReadsIt: the words on a line are counted
// by pzm, once, so a line with too few, or a key said twice, is refused
// in pzm's words, and a pair of quotes may not change the count.
func TestPzmRefusesALineBeforeGameReadsIt(t *testing.T) {
	for src, want := range map[string]string{
		"needs TOKEN\n":        "needs: this key takes at least 2 words",
		"set TOKEN\n":          "set: this key takes at least 2 words",
		"sweep allow a.md\n":   "sweep: this key takes at least 3 words",
		"author \" a \"\n":     "change how many words it has",
		"author a\nauthor b\n": "author: this key is on one line at most",
		"author\nname a b\n":   "the.file:2: name: this key takes 1 word",
	} {
		_, err := parsed(src, true)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", src, err, want)
		}
	}
}

// TestTheLinesThatStandAreRead: the lines just short of a refusal are
// read, and read whole.
func TestTheLinesThatStandAreRead(t *testing.T) {
	game, err := parsed("stack a main\n"+
		"sweep allow a.md emails\n"+
		"sweep allow a.md author word\n"+
		"lint allow a.md width\n"+
		"lint allow b.md rows paragraph\n"+
		"target build . :: go build\n", false)
	if err != nil {
		t.Fatal(err)
	}
	members := game.Stack()
	if len(members) != count.One || members[count.First].Name() != "a" ||
		members[count.First].Ref() != "main" {
		t.Errorf("stack: %v", members)
	}
	allowed := game.SweepAllow()
	got := allowed["a.md"]
	if !slices.Equal(got, []string{"emails", "author", "word"}) ||
		len(allowed) != count.One {
		t.Errorf("sweep: %v", allowed)
	}
	second := count.Next(count.First)
	rules := game.Lint()
	if len(rules) != count.Next(count.One) ||
		rules[count.First].Name() != "allow" ||
		!slices.Equal(rules[count.First].Args(),
			[]string{"a.md", "width"}) ||
		!slices.Equal(rules[second].Args(),
			[]string{"b.md", "rows", "paragraph"}) {
		t.Errorf("lint: %+v", rules)
	}
	targets := game.Targets()
	steps := targets[count.First].Steps()
	if len(targets) != count.One ||
		!slices.Equal(steps[count.First].Args(), []string{"go", "build"}) {
		t.Errorf("target: %+v", targets)
	}
}

// TestABlockOpenedAgainKeepsWhatItHad: a project block named a second
// time adds to the first, and an empty block is still a block. a road
// named for the block alone is a url, not the block's name said twice.
func TestABlockOpenedAgainKeepsWhatItHad(t *testing.T) {
	cfg, err := parsed("project x\n\troad x\nproject y\nproject x\n"+
		"\tdist d.git\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if road, dist := cfg.Remotes(false)["x"], cfg.Remotes(true)["x"]; road != "x" ||
		dist != "d.git" {
		t.Errorf("x: road %q, dist %q", road, dist)
	}
	if err := cfg.NoBlock(); !strings.Contains(err.Error(),
		"its project blocks are x, y") {
		t.Errorf("the empty block y is not there: %v", err)
	}
}

// TestQuotesComeOffOnlyInPairs: a value in matching quotes loses them;
// one quote, or two that do not match, is part of the value.
func TestQuotesComeOffOnlyInPairs(t *testing.T) {
	for src, want := range map[string]string{
		"author \"Ada\"\n": "Ada", "author 'Ada'\n": "Ada",
		"author \"Ada\n": "\"Ada", "author Ada\"\n": "Ada\"",
		"author 'Ada\n": "'Ada", "author Ada'\n": "Ada'",
		"author \"Ada'\n": "\"Ada'",
	} {
		cfg, err := parsed(src, true)
		if err != nil || cfg.Author() != want {
			t.Errorf("%q: %q, %v, want %q", src, cfg.Author(), err, want)
		}
	}
}

// TestATildeAloneIsHome: ~ is the home, ~/ under it, and a ~ anywhere
// else is a ~. the words line is a path, so it is read that way.
func TestATildeAloneIsHome(t *testing.T) {
	for val, want := range map[string]string{
		"~": "/h", "~/w": "/h/w", "a~": "a~", "~w": "~w",
	} {
		got, err := parsed("words "+val+"\n", true)
		if err != nil || got.Words() != want {
			t.Errorf("%q: %q, %v, want %q", val, got.Words(), err, want)
		}
	}
}

// TestTheSmallerNumberIsTheStrictest: of two rules with a number, the
// smaller wins, whichever comes first, and a rule with no number, or one
// that is not a number, never beats another.
func TestTheSmallerNumberIsTheStrictest(t *testing.T) {
	for _, pair := range []struct {
		one, other []string
		want       []string
	}{
		{[]string{"1"}, []string{"2"}, []string{"1"}},
		{[]string{"2"}, []string{"1"}, []string{"1"}},
		{[]string{"2"}, []string{"2"}, []string{"2"}},
		// equal in value and not in spelling: the first stays
		{[]string{"02"}, []string{"2"}, []string{"02"}},
		{[]string{"2"}, []string{"02"}, []string{"2"}},
		{nil, []string{"1"}, nil},
		{[]string{"1"}, nil, []string{"1"}},
		{[]string{"x"}, []string{"1"}, []string{"x"}},
		{[]string{"1"}, []string{"x"}, []string{"1"}},
		{[]string{"-1"}, []string{"x"}, []string{"-1"}},
		{[]string{"x"}, []string{"-1"}, []string{"x"}},
	} {
		got := config.Strictest(
			[]config.Rule{config.Ruled("width", pair.one...)},
			[]config.Rule{config.Ruled("width", pair.other...)},
		)
		if len(got) != count.One ||
			!slices.Equal(got[count.First].Args(), pair.want) {
			t.Errorf("width %v then %v: %+v, want %v", pair.one, pair.other,
				got, pair.want)
		}
	}
}

// TestAllowancesAreHeldPerFile: across members, an allowance for one
// file is merged with another's for that file, and never with one for a
// different file.
func TestAllowancesAreHeldPerFile(t *testing.T) {
	got := config.Strictest(
		[]config.Rule{config.Ruled("allow", "a.md", "width")},
		[]config.Rule{config.Ruled("allow", "b.md", "rows")},
		[]config.Rule{config.Ruled("allow", "a.md", "paragraph",
			"width")},
	)
	want := []config.Rule{
		config.Ruled("allow", "a.md", "width", "paragraph"),
		config.Ruled("allow", "b.md", "rows"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for place := range want {
		if got[place].Name() != want[place].Name() ||
			!slices.Equal(got[place].Args(), want[place].Args()) {
			t.Errorf("%d: %+v, want %+v", place, got[place], want[place])
		}
	}
}

// TestTheNameFromAModulePath: the last element, or the one before a
// version; a v that is not a version is a name.
func TestTheNameFromAModulePath(t *testing.T) {
	for mod, want := range map[string]string{
		"example.com/x/v2": "x", "example.com/x": "x", "v2": "v2",
		"x/v2": "x", "example.com/x/vendor": "vendor",
		"example.com/x/v": "v", "/x/": "x",
	} {
		home, tree := t.TempDir(), t.TempDir()
		writeModule(t, tree, mod)
		cfg, err := config.Load(home, home, tree)
		if got := cfg.Named(); err != nil || got != want {
			t.Errorf("%q: %q, %v, want %q", mod, got, err, want)
		}
	}
}

// TestNoBlockNamesTheOneBlockThereIs: with one block, the refusal says
// which.
func TestNoBlockNamesTheOneBlockThereIs(t *testing.T) {
	cfg, err := parsed("project a\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.NoBlock(); !strings.Contains(err.Error(),
		"its project blocks are a") {
		t.Errorf("%v", err)
	}
}

// TestRemotesLeaveOutWhatIsNotThere: a project with no road is not among
// the roads, and one with no dist is not among the dists.
func TestRemotesLeaveOutWhatIsNotThere(t *testing.T) {
	cfg, err := parsed("project a\n\troad a-road\nproject b\n\tdist b-dist\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if roads := cfg.Remotes(false); len(roads) != count.One ||
		roads["a"] != "a-road" {
		t.Errorf("roads: %v", roads)
	}
	if dists := cfg.Remotes(true); len(dists) != count.One ||
		dists["b"] != "b-dist" {
		t.Errorf("dists: %v", dists)
	}
}

// TestABlocksValuesReachTheConfig: a value set in the tree's own block
// is the config's, when nothing is set at the margin.
func TestABlocksValuesReachTheConfig(t *testing.T) {
	whose, tree := t.TempDir(), t.TempDir()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	at := filepath.Join(whose, ".config", "game")
	if err := os.MkdirAll(at, openDir); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project x\n\tset TOKEN abc\n")
	if err := os.WriteFile(filepath.Join(at, "config"), dot,
		openFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, ".game"),
		[]byte("name x\n"), openFile); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(whose, whose, tree)
	if err != nil || cfg.Set()["TOKEN"] != "abc" {
		t.Errorf("set: %v, %v", cfg.Set(), err)
	}
}

// TestStrictestTakesAllowancesAsTheyCome: an allowance built with no
// file is held as one rule, and allowances naming only a file are held
// per file, not merged into one.
func TestStrictestTakesAllowancesAsTheyCome(t *testing.T) {
	if got := config.Strictest([]config.Rule{config.Ruled("allow")}); len(got) != count.One {
		t.Errorf("an allowance with no file: %+v", got)
	}
	got := config.Strictest([]config.Rule{config.Ruled("allow", "a.md")},
		[]config.Rule{config.Ruled("allow", "b.md")})
	if len(got) != count.Next(count.One) {
		t.Errorf("two files, one allowance each: %+v", got)
	}
}

// TestAnEqualsSignWithNoSpacesIsPartOfTheKey: pzm reads "key=value" as
// one word, which is no key. game used to take the key before the equals
// sign. a lone equals sign after the key is still read as nothing.
func TestAnEqualsSignWithNoSpacesIsPartOfTheKey(t *testing.T) {
	_, err := parsed("author=Ada Byron\n", false)
	if err == nil || !strings.Contains(err.Error(),
		"author=Ada: this is not a key") {
		t.Errorf("author=Ada Byron: %v, want it refused", err)
	}
	got, err := parsed("author = Ada Byron\n", false)
	if err != nil || got.Author() != "Ada Byron" {
		t.Errorf("author = Ada Byron: %q, %v", got.Author(), err)
	}
}

// TestASpaceBeforeAnEqualsSignEndsTheKey: with the space first, the key
// is the first word and the equals sign belongs to the value.
func TestASpaceBeforeAnEqualsSignEndsTheKey(t *testing.T) {
	got, err := parsed("author Ada=Byron\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Author() != "Ada=Byron" {
		t.Errorf("author read as %q, want Ada=Byron", got.Author())
	}
}

// TestAKeyIsLowercase: pzm does not lowercase keys, as game used to.
func TestAKeyIsLowercase(t *testing.T) {
	_, err := parsed("Author Ada\n", false)
	if err == nil || !strings.Contains(err.Error(),
		"the.file:1: Author: this is not a key") {
		t.Errorf("Author Ada: %v, want it refused", err)
	}
}

// TestABlockMayMixTabsAndSpaces: any leading whitespace indents a line
// into its block, tabs, spaces or both, as game has always read them.
func TestABlockMayMixTabsAndSpaces(t *testing.T) {
	cfg, err := parsed("project a\n  road a.git\n   dist d.git\n", true)
	if err != nil || cfg.Remotes(true)["a"] != "d.git" {
		t.Errorf("spaces of different widths: %v, %v", cfg.Remotes(true), err)
	}
	cfg, err = parsed("project a\n\troad a.git\n  dist d.git\n", true)
	if err != nil || cfg.Remotes(true)["a"] != "d.git" {
		t.Errorf("tabs then spaces: %v, %v", cfg.Remotes(true), err)
	}
	cfg, err = parsed("project a\n  road a.git\n\tdist d.git\n", true)
	if err != nil || cfg.Remotes(true)["a"] != "d.git" {
		t.Errorf("spaces then tabs: %v, %v", cfg.Remotes(true), err)
	}
}

// TestAFileThatCannotBeReadIsRefused: a reader that fails is the
// failure, and nothing is read from it.
func TestAFileThatCannotBeReadIsRefused(t *testing.T) {
	var cfg config.Config
	failed := errors.New("the disk went")
	err := cfg.Parse(iotest.ErrReader(failed), "f", "/h", true)
	if !errors.Is(err, failed) {
		t.Errorf("a failing reader: %v", err)
	}
}

// TestEveryLineThatCannotStandIsNamed: a file with several bad lines is
// refused with all of them, in the order of their lines. pzm's lines
// come first, and game's only once pzm has read the whole file.
func TestEveryLineThatCannotStandIsNamed(t *testing.T) {
	_, err := parsed("lint dance\ncover ten\nlint allow a.md\n", false)
	if err == nil {
		t.Fatal("bad lines were read")
	}
	last := -1
	for _, want := range []string{"the.file:1: lint:", "the.file:2: cover:",
		"the.file:3: lint:"} {
		at := strings.Index(err.Error(), want)
		if at <= last {
			t.Errorf("%q is not after the line before it in\n%v", want, err)
		}
		last = at
	}
}

// writeModule gives a tree a go.mod of the given module path.
func writeModule(t *testing.T, tree, mod string) {
	t.Helper()
	body := []byte("module " + mod + "\n")
	if err := os.WriteFile(filepath.Join(tree, "go.mod"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNeedsAreWhatTheLinesSay: each needs line is a name and why, in
// order; a config with none needs nothing.
func TestNeedsAreWhatTheLinesSay(t *testing.T) {
	cfg, err := parsed("needs TOKEN to call out\nneeds URL where it lives\n", false)
	if err != nil {
		t.Fatal(err)
	}
	needs := cfg.Needs()
	if len(needs) != count.Next(count.One) ||
		needs[count.First].Name() != "TOKEN" ||
		needs[count.First].Why() != "to call out" ||
		needs[count.Next(count.First)].Name() != "URL" {
		t.Errorf("needs: %+v", needs)
	}
	none, err := parsed("author Ada\n", false)
	if err != nil || len(none.Needs()) != count.Nothing {
		t.Errorf("no needs lines: %+v, %v", none.Needs(), err)
	}
}

// TestTheirsAreTheMembersMarkedSo: the members marked theirs, in order,
// and none when none is.
func TestTheirsAreTheMembersMarkedSo(t *testing.T) {
	cfg, err := parsed("stack a main theirs\nstack b main\nstack c v1 theirs\n",
		false)
	if err != nil || !slices.Equal(cfg.Theirs(), []string{"a", "c"}) {
		t.Errorf("theirs: %v, %v", cfg.Theirs(), err)
	}
	none, err := parsed("stack a main\n", false)
	if err != nil || len(none.Theirs()) != count.Nothing {
		t.Errorf("none marked: %v, %v", none.Theirs(), err)
	}
}

// TestAProjectKeepsItsRoadAndDist: the block the tree is named for has
// its road and its dist, and a tree with no block has no project.
func TestAProjectKeepsItsRoadAndDist(t *testing.T) {
	home, tree := t.TempDir(), t.TempDir()
	writeModule(t, tree, "example.com/thing")
	at := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}
	dot := []byte("project thing\n\troad /r.git\n\tdist /d.git\n")
	if err := os.WriteFile(filepath.Join(at, "config"), dot, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, home, tree)
	if err != nil {
		t.Fatal(err)
	}
	mine, held := cfg.Mine()
	if !held || mine.Road() != "/r.git" || mine.Dist() != "/d.git" {
		t.Errorf("the block: %+v, held %v", mine, held)
	}
	other := t.TempDir()
	writeModule(t, other, "example.com/other")
	cfg, err = config.Load(home, home, other)
	if _, held := cfg.Mine(); err != nil || held {
		t.Errorf("a tree with no block: held %v, %v", held, err)
	}
}
