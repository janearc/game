package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// stickyFixture is a bare road, a bare dist and two clones of the
// road, standing in for two people pairing on one repository and then
// releasing what they built. Both clones start on branch "work",
// since stick refuses to claim main.
func stickyFixture(t *testing.T) (one, two repo.Repo, road, dist string) {
	t.Helper()
	dir := t.TempDir()
	road = filepath.Join(dir, "road.git")
	dist = filepath.Join(dir, "dist.git")
	git := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git(dir, "init", "-q", "--bare", road)
	git(dir, "init", "-q", "--bare", dist)
	seed := filepath.Join(dir, "seed")
	os.MkdirAll(seed, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "commit.gpgsign", "false"},
		{"config", "user.email", "seed@example.invalid"},
		{"config", "user.name", "seed"},
		{"config", "core.hooksPath", filepath.Join(dir, "no-hooks")},
	} {
		git(seed, args...)
	}
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644)
	git(seed, "add", "a.txt")
	git(seed, "commit", "-q", "-m", "seed")
	git(seed, "push", road, "main")
	clone := func(name string) repo.Repo {
		at := filepath.Join(dir, name)
		git(dir, "clone", "-q", road, at)
		for _, args := range [][]string{
			{"config", "commit.gpgsign", "false"},
			{"config", "user.email", name + "@example.invalid"},
			{"config", "user.name", name},
			// isolated from whatever hooks this machine's own git
			// config carries, so the suite is the same test on any
			// machine it runs on.
			{"config", "core.hooksPath", filepath.Join(dir, "no-hooks")},
			{"checkout", "-q", "-b", "work"},
		} {
			git(at, args...)
		}
		return repo.Repo{Dir: at}
	}
	return clone("one"), clone("two"), road, dist
}

// cfgFor is the builder's config a test needs: an author, and this
// project's two remotes, exactly what a repository's dotfile would
// hold, built directly rather than parsed from a file.
func cfgFor(author, road, dist string) config.Config {
	return config.Config{
		Name:    "x",
		Named:   "x",
		NamedBy: "the test's own config",
		Dotfile: "(none: built in the test)",
		Author:  author,
		Projects: map[string]config.Project{
			"x": {Road: road, Dist: dist},
		},
	}
}

// commitFile adds one file and commits it under the clone's own
// identity, so a build has something to sync.
func commitFile(t *testing.T, r repo.Repo, name, body, message string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(r.Dir, name), []byte(body), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git("add", name); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GitIn("", "commit", "-q", "-m", message); err != nil {
		t.Fatal(err)
	}
}

// asStamped runs fn as though this binary had been built by game
// rather than by go test: releasePush and releasePrepare both refuse
// an unstamped binary, since a release is a claim a tool without
// provenance cannot make. the test claims none; it only needs the gate
// open to reach the code the sticky check guards.
func asStamped(t *testing.T, fn func()) {
	t.Helper()
	oldBuild, oldBuilt := build, built
	build, built = "test", time.Now().UTC().Format(time.RFC3339)
	t.Cleanup(func() { build, built = oldBuild, oldBuilt })
	fn()
}

// TestStickyThenBuildFastForwards is the pairing scenario end to end:
// one clone drives, sticks and pushes; the other runs sticky, which
// picks up the claim, and its next build fast-forwards to whatever the
// driver just pushed before it builds, marking the output sticky.
func TestStickyThenBuildFastForwards(t *testing.T) {
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor("ada", road, "")

	// the binary to build is the driver's own work, committed and
	// pushed before the partner ever looks at the road, so the
	// partner's clone never carries a commit of its own to diverge
	// with: it only ever fast-forwards. a go.mod makes the fixture a
	// module go build can find.
	os.WriteFile(
		filepath.Join(one.Dir, "go.mod"),
		[]byte("module x\n\ngo 1.21\n"),
		0o644,
	)
	os.MkdirAll(filepath.Join(one.Dir, "cmd", "miami"), 0o755)
	os.WriteFile(
		filepath.Join(one.Dir, "cmd", "miami", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	)
	if _, err := one.Git("add", "go.mod", "cmd/miami/main.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.GitIn("", "commit", "-q", "-m", "a binary to build"); err != nil {
		t.Fatal(err)
	}
	// no push typed here: stick pushes the claim itself, and the
	// partner's sticky below only works if it did.
	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}

	if err := doSticky(two, cfg); err != nil {
		t.Fatalf("sticky: %v", err)
	}
	if _, ok, err := sticky.Stuck(two.Dir); err != nil || !ok {
		t.Fatalf("sticky did not pick up the claim: ok=%v err=%v", ok, err)
	}

	// the driver pushes more work after the pairing partner is
	// already sticky, and the partner makes no commit of its own; its
	// next build should pick the new work up on its own.
	commitFile(t, one, "more.txt", "more work\n", "more work")
	if _, err := one.Git("push", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}

	if err := buildBinaries(two, cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(two.Dir, "bin", "miami")); err != nil {
		t.Errorf("the binary was not built: %v", err)
	}
	if _, err := os.Stat(filepath.Join(two.Dir, "more.txt")); err != nil {
		t.Error("the build did not fast-forward to the driver's push")
	}
}

