package config_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// a word of the kind a value is made of: no space, no tilde, no dollar,
// nothing expand would touch and nothing split would cut.
var plainWord = rapid.StringMatching(`[a-z][a-z0-9._/-]{0,11}`)

// same is the identity, for drawing distinct words.
func same(word string) string { return word }

// drawnProject is one block as it was drawn: what Load should read back.
type drawnProject struct {
	name, road, dist string
	set              map[string]string
}

// drawProjects adds one to three project blocks to the lines, each with
// a road, maybe a dist and some set lines indented under it, and
// returns what was drawn.
func drawProjects(
	t *rapid.T,
	lines *[]string,
	mostProjects, mostSets int,
) []drawnProject {
	var want []drawnProject
	names := rapid.SliceOfNDistinct(plainWord, count.One, mostProjects, same)
	keys := rapid.SliceOfNDistinct(plainWord, count.Nothing, mostSets, same)
	for _, name := range names.Draw(t, "projects") {
		*lines = append(*lines, "", "# "+name, "project "+name)
		// a block's indent, drawn once per block; a mix is read too
		// (TestABlockMayMixTabsAndSpaces)
		indent := rapid.SampledFrom([]string{"\t", "    "}).Draw(t, "indent")
		road := plainWord.Draw(t, "road")
		*lines = append(*lines, indent+"road "+road)
		dist := ""
		if rapid.Bool().Draw(t, "has dist") {
			dist = plainWord.Draw(t, "dist")
			*lines = append(*lines, indent+"dist "+dist)
		}
		var set map[string]string
		for _, key := range keys.Draw(t, "project set names") {
			if set == nil {
				set = map[string]string{}
			}
			value := plainWord.Draw(t, "project set value")
			set[key] = value
			*lines = append(*lines, indent+"set "+key+" "+value)
		}
		want = append(want, drawnProject{name, road, dist, set})
	}
	return want
}

// readBackProperty is the property that a dotfile drawn at random reads
// back as exactly what was drawn.
type readBackProperty struct {
	whose        string
	repo         string
	mostProjects int
	mostSets     int
	mostExcludes int
	mostChecks   int
}

// check is the property as rapid runs it.
func (prop readBackProperty) check(t *rapid.T) {
	author := rapid.StringMatching(`[A-Z][a-z]{1,8}`).Draw(t, "author")
	license := plainWord.Draw(t, "license")
	words := plainWord.Draw(t, "words")
	lines := []string{"author " + author, "license = " + license,
		"words " + words}
	margin := map[string]string{}
	names := rapid.SliceOfNDistinct(plainWord, count.Nothing,
		prop.mostSets, same)
	for _, name := range names.Draw(t, "set names") {
		margin[name] = plainWord.Draw(t, "set value")
		lines = append(lines, "set "+name+" "+margin[name])
	}
	exclude := rapid.SliceOfN(plainWord, count.Nothing,
		prop.mostExcludes).Draw(t, "exclude")
	for _, path := range exclude {
		lines = append(lines, "exclude "+path)
	}
	drawn := drawProjects(t, &lines, prop.mostProjects, prop.mostSets)
	checks := rapid.IntRange(count.One, prop.mostChecks).Draw(t, "checks")
	lines = append(lines, "rapid checks "+strconv.Itoa(checks))
	text := strings.Join(lines, "\n") + "\n"
	var cfg config.Config
	err := cfg.Parse(strings.NewReader(text), "config", "/srv/ada", true)
	if err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if cfg.Author() != author || cfg.License() != license ||
		cfg.Words() != words || cfg.RapidChecks() != checks ||
		!maps.Equal(cfg.Set(), margin) ||
		!slices.Equal(cfg.Exclude(), exclude) {
		t.Fatalf("read back %+v\nfrom\n%s", cfg, text)
	}
	prop.blocksReadBack(t, text, margin, drawn)
}

