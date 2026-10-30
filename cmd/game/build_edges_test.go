package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// commands is a repository named x with two commands to build and a
// file in cmd/ that is not one, committed.
func commands(t *testing.T) repo.Repo {
	t.Helper()
	return freshTree(t, "x", map[string]string{
		"cmd/a/main.go": "package main\n",
		"cmd/b/main.go": "package main\n",
		"cmd/README":    "not a command\n",
	})
}

// each command is built into bin/ with the commit and its time stamped
// in, dirty when the tree is, with who built it and the sessions behind
// it, and each is said.
func TestBuildBinariesBuildsEachCommandStamped(t *testing.T) {
	t.Parallel()
	work := commands(t)
	commit := gitAt(t, work.Dir(), "rev-parse", "--short", "HEAD")
	when := gitAt(t, work.Dir(), "log", "-1", "--format=%cI")
	for _, dirty := range []bool{false, true} {
		if dirty {
			write(t, work.Path("new"), "untracked")
		}
		made := commit
		if dirty {
			made += "-dirty"
		}
		stamp := "-X main.build=" + made + " -X main.built=" + when +
			" -X 'main.stampedWho=jm/4b732342 on loft' -X main.stampedSids="
		stand := &asked{}
		var out bytes.Buffer
		if err := buildBinaries(work, blankConfig(),
			"jm/4b732342 on loft", stand.run, &out, true); err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, name := range []string{"a", "b"} {
			want = append(want, "x: go build -trimpath -ldflags "+stamp+
				" -o bin/"+name+" ./cmd/"+name)
			if runtime.GOOS == "darwin" {
				want = append(want, "x: codesign -s - -f bin/"+name)
			}
		}
		if !reflect.DeepEqual(stand.said, want) {
			t.Errorf("dirty %v: asked %q, want %q", dirty, stand.said,
				want)
		}
		said := "build: bin/a " + made + "\nbuild: bin/b " + made + "\n"
		if out.String() != said {
			t.Errorf("dirty %v: said %q, want %q", dirty, out.String(),
				said)
		}
	}
	if info, err := os.Stat(work.Path("bin")); err != nil ||
		!info.IsDir() {
		t.Errorf("no bin/ to build into: %v", err)
	}
}

func TestBuildBinariesBakesNeverWordsForGame(t *testing.T) {
	t.Parallel()
	work := commands(t)
	cfg := loadedAs(t, "game", nil, nil)
	stand := &asked{}
	if err := buildBinaries(work, cfg, "", stand.run,
		&bytes.Buffer{}, false); err != nil {
		t.Fatal(err)
	}
	first := "x: go run ./cmd/never"
	if len(stand.said) == count.Nothing || stand.said[count.First] != first {
		t.Fatalf("build did not bake first: %q", stand.said)
	}
	stand = &asked{fail: "go run"}
	if err := buildBinaries(work, cfg, "", stand.run,
		&bytes.Buffer{}, false); err == nil || len(stand.said) != count.One {
		t.Fatalf("bake failure: %v, asked %q", err, stand.said)
	}
}

// a claimed tree's build is a sticky build, in its colour when there is
// colour, and plain when there is not.
func TestBuildBinariesMarksAStickyBuild(t *testing.T) {
	t.Parallel()
	work := commands(t)
	write(t, work.Path(".sticky"), "ada\n")
	commit := gitAt(t, work.Dir(), "rev-parse", "--short", "HEAD") + "-dirty"
	for coloured, want := range map[bool]string{
		false: "sticky build: bin/a " + commit + "\n",
		true:  "\x1b[36msticky build: bin/a " + commit + "\x1b[0m\n",
	} {
		var out bytes.Buffer
		if err := buildBinaries(work, blankConfig(), "", (&asked{}).run,
			&out, coloured); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), want) {
			t.Errorf("coloured %v: said %q, want %q first", coloured,
				out.String(), want)
		}
	}
}