// TestReleaseAfterUnstickCarriesNoTrace is the design's own point: once
// unstuck, the flat release commit game release prepares has no
// .sticky in it anywhere, and neither does what game release push
// sends to dist.
func TestReleaseAfterUnstickCarriesNoTrace(t *testing.T) {
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor("ada", road, dist)

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}
	commitFile(t, one, "feature.txt", "a feature\n", "a feature")
	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if _, ok, _ := sticky.Stuck(one.Dir); ok {
		t.Fatal(".sticky is still in the working tree after unstick")
	}

	asStamped(t, func() {
		if err := releasePrepare(one, cfg, "v1", false); err != nil {
			t.Fatalf("release: %v", err)
		}
		if err := releasePush(one, cfg, "v1", false); err != nil {
			t.Fatalf("release push: %v", err)
		}
	})

	main, err := one.RemoteMain(dist)
	if err != nil || main == "" {
		t.Fatalf("dist has no main: %q, %v", main, err)
	}
	if out, err := one.Git("ls-tree", "-r", "--name-only", main); err != nil ||
		strings.Contains(out, ".sticky") {
		t.Errorf("the released tree carries .sticky: %q", out)
	}
	if out, err := one.Git("log", main, "--format=%B"); err != nil ||
		strings.Contains(out, "stick") {
		t.Errorf("the released history mentions sticking: %q", out)
	}
}

