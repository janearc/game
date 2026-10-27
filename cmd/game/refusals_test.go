package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// refused is whether err is a refusal that says want.
func refused(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}

// stuckUnreadably makes .sticky a directory, so asking whether the tree
// is stuck is an error rather than a yes or a no.
func stuckUnreadably(t *testing.T, clone repo.Repo) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.Mkdir(clone.Path(".sticky"), openDir); err != nil {
		t.Fatal(err)
	}
}

// unguardable points the tree's hooks at a file, so the guard on dist
// cannot be written.
func unguardable(t *testing.T, clone repo.Repo) {
	t.Helper()
	if _, err := clone.Git("config", "core.hooksPath",
		clone.Path("a.txt")); err != nil {
		t.Fatal(err)
	}
}

// TestAStampNeedsBothItsCommitAndItsTime: a game that has a commit but
// no time, or a time but says dev, cannot say what it is.
func TestAStampNeedsBothItsCommitAndItsTime(t *testing.T) {
	t.Parallel()
	for _, half := range []stamp{
		{commit: "abc1234"}, {commit: "dev", at: made.at},
	} {
		if err := half.stamped(); !refused(err, "built by go") {
			t.Errorf("%+v: %v, want the provenance refusal", half, err)
		}
	}
	if err := made.stamped(); err != nil {
		t.Errorf("a whole stamp: %v", err)
	}
}

// TestReleaseVerbReadsItsWordsFirst: a release with no tag, or a word
// left over after its flags, is refused before anything runs; a whole
// one reaches its form, which in a game built by go test is refused
// for want of a stamp. so each form is given this binary's own.
func TestReleaseVerbReadsItsWordsFirst(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)
	for _, words := range []struct {
		args []string
		want string
	}{
		{nil, "release wants a tag"},
		{[]string{"v1", "--i-mean-it", "again"}, `"again" is not a word`},
		{[]string{"v1"}, "built by go"},
		{[]string{"push", "v1"}, "built by go"},
		{[]string{"stack", "v1"}, "built by go"},
	} {
		if err := releaseVerb(one, cfg, words.args); !refused(err,
			words.want) {
			t.Errorf("release %q: %v, want %q", words.args, err,
				words.want)
		}
	}
}

// gateSet is what a gate changes about the tree or the config.
type gateSet func(t *testing.T, clone repo.Repo, spec *gateSpec)

// gateSpec is the road and dist the config has, and the lines of its .game
// besides, until a gate has changed them.
type gateSpec struct {
	road, dist string
	game       []string
}

// setStuckUnreadably makes .sticky a directory.
func setStuckUnreadably(t *testing.T, clone repo.Repo, _ *gateSpec) {
	stuckUnreadably(t, clone)
}

// setNoDist takes the project's dist away.
func setNoDist(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.road, spec.dist = "", ""
}

// setNoGuard makes the tree's hooks unwritable.
func setNoGuard(t *testing.T, clone repo.Repo, _ *gateSpec) {
	unguardable(t, clone)
}

// setNeeding makes the config need a setting nothing gives.
func setNeeding(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.game = append(spec.game, "needs TOKEN a test")
}

// setNeverWord commits a never-word to the tree.
func setNeverWord(t *testing.T, clone repo.Repo, _ *gateSpec) {
	commitFile(t, clone, "b.txt", "zorblax\n", "a word")
}

// setRemoteUnsaid gives the project a dist but no road, so the release
// is never said to anyone.
func setRemoteUnsaid(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.road, spec.dist = "", "https://example.invalid/x.git"
}

// prepareGate is one gate in front of the cut: what to change, the
// stamp, and the words of the refusal.
type prepareGate struct {
	name string
	set  gateSet
	made stamp
	want string
}

// check is the gate as a sub-test: refused in its own words, and
// nothing prepared.
func (gate prepareGate) check(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	spec := gateSpec{road: road, dist: dist}
	if gate.set != nil {
		gate.set(t, one, &spec)
	}
	cfg := cfgWith(t, "ada", spec.road, spec.dist, spec.game...)
	err := releasePrepare(one, cfg, "v1", false, io.Discard, gate.made)
	if !refused(err, gate.want) {
		t.Errorf("%v, want %q", err, gate.want)
	}
	if kept, _ := one.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused release kept %s", kept)
	}
}

// TestReleasePrepareRefusesBeforeItCuts: each gate in front of the cut
// refuses on its own, in its own words, and nothing is prepared.
func TestReleasePrepareRefusesBeforeItCuts(t *testing.T) {
	t.Parallel()
	gates := []prepareGate{
		{"unstamped", nil, unstamped, "built by go"},
		{"stuck unreadably", setStuckUnreadably, made,
			".sticky: is a directory"},
		{"no dist", setNoDist, made, "has no dist"},
		{"no guard", setNoGuard, made, "a.txt: not a directory"},
		{"a setting not set", setNeeding, made, "set the needed settings"},
		{"a never-word", setNeverWord, made, "a never-word in b.txt"},
	}
	for _, gate := range gates {
		t.Run(gate.name, gate.check)
	}
}