// no cmd/, a claim that cannot be read, a sync that fails or a build
// that fails is the error, and nothing after it is built.
func TestBuildBinariesStopsAtWhatFails(t *testing.T) {
	t.Parallel()
	err := buildBinaries(cleaned(t, "x", nil), blankConfig(), "",
		(&asked{}).run, &bytes.Buffer{}, false)
	if err == nil || err.Error() != "no cmd/ directory to build" {
		t.Errorf("no cmd/: %v", err)
	}
	unread := commands(t)
	write(t, unread.Path(".sticky", "x"), "a directory")
	stand := &asked{}
	if err := buildBinaries(unread, blankConfig(), "", stand.run,
		&bytes.Buffer{}, false); err == nil || stand.said != nil {
		t.Errorf("a claim that is a directory: %v, asked %q", err,
			stand.said)
	}
	unsynced := commands(t)
	write(t, unsynced.Path(".sticky"), "ada\n")
	stand = &asked{}
	nowhereRoad := filepath.Join(t.TempDir(), "nowhere.git")
	cfg := cfgFor(t, "ada", nowhereRoad, "")
	if err := buildBinaries(unsynced, cfg, "", stand.run,
		&bytes.Buffer{}, false); err == nil || stand.said != nil {
		t.Errorf("a sync that failed: %v, asked %q", err, stand.said)
	}
	stand = &asked{fail: "./cmd/a"}
	if err := buildBinaries(commands(t), blankConfig(), "",
		stand.run, &bytes.Buffer{}, false); err == nil ||
		len(stand.said) != count.One {
		t.Errorf("a build that failed: %v, asked %q", err, stand.said)
	}
	stand = &asked{fail: "codesign"}
	if err := buildBinaries(commands(t), blankConfig(), "",
		stand.run, &bytes.Buffer{}, false); err == nil ||
		len(stand.said) != count.Next(count.One) {
		t.Errorf("a signing that failed: %v, asked %q", err, stand.said)
	}
}

// a build syncs only for a block that has a road; it says what it
// merged when the road moved, and nothing when it did not.
func TestSyncBuildSaysWhatMoved(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	for name, cfg := range map[string]config.Config{
		"no block":             blankConfig(),
		"a block with no road": cfgFor(t, "ada", "", "dist.git"),
	} {
		var out bytes.Buffer
		if err := syncBuild(two, cfg, &out); err != nil ||
			count.Positive(out.Len()) {
			t.Errorf("%s: %v, said %q", name, err, out.String())
		}
	}
	cfg := cfgFor(t, "ada", road, "")
	// the road has no work branch yet, so there is nothing to sync from
	if err := syncBuild(two, cfg, &bytes.Buffer{}); err == nil {
		t.Error("a branch the road lacks was synced")
	}
	commitFile(t, one, "more.txt", "more\n", "more")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	tip := gitAt(t, one.Dir(), "rev-parse", "HEAD")
	var out bytes.Buffer
	if err := syncBuild(two, cfg, &out); err != nil {
		t.Fatal(err)
	}
	if want := "build: updates from " + repo.Short(tip) + " merged\n"; out.String() !=
		want {
		t.Errorf("said %q, want %q", out.String(), want)
	}
	out.Reset()
	if err := syncBuild(two, cfg, &out); err != nil ||
		count.Positive(out.Len()) {
		t.Errorf("nothing moved: %v, said %q", err, out.String())
	}
	noRoad := filepath.Join(t.TempDir(), "no.git")
	nowhere := cfgFor(t, "ada", noRoad, "")
	if err := syncBuild(two, nowhere, &bytes.Buffer{}); err == nil {
		t.Error("a road that is not there was synced")
	}
	unborn := cleaned(t, "unborn", nil)
	if err := syncBuild(unborn, cfg, &bytes.Buffer{}); err == nil {
		t.Error("a tree with no branch was synced")
	}
}

// game build's words choose what it builds: nothing for the binaries,
// stack, docker, or a target's name; each is reached, and refuses here
// for want of what it builds.
func TestBuildChoosesWhatToBuildByItsWord(t *testing.T) {
	t.Parallel()
	for word, want := range map[string]string{
		"":       "no cmd/ directory to build",
		"stack":  "no stack described",
		"docker": "no Dockerfile to build",
		"x":      `no target called "x"; described: `,
	} {
		var args []string
		if word != "" {
			args = []string{word}
		}
		err := buildWord(cleaned(t, "root", nil), blankConfig(), args, stackAsk{}, &bytes.Buffer{}, false, os.Executable)

		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: %v, want %q", word, err, want)
		}
	}
}

// the art is shown after a build that passed, when there is colour, and
// never after one that failed.
func TestBuildShowsTheArtAfterAPass(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"art/x.ansi": "ART\n"})
	cfg := loadedAs(t, "x", nil, []string{"target pass :: true", "target fail :: false"})
	for _, each := range []struct {
		target   string
		coloured bool
		art      bool
	}{
		{"pass", true, true},
		{"pass", false, false},
		{"fail", true, false},
	} {
		var out bytes.Buffer
		_ = buildWord(work, cfg, []string{each.target}, stackAsk{}, &out, each.coloured, os.Executable)

		if got := strings.HasSuffix(out.String(), "ART\n"); got != each.art {
			t.Errorf("%s, coloured %v: art %v, said:\n%s", each.target,
				each.coloured, got, out.String())
		}
	}
}

// a stack member's child is this same program, running verify.
func TestSelfVerifyIsThisProgramVerifying(t *testing.T) {
	t.Parallel()
	child, err := selfVerify(os.Executable)
	if err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if got := child("member").Args; !reflect.DeepEqual(got,
		[]string{self, "verify"}) {
		t.Errorf("the child is %q", got)
	}
}