// TestReleaseRefusesWhileStuck is the first gate: game release will
// not flatten a tree that still carries the claim, because the flat
// tree is what reaches dist and it must carry no trace of the pairing.
func TestReleaseRefusesWhileStuck(t *testing.T) {
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor("ada", road, dist)

	if _, err := sticky.Stick(one, road, "hedy"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	asStamped(t, func() {
		err := releasePrepare(one, cfg, "v1", false)
		if err == nil || !strings.Contains(err.Error(), "unstick first") {
			t.Fatalf("want the unstick refusal, got %v", err)
		}
	})
}

// TestReleasePushRefusesWhileStuck is the last gate: a release prepared
// before the claim does not go out while the claim holds. With nothing
// on the road to merge, the claim is all that is in the way, and the
// refusal says unstick.
func TestReleasePushRefusesWhileStuck(t *testing.T) {
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor("ada", road, dist)

	asStamped(t, func() {
		if err := releasePrepare(one, cfg, "v1", false); err != nil {
			t.Fatalf("release: %v", err)
		}
	})
	// stuck by a name that is not this project's release author, so
	// the refusal is the sticky check and not the sweep's rule against
	// the author's own name in a released file.
	if _, err := sticky.Stick(one, road, "hedy"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if _, err := one.Git("push", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}
	asStamped(t, func() {
		err := releasePush(one, cfg, "v1", false)
		if err == nil || !strings.Contains(err.Error(), "unstick first") {
			t.Fatalf("want the unstick refusal, got %v", err)
		}
	})
	if main, _ := one.RemoteMain(dist); main != "" {
		t.Error("release push reached dist while stuck")
	}

	// unstick, prepare again from the clean tree, and it goes out.
	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	asStamped(t, func() {
		if err := releasePrepare(one, cfg, "v1", false); err != nil {
			t.Fatalf("release after unstick: %v", err)
		}
		if err := releasePush(one, cfg, "v1", false); err != nil {
			t.Fatalf("release push after unstick: %v", err)
		}
	})
	if main, _ := one.RemoteMain(dist); main == "" {
		t.Error("release push never reached dist after unstick")
	}
}

// TestReleasePushBehindTheSticker is the refusal's other wording: the
// other person holds the claim and has pushed work this tree has not
// taken, so the way out is to pull or build first.
func TestReleasePushBehindTheSticker(t *testing.T) {
	one, two, road, dist := stickyFixture(t)
	cfg := cfgFor("ada", road, dist)

	asStamped(t, func() {
		if err := releasePrepare(one, cfg, "v1", false); err != nil {
			t.Fatalf("release: %v", err)
		}
	})
	if _, err := sticky.Stick(two, road, "hedy"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if _, err := two.Git("push", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := doSticky(one, cfg); err != nil {
		t.Fatalf("sticky: %v", err)
	}
	commitFile(t, two, "more.txt", "more work\n", "more work")
	if _, err := two.Git("push", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}
	asStamped(t, func() {
		err := releasePush(one, cfg, "v1", false)
		if err == nil ||
			!strings.Contains(err.Error(), "pull or build first") {
			t.Fatalf("want the merge refusal, got %v", err)
		}
	})
	if main, _ := one.RemoteMain(dist); main != "" {
		t.Error("release push reached dist while behind the sticker")
	}
}

// TestStickAndUnstickPushTheClaim is that the claim is where the other
// person looks: stick leaves the road carrying it, and unstick leaves
// the road without it, with no push typed by hand.
func TestStickAndUnstickPushTheClaim(t *testing.T) {
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor("ada", road, "")

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}
	c, held, err := sticky.Holder(two, road)
	if err != nil || !held || c.Holder != "ada" {
		t.Fatalf("the road does not carry the claim: %+v held=%v err=%v",
			c, held, err)
	}

	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if _, held, err := sticky.Holder(two, road); err != nil || held {
		t.Fatalf("the road still carries the claim: held=%v err=%v",
			held, err)
	}
}

// TestStickCatchesUpToTheRoadFirst: the road moved since this clone
// last fetched, plain work and no claim. a claim committed on top of
// the old tip cannot be pushed, and "game push sends it" is refused
// the same way. stick takes the road's tip first, then claims.
func TestStickCatchesUpToTheRoadFirst(t *testing.T) {
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor("ada", road, "")
	commitFile(t, two, "theirs.txt", "the partner's work\n", "partner")
	if _, err := two.Git("push", "-q", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick behind the road: %v", err)
	}
	if _, err := os.Stat(filepath.Join(one.Dir, "theirs.txt")); err != nil {
		t.Error("stick did not take the road's tip first")
	}
	c, held, err := sticky.Holder(two, road)
	if err != nil || !held || c.Holder != "ada" {
		t.Fatalf("the claim did not reach the road: %+v held=%v err=%v",
			c, held, err)
	}
}

// TestUnstickBehindTheStickerRefusesFirst: the other person has pushed
// work this tree has not taken. an unstick committed here cannot be
// pushed, and "game push sends the release" is refused the same way.
// it is refused before it commits, in the words release push uses for
// the same state, and after a build takes the road's tip it goes.
func TestUnstickBehindTheStickerRefusesFirst(t *testing.T) {
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor("ada", road, "")
	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if err := doSticky(two, cfg); err != nil {
		t.Fatalf("sticky: %v", err)
	}
	commitFile(t, one, "more.txt", "more\n", "more from the sticker")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}

	before, _ := two.Git("rev-parse", "HEAD")
	err := doUnstick(two, cfg)
	if err == nil {
		t.Fatal("unstuck a tree behind the sticker")
	}
	if !strings.Contains(err.Error(), "pull or build first") {
		t.Errorf("the refusal is not release push's words: %v", err)
	}
	if after, _ := two.Git("rev-parse", "HEAD"); after != before {
		t.Error("a refused unstick still made a commit")
	}
	if _, ok, _ := sticky.Stuck(two.Dir); !ok {
		t.Error("a refused unstick removed the claim")
	}

	if err := syncBuild(two, cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := doUnstick(two, cfg); err != nil {
		t.Fatalf("unstick after catching up: %v", err)
	}
	if _, held, _ := sticky.Holder(one, road); held {
		t.Error("the road still carries the claim")
	}
}
