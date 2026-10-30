package config_test

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

// update writes the golden files from what the reading gives now. It is
// only run by hand, against a reading already trusted.
var update = flag.Bool("update", false, "write the golden files")

// dumped is every setting of a config, one to a line, in an order that
// does not change between runs. Two readings that dump the same text
// gave the same config.
func dumped(cfg config.Config) string {
	var out strings.Builder
	say := func(name string, value any) {
		fmt.Fprintf(&out, "%s: %q\n", name, value)
	}
	say("author", cfg.Author())
	say("words", cfg.Words())
	say("license", cfg.License())
	say("exclude", cfg.Exclude())
	say("omit", cfg.Omit())
	say("patches", cfg.Patches())
	say("theirs", cfg.Theirs())
	checks, floor := cfg.RapidChecks(), cfg.CoverFloor()
	say("rapid", fmt.Sprint(checks))
	say("cover floor", fmt.Sprint(floor))
	allowed, set := cfg.CoverAllowed(), cfg.Set()
	say("cover allowed", sortedPairs(allowed))
	say("set", sortedPairs(set))
	roads, dists := cfg.Remotes(false), cfg.Remotes(true)
	say("road", sortedPairs(roads))
	say("dist", sortedPairs(dists))
	for _, member := range cfg.Stack() {
		say("stack", []string{member.Name(), member.Ref()})
	}
	for _, rule := range cfg.Lint() {
		say("lint", append([]string{rule.Name()}, rule.Args()...))
	}
	for _, need := range cfg.Needs() {
		say("needs", []string{need.Name(), need.Why()})
	}
	kinds := map[string][]config.Target{"target": cfg.Targets(), "run": cfg.Runs()}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		for _, target := range kinds[kind] {
			for _, step := range target.Steps() {
				say(kind, append([]string{target.Name(), step.Dir()},
					step.Args()...))
			}
		}
	}
	for _, file := range slices.Sorted(maps.Keys(cfg.SweepAllow())) {
		say("sweep", append([]string{file}, cfg.SweepAllow()[file]...))
	}
	return out.String()
}

// sortedPairs is a map as key=value strings, in key order.
func sortedPairs(pairs map[string]string) []string {
	var out []string
	for _, key := range slices.Sorted(maps.Keys(pairs)) {
		out = append(out, key+"="+pairs[key])
	}
	return out
}

// a case is the real files read in order, each as the builder's config
// or as a repository's .game, into one config.
type reading struct {
	name  string
	files []string
}

// readings is every real config the tests hold to, alone and together.
var readings = []reading{
	{"builder", []string{"builder.config"}},
	{"game", []string{"game.game"}},
	{"nilgai", []string{"nilgai.game"}},
	{"libtheme-css", []string{"libtheme-css.game"}},
	{"dismay", []string{"dismay.game"}},
	{"every-config", []string{"every.config"}},
	{"every-game", []string{"every.game"}},
	{"every-both", []string{"every.config", "every.game"}},
	{"builder-and-game", []string{"builder.config", "game.game"}},
	{"builder-and-libtheme-css", []string{"builder.config",
		"libtheme-css.game"}},
}

// the real files, read, give the same config they gave when game parsed
// them itself: the golden files are what that parser made of them.
func TestRealConfigsReadAsTheyAlwaysHave(t *testing.T) {
	for _, each := range readings {
		t.Run(each.name, func(t *testing.T) {
			var cfg config.Config
			for _, name := range each.files {
				path := filepath.Join("testdata", "real", name)
				file, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				dotted := strings.HasSuffix(name, ".config")
				if err := cfg.Parse(file, name, "/srv/ada", dotted); err != nil {
					t.Fatalf("%s was refused: %v", name, err)
				}
			}
			golden := filepath.Join("testdata", "real", each.name+".golden")
			got := dumped(cfg)
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s read as\n%s\nwant\n%s", each.name, got, want)
			}
		})
	}
}
