package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	gitAt(t, dir, "init", "-q", "--bare", road)
	gitAt(t, dir, "init", "-q", "--bare", dist)
	seed := filepath.Join(dir, "seed")
	seeded(t, dir, seed, road)
	return cloneOf(t, dir, road, "one"), cloneOf(t, dir, road, "two"),
		road, dist
}

// seeded is the road's first commit: one file on main, from nobody's
// own git config.
func seeded(t *testing.T, dir, seed, road string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(seed, openDir); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "commit.gpgsign", "false"},
		{"config", "user.email", "seed@example.invalid"},
		{"config", "user.name", "seed"},
		{"config", "core.hooksPath", filepath.Join(dir, "no-hooks")},
	} {
		gitAt(t, seed, args...)
	}
	openFile := os.FileMode(0o644)
	first := filepath.Join(seed, "a.txt")
	if err := os.WriteFile(first, []byte("a\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitAt(t, seed, "add", "a.txt")
	gitAt(t, seed, "commit", "-q", "-m", "seed")
	gitAt(t, seed, "push", road, "main")
}

// cloneOf is a clone of the road by the name, on branch work, with its
// own git identity and no hooks of this machine's.
func cloneOf(t *testing.T, dir, road, name string) repo.Repo {
	t.Helper()
	at := filepath.Join(dir, name)
	gitAt(t, dir, "clone", "-q", road, at)
	for _, args := range [][]string{
		{"config", "commit.gpgsign", "false"},
		{"config", "user.email", name + "@example.invalid"},
		{"config", "user.name", name},
		{"config", "core.hooksPath", filepath.Join(dir, "no-hooks")},
		{"checkout", "-q", "-b", "work"},
	} {
		gitAt(t, at, args...)
	}
	return repoAt(at)
}

// cfgFor is the builder's config a test needs: an author, and this
// project's two remotes, exactly what a repository's dotfile would
// hold, built directly rather than parsed from a file.
func cfgFor(t *testing.T, author, road, dist string) config.Config {
	t.Helper()
	return cfgWith(t, author, road, dist)
}

// cfgWith is cfgFor with lines of the .game besides.
func cfgWith(
	t *testing.T, author, road, dist string, game ...string,
) config.Config {
	t.Helper()
	dotfile := block("x", road, dist)
	if author != "" {
		dotfile = append([]string{"author " + author}, dotfile...)
	}
	return loadedAs(t, "x", dotfile, game)
}

// commitFile adds one file and commits it under the clone's own
// identity, so a build has something to sync.
func commitFile(t *testing.T, work repo.Repo, name, body,
	message string) {
	t.Helper()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(work.Path(name), []byte(body),
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("add", name); err != nil {
		t.Fatal(err)
	}
	if _, err := work.GitIn("", "commit", "-q", "-m", message); err != nil {
		t.Fatal(err)
	}
}

// made is a stamp as game build gives one, so a release will start:
// releasePrepare, releasePush and releaseStack refuse a game that cannot
// say which commit it came from. the test claims none; it only needs the
// gate open to reach the code behind it.
var made = stamp{commit: "test", at: "2026-10-15T00:00:00Z"}

// unstamped is a game built by go rather than by game.
var unstamped = stamp{commit: "dev"}

// withModule gives the clone a go.mod and a miami command, committed.
func withModule(t *testing.T, one repo.Repo) {
	t.Helper()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(one.Path("go.mod"),
		[]byte("module x\n\ngo 1.21\n"), openFile); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(one.Path("cmd", "miami"), openDir); err != nil {
		t.Fatal(err)
	}
	main := one.Path("cmd", "miami", "main.go")
	if err := os.WriteFile(main,
		[]byte("package main\n\nfunc main() {}\n"), openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("add", "go.mod", "cmd/miami/main.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.GitIn("", "commit", "-q", "-m",
		"a binary to build"); err != nil {
		t.Fatal(err)
	}
}

// TestStickyThenBuildFastForwards is the pairing scenario end to end:
// one clone drives, sticks and pushes; the other runs sticky, which
// picks up the claim, and its next build fast-forwards to whatever the
// driver just pushed before it builds, marking the output sticky.
func TestStickyThenBuildFastForwards(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")

	// the binary to build is the driver's own work, committed and
	// pushed before the partner ever looks at the road, so the
	// partner's clone never carries a commit of its own to diverge
	// with: it only ever fast-forwards. a go.mod makes the fixture a
	// module go build can find.
	withModule(t, one)
	// no push typed here: stick pushes the claim itself, and the
	// partner's sticky below only works if it did.
	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}

	if err := doSticky(two, cfg); err != nil {
		t.Fatalf("sticky: %v", err)
	}
	if _, ok, err := sticky.Stuck(two.Dir()); err != nil || !ok {
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
	if _, err := os.Stat(two.Path("bin", "miami")); err != nil {
		t.Errorf("the binary was not built: %v", err)
	}
	if _, err := os.Stat(two.Path("more.txt")); err != nil {
		t.Error("the build did not fast-forward to the driver's push")
	}
}

// TestReleaseAfterUnstickCarriesNoTrace is the design's own point: once
// unstuck, the flat release commit game release prepares has no
// .sticky in it anywhere, and neither does what game release push
// sends to dist.
func TestReleaseAfterUnstickCarriesNoTrace(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}
	commitFile(t, one, "feature.txt", "a feature\n", "a feature")
	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if _, ok, _ := sticky.Stuck(one.Dir()); ok {
		t.Fatal(".sticky is still in the working tree after unstick")
	}

	if err := releasePrepare(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := releasePush(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release push: %v", err)
	}
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
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)

	if _, err := sticky.Stick(one, road, "partner"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	err := releasePrepare(one, cfg, "v1", false, io.Discard, made)
	if err == nil || !strings.Contains(err.Error(), "unstick first") {
		t.Fatalf("want the unstick refusal, got %v", err)
	}
}

// TestReleasePushRefusesWhileStuck is the last gate: a release prepared
// before the claim does not go out while the claim holds. With nothing
// on the road to merge, the claim is all that is in the way, and the
// refusal says unstick.
func TestReleasePushRefusesWhileStuck(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)

	if err := releasePrepare(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release: %v", err)
	}
	// stuck by a name that is not this project's release author, so
	// the refusal is the sticky check and not the sweep's rule against
	// the author's own name in a released file.
	if _, err := sticky.Stick(one, road, "partner"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if _, err := one.Git("push", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}
	err := releasePush(one, cfg, "v1", false, io.Discard, made)
	if err == nil || !strings.Contains(err.Error(), "unstick first") {
		t.Fatalf("want the unstick refusal, got %v", err)
	}
	if main, _ := one.RemoteMain(dist); main != "" {
		t.Error("release push reached dist while stuck")
	}

	// unstick, prepare again from the clean tree, and it goes out.
	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if err := releasePrepare(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release after unstick: %v", err)
	}
	if err := releasePush(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release push after unstick: %v", err)
	}
	if main, _ := one.RemoteMain(dist); main == "" {
		t.Error("release push never reached dist after unstick")
	}
}

// TestReleasePushBehindTheSticker is the refusal's other wording: the
// other person holds the claim and has pushed work this tree has not
// taken, so the way out is to pull or build first.
func TestReleasePushBehindTheSticker(t *testing.T) {
	t.Parallel()
	one, two, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)

	if err := releasePrepare(one, cfg, "v1", false, io.Discard, made); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := sticky.Stick(two, road, "partner"); err != nil {
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
	err := releasePush(one, cfg, "v1", false, io.Discard, made)
	if err == nil ||
		!strings.Contains(err.Error(), "pull or build first") {
		t.Fatalf("want the merge refusal, got %v", err)
	}
	if main, _ := one.RemoteMain(dist); main != "" {
		t.Error("release push reached dist while behind the sticker")
	}
}

// claimOnRoad is who the road's work branch says holds the baton, read
// the way the other person's clone reads it: fetch the branch and look
// at the .sticky it carries.
func claimOnRoad(t *testing.T, clone repo.Repo, road string) (string, bool) {
	t.Helper()
	if _, err := clone.Git("fetch", "-q", road, "work"); err != nil {
		t.Fatalf("fetch %s work: %v", road, err)
	}
	holder, err := clone.Show("FETCH_HEAD", ".sticky")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(holder), true
}

// TestStickAndUnstickPushTheClaim is that the claim is where the other
// person looks: stick leaves the road carrying it, and unstick leaves
// the road without it, with no push typed by hand.
func TestStickAndUnstickPushTheClaim(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick: %v", err)
	}
	holder, held := claimOnRoad(t, two, road)
	if !held || holder != "ada" {
		t.Fatalf("the road does not carry the claim: %q held=%v",
			holder, held)
	}

	if err := doUnstick(one, cfg); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if holder, held := claimOnRoad(t, two, road); held {
		t.Fatalf("the road still carries the claim: %q", holder)
	}
}

// TestStickCatchesUpToTheRoadFirst: the road moved since this clone
// last fetched, plain work and no claim. a claim committed on top of
// the old tip cannot be pushed, and "game push sends it" is refused
// the same way. stick takes the road's tip first, then claims.
func TestStickCatchesUpToTheRoadFirst(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	commitFile(t, two, "theirs.txt", "the partner's work\n", "partner")
	if _, err := two.Git("push", "-q", road, "work"); err != nil {
		t.Fatalf("push: %v", err)
	}

	if err := doStick(one, cfg); err != nil {
		t.Fatalf("stick behind the road: %v", err)
	}
	if _, err := os.Stat(one.Path("theirs.txt")); err != nil {
		t.Error("stick did not take the road's tip first")
	}
	holder, held := claimOnRoad(t, two, road)
	if !held || holder != "ada" {
		t.Fatalf("the claim did not reach the road: %q held=%v",
			holder, held)
	}
}

// TestUnstickBehindTheStickerRefusesFirst: the other person has pushed
// work this tree has not taken. an unstick committed here cannot be
// pushed, and "game push sends the release" is refused the same way.
// it is refused before it commits, in the words release push uses for
// the same state, and after a build takes the road's tip it goes.
func TestUnstickBehindTheStickerRefusesFirst(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
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
	if _, ok, _ := sticky.Stuck(two.Dir()); !ok {
		t.Error("a refused unstick removed the claim")
	}

	if err := syncBuild(two, cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := doUnstick(two, cfg); err != nil {
		t.Fatalf("unstick after catching up: %v", err)
	}
	if holder, held := claimOnRoad(t, one, road); held {
		t.Errorf("the road still carries the claim: %q", holder)
	}
}