// pushGate is one gate in front of a push: what to change, whether a
// release is prepared first, the stamp, and the words of the refusal.
type pushGate struct {
	name    string
	set     gateSet
	prepare bool
	made    stamp
	want    string
}

// check is the gate as a sub-test: refused in its own words, and dist
// left without a main.
func (gate pushGate) check(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)
	if gate.prepare {
		if err := releasePrepare(one, cfg, "v1", false,
			io.Discard, made); err != nil {
			t.Fatal(err)
		}
	}
	spec := gateSpec{road: road, dist: dist}
	if gate.set != nil {
		gate.set(t, one, &spec)
	}
	cfg = cfgWith(t, "ada", spec.road, spec.dist, spec.game...)
	err := releasePush(one, cfg, "v1", false, io.Discard, gate.made)
	if !refused(err, gate.want) {
		t.Errorf("%v, want %q", err, gate.want)
	}
	if main, _ := one.RemoteMain(dist); main != "" {
		t.Errorf("a refused push reached dist: %s", main)
	}
}

// TestReleasePushRefusesBeforeItSends: each gate in front of the push
// refuses on its own, and dist is left without a main.
func TestReleasePushRefusesBeforeItSends(t *testing.T) {
	t.Parallel()
	gates := []pushGate{
		{"stuck unreadably", setStuckUnreadably, false, made,
			".sticky: is a directory"},
		{"no dist", setNoDist, false, made, "has no dist"},
		{"no guard", setNoGuard, true, made, "a.txt: not a directory"},
		{"unstamped", nil, true, unstamped, "built by go"},
		{"a remote, unsaid", setRemoteUnsaid, false, made,
			"wants the word"},
		{"nothing prepared", nil, false, made, "no prepared release v1"},
	}
	for _, gate := range gates {
		t.Run(gate.name, gate.check)
	}
}

// TestAReleaseSaysWhatItPreparedAndSent: prepare names the commit by
// its first seven, and the tree; push names the same seven on dist.
func TestAReleaseSaysWhatItPreparedAndSent(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)
	var said bytes.Buffer
	if err := releasePrepare(one, cfg, "v1", false, &said,
		made); err != nil {
		t.Fatal(err)
	}
	if err := releasePush(one, cfg, "v1", false, &said, made); err != nil {
		t.Fatal(err)
	}
	main, err := one.RemoteMain(dist)
	if err != nil || main == "" {
		t.Fatalf("dist has no main: %q, %v", main, err)
	}
	short := regexp.QuoteMeta(repo.Short(main))
	for _, line := range []string{
		`release x v1: prepared ` + short + `, tree [0-9a-f]{40}\n`,
		`send it with: game release push v1, to dist \(clean\): `,
		`dist \(clean\): .*: main at ` + short + `, tagged v1\n`,
	} {
		if !regexp.MustCompile(line).MatchString(said.String()) {
			t.Errorf("want %s in\n%s", line, said.String())
		}
	}
}

// setHostAndPath gives dist as a host and a path with no user, which
// git reads as another machine.
func setHostAndPath(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.dist = "example.invalid:x.git"
}

// setFtp gives dist as a url git sends elsewhere though it names none of
// the usual schemes.
func setFtp(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.dist = "ftp://example.invalid/x.git"
}

// setTransportHelper gives dist as a transport helper, which runs a
// program of its own to send.
func setTransportHelper(_ *testing.T, _ repo.Repo, spec *gateSpec) {
	spec.dist = "ext::sh -c true"
}

// setNamedFarRemote gives dist as the name of a remote whose url is
// another machine.
func setNamedFarRemote(t *testing.T, clone repo.Repo, spec *gateSpec) {
	t.Helper()
	url := "https://example.invalid/far.git"
	if _, err := clone.Git("remote", "add", "far", url); err != nil {
		t.Fatal(err)
	}
	spec.dist = "far"
}

// setRewrittenPath gives dist as a path that the repository's config
// sends to another machine when it pushes.
func setRewrittenPath(t *testing.T, clone repo.Repo, spec *gateSpec) {
	t.Helper()
	key := "url.example.invalid:.pushInsteadOf"
	if _, err := clone.Git("config", key, "/mirrored/"); err != nil {
		t.Fatal(err)
	}
	spec.dist = "/mirrored/x.git"
}

// TestReleasePushWantsTheWordForEveryWayToLeave: a dist that git would
// send to another machine needs --publish however it is spelt, and
// nothing reaches it without the word.
func TestReleasePushWantsTheWordForEveryWayToLeave(t *testing.T) {
	t.Parallel()
	gates := []pushGate{
		{"host and path", setHostAndPath, false, made, "wants the word"},
		{"ftp", setFtp, false, made, "wants the word"},
		{"transport helper", setTransportHelper, false, made,
			"wants the word"},
		{"a remote named far", setNamedFarRemote, false, made,
			"wants the word"},
		{"a rewritten path", setRewrittenPath, false, made,
			"wants the word"},
	}
	for _, gate := range gates {
		t.Run(gate.name, gate.check)
	}
}