// a dotfile drawn at random, margin lines then project blocks with
// their indented lines, reads back as exactly what was drawn.
func TestParseReadsBackWhatWasWritten(t *testing.T) {
	prop := readBackProperty{
		whose:        t.TempDir(),
		repo:         t.TempDir(),
		mostProjects: int(held(t, "drawn-most-projects")),
		mostSets:     int(held(t, "drawn-most-set-names")),
		mostExcludes: int(held(t, "drawn-most-excludes")),
		mostChecks:   int(held(t, "drawn-most-checks")),
	}
	rapid.Check(t, prop.check)
}

// knownKeys knows the keys config knows, so a test can draw one it does
// not.
type knownKeys map[string]bool

// unknown is whether the key is one config does not know.
func (known knownKeys) unknown(key string) bool { return !known[key] }

// unknownKeyProperty is the property that an unknown key, in a dotfile
// or in a .game, is an error that names the file and the line it is on.
type unknownKeyProperty struct {
	unknown    *rapid.Generator[string]
	mostBefore int
}

// check is the property as rapid runs it.
func (prop unknownKeyProperty) check(t *rapid.T) {
	key := prop.unknown.Draw(t, "key")
	var lines []string
	before := rapid.IntRange(count.Nothing, prop.mostBefore)
	for range before.Draw(t, "lines before") {
		lines = append(lines, "exclude "+plainWord.Draw(t, "exclude"))
	}
	lines = append(lines, key+" "+plainWord.Draw(t, "value"))
	text := strings.Join(lines, "\n") + "\n"
	want := fmt.Sprintf("f:%d: %s: this is not a key", len(lines), key)
	for _, dotfile := range []bool{true, false} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(text), "f", "/srv/ada",
			dotfile)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("want %q, got %v\n%s", want, err, text)
		}
	}
}

// an unknown key, in a dotfile or in a .game, is an error that names
// the file and the line it is on.
func TestParseNamesTheLineOfAnUnknownKey(t *testing.T) {
	known := knownKeys{"public": true}
	for _, key := range keysConfigNames(t) {
		known[key] = true
	}
	prop := unknownKeyProperty{
		unknown:    rapid.StringMatching(`[a-z]{1,10}`).Filter(known.unknown),
		mostBefore: int(held(t, "drawn-most-lines-before")),
	}
	rapid.Check(t, prop.check)
}

// blocksReadBack checks each drawn block against what Load reads from
// the same text: its road and dist, and its values laid over the margin's
// for the tree that names the block.
func (prop readBackProperty) blocksReadBack(
	t *rapid.T, text string, margin map[string]string, drawn []drawnProject,
) {
	dotfile := filepath.Join(prop.whose, ".config", "game", "config")
	if err := os.MkdirAll(filepath.Dir(dotfile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotfile, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, block := range drawn {
		game := filepath.Join(prop.repo, ".game")
		err := os.WriteFile(game, []byte("name "+block.name+"\n"), 0o644)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(prop.whose, prop.whose, prop.repo)
		if err != nil {
			t.Fatalf("%v\n%s", err, text)
		}
		want := maps.Clone(margin)
		if want == nil {
			want = map[string]string{}
		}
		maps.Copy(want, block.set)
		roads, dists := cfg.Remotes(false), cfg.Remotes(true)
		if roads[block.name] != block.road || dists[block.name] != block.dist ||
			!maps.Equal(cfg.Set(), want) {
			t.Fatalf("block %q read back as road %q, dist %q, values %v; "+
				"drawn %+v with margin %v", block.name, roads[block.name],
				dists[block.name], cfg.Set(), block, margin)
		}
	}
}

// keysConfigNames is the keys config says it knows, as it says them when
// it refuses one it does not.
func keysConfigNames(t *testing.T) []string {
	t.Helper()
	_, err := parsed("no-such-key x\n", true)
	if err == nil {
		t.Fatal("an unknown key was read")
	}
	_, list, found := strings.Cut(err.Error(), "the keys are ")
	if !found {
		t.Fatalf("the refusal lists no keys: %v", err)
	}
	return strings.Split(list, ", ")
}
