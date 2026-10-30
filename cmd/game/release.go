package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
	"github.com/janearc/game/internal/sweep"
	"github.com/janearc/puzzlemouth/pzm"
)

// releaseVerb handles four forms: TAG, diff TAG, push TAG and stack TAG.
func releaseVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	form, tag, err := releaseForm(call.Args())
	if err != nil {
		return err
	}
	if err := releaseFlagsFit(form, call); err != nil {
		return err
	}
	mean, publish := call.On("i-mean-it"), call.On("publish")
	from := valueOr(call, "from", "dist")
	switch form {
	case "stack":
		return releaseStack(here, cfg, tag, from, mean, call.Out(),
			build, built)
	case "diff":
		return releaseDiff(here, cfg, tag, neverWords(), call.Out())
	case "push":
		return releasePush(here, cfg, tag, publish, call.Out(), build,
			built)
	}
	return releasePrepare(here, cfg, tag, mean, call.Out(), build, built)
}

// releaseFlags are the flags each form of release takes. a flag the form
// does not take is refused.
var releaseFlags = map[string][]string{
	"":      {"i-mean-it"},
	"stack": {"from", "i-mean-it"},
	"push":  {"publish"},
	"diff":  nil,
}

// releaseFlagsFit refuses a flag that the form of release does not take.
func releaseFlagsFit(form string, call pzm.Call) error {
	given := map[string]bool{"i-mean-it": call.On("i-mean-it"),
		"from": call.Given("from"), "publish": call.On("publish")}
	for _, name := range []string{"i-mean-it", "from", "publish"} {
		if !given[name] {
			continue
		}
		if !slices.Contains(releaseFlags[form], name) {
			return fmt.Errorf("%s: --%s is a flag it does not take",
				strings.TrimSpace("release "+form), name)
		}
	}
	return nil
}

// releaseDiff is game release diff TAG. it shows what a release now would
// send to dist, with the sweep's refusals first. it sweeps for never and
// says it on out.
func releaseDiff(here repo.Repo, cfg config.Config, tag string,
	never sweep.Never, out io.Writer) error {
	name, dist, err := distFor(cfg, "nothing to diff against")
	if err != nil {
		return err
	}
	opts := releaseOptions(name, dist, tag, cfg)
	// the setting values are the release's refusal, not the diff's
	opts = opts.Refusing(nil)
	diff, err := release.Diff(here, opts)
	if err != nil {
		return err
	}
	what := name
	if tag != "" {
		what = name + " " + tag
	}
	if never.Empty() {
		fmt.Fprintf(out, "release diff %s: this game was built "+
			"without never-words, so the sweep below did not "+
			"look for them, and a release would refuse\n", what)
	}
	tellDiff(out, what, *diff)
	return nil
}

// tellDiff prints a diff. it says if there is nothing to send, else the two
// ends, the stat, what the sweep would refuse, and the patch.
func tellDiff(out io.Writer, what string, diff release.Difference) {
	if diff.Same() {
		fmt.Fprintf(
			out,
			"release diff %s: dist already has this tree, "+
				"so a release would change nothing\n",
			what,
		)
		return
	}
	from := "an empty dist"
	if diff.Parent() != "" {
		from = repo.Short(diff.Parent())
	}
	to := repo.Short(diff.Tree())
	fmt.Fprintf(out, "release diff %s: %s -> %s\n\n", what, from, to)
	// Repo.Git trims the stat, so the leading space and the trailing
	// newline are put back here.
	fmt.Fprintf(out, " %s\n", diff.Stat())
	// the sweep's hits are printed first, because any one of them
	// stops the release.
	for _, hit := range diff.Hits() {
		fmt.Fprintf(out, "would be refused: %s\n", hit)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, diff.Patch())
}

// releaseForms is the words release knows after itself. any other word
// that is not a flag is a tag.
var releaseForms = map[string]bool{"diff": true, "push": true, "stack": true}

// releaseForm reads the words after release into a form, a tag and the
// flags, with the form and the tag in either order. a word release does not
// know stops it, and a form that needs a tag says so.
func releaseForm(words []string) (form, tag string, err error) {
	for _, word := range words {
		switch {
		case releaseForms[word] && form == "":
			form = word
		case !releaseForms[word] && tag == "":
			tag = word
		default:
			return "", "", unknownWord(word, firstTag(words))
		}
	}
	if tag == "" && form != "diff" {
		want := "game release TAG"
		if form != "" {
			want = "game release " + form + " TAG"
		}
		return "", "", fmt.Errorf(
			"release wants a tag: %s; nothing was prepared",
			want,
		)
	}
	return form, tag, nil
}

// firstTag is the first word that is not a form, or TAG.
func firstTag(words []string) string {
	for _, word := range words {
		if !releaseForms[word] {
			return word
		}
	}
	return "TAG"
}

// unknownWord is the error for a word release does not know.
func unknownWord(word, tag string) error {
	return fmt.Errorf(
		"release: %q is not a word release knows; nothing was "+
			"prepared and nothing was pushed. did you mean game "+
			"release diff %s, or game release push %s",
		word,
		tag,
		tag,
	)
}

// noDistForRelease is why a block with no dist cannot release.
const noDistForRelease = "nowhere for a release to go: this tree " +
	"builds locally and that is all"

// distFor is the project's name and dist, or why no dist is available.
func distFor(cfg config.Config, why string) (name, dist string, err error) {
	name = cfg.Named()
	mine, held := cfg.Mine()
	if !held {
		return name, "", cfg.NoBlock()
	}
	if mine.Dist() == "" {
		return name, "", fmt.Errorf(
			"%s has no dist in its block in %s, so there is %s",
			name,
			cfg.Dotfile(),
			why,
		)
	}
	return name, mine.Dist(), nil
}

// releaseOptions returns the release package's options. they hold the
// project, its dist and tag, and everything the sweep refuses.
func releaseOptions(name, dist, tag string, cfg config.Config) release.Options {
	return release.For(name, dist, tag).
		Swept(neverWords(), cfg.Exclude(), cfg.SweepAllow()).
		Omitting(cfg.Omit()).
		By(cfg.Author()).
		Licensed(cfg.License()).
		Refusing(slices.Collect(maps.Values(cfg.Set()))).
		PatchedFrom(cfg.Patches())
}

// releaseChecks is what a release passes before a commit is cut. every
// needed setting is set, the lint is at zero unless meant, go's own
// checks pass where there is a module, and the test targets pass.
//
// with lint harden in the .game, the layers beyond the source rules
// run last, over binaries freshly built, and their findings refuse the
// release as the lint's do, unless meant.
func releaseChecks(here repo.Repo, cfg config.Config, mean bool, by runner,
	out io.Writer) error {
	if err := needsSet(cfg, out); err != nil {
		return err
	}
	rules, err := lint.Describe(cfg.Lint())
	if err != nil {
		return err
	}
	if err := lintClean(here, rules, mean, out); err != nil {
		return err
	}
	if hasGoMod(here) {
		if err := check(here, cfg, by, out); err != nil {
			return err
		}
	}
	for _, target := range cfg.Targets() {
		if !testTarget(target.Name()) {
			continue
		}
		err := buildTarget(here, cfg, target.Name(), out)
		if err != nil {
			return err
		}
	}
	if !rules.Harden() {
		return nil
	}
	beyond, err := hardenBuilt(here, cfg, by, out)
	if err != nil {
		return err
	}
	return refuseHardened(beyond, mean, out)
}

// needsSet refuses a release while a needed setting is unset, saying
// each on out.
func needsSet(cfg config.Config, out io.Writer) error {
	miss := cfg.Missing()
	if count.None(len(miss)) {
		return nil
	}
	for _, need := range miss {
		fmt.Fprintf(out, "release: %s is needed: %s\n", need.Name(),
			need.Why())
	}
	return fmt.Errorf("refused: set the needed settings in " +
		"~/.config/game/config first")
}

// lintClean refuses a release on any lint finding unless meant, saying
// each on out.
func lintClean(here repo.Repo, rules lint.Lint, mean bool,
	out io.Writer) error {
	found, err := rules.Run(here.Dir())
	if err != nil {
		return err
	}
	if count.None(len(found)) || mean {
		return nil
	}
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	return fmt.Errorf("refused: the lint found %d; a release "+
		"takes none (--i-mean-it overrides)", len(found))
}

// hardenBuilt is the harden layers beyond the source rules, over
// binaries built now rather than whatever bin/ held: a stale bin/
// audited clean would be a false claim about the tree being released.
func hardenBuilt(here repo.Repo, cfg config.Config, by runner,
	out io.Writer) ([]lint.Finding, error) {
	if st, err := os.Stat(here.Path("cmd")); err == nil &&
		st.IsDir() {
		if err := buildBinaries(here, cfg, madeBy(here), by, out,
			false); err != nil {
			return nil, err
		}
	}
	return hardenBeyondSource(here.Dir(), by)
}

// refuseHardened says each harden finding on out and refuses, unless
// meant: --i-mean-it is the glass to break when a release has to go
// out over a finding, and it breaks here as it does over the lint.
func refuseHardened(found []lint.Finding, mean bool, out io.Writer) error {
	if none(found) {
		return nil
	}
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	if mean {
		fmt.Fprintf(out, "release: harden found %d, and you meant it\n",
			len(found))
		return nil
	}
	return fmt.Errorf(
		"refused: harden found %d; a release "+
			"takes none (--i-mean-it overrides)",
		len(found),
	)
}

// testTarget returns true if the name is "test" or starts with "test-".
func testTarget(name string) bool {
	return name == "test" || strings.HasPrefix(name, "test-")
}

// releasePrepare is game release TAG. it runs the gate and the checks, then
// keeps one flat commit at the release ref for its push.
func releasePrepare(
	here repo.Repo,
	cfg config.Config,
	tag string,
	mean bool,
	out io.Writer,
	gameCommit, gameBuilt string,
) error {
	if err := needStamp(gameCommit, gameBuilt); err != nil {
		return err
	}
	if err := refuseStuck(here, "flat tree"); err != nil {
		return err
	}
	name, dist, err := distFor(cfg, noDistForRelease)
	if err != nil {
		return err
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	if err := releaseChecks(here, cfg, mean, run, out); err != nil {
		return err
	}
	res, err := release.Prepare(here, releaseOptions(name, dist, tag, cfg))
	if err != nil {
		return err
	}
	commit := repo.Short(res.Commit())
	fmt.Fprintf(out, "release %s %s: prepared %s, tree %s\n", name, tag,
		commit, res.Tree())
	fmt.Fprintf(
		out,
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		dist,
	)
	return nil
}

// needStamp refuses to make or push a release from a binary that can't say
// which commit it came from. commit and at are the commit a game was
// built from and that commit's time: for this binary, build and built.
func needStamp(commit, at string) error {
	if at != "" && commit != "dev" {
		return nil
	}
	return fmt.Errorf(
		"this game was built by go rather than by game, so it cannot " +
			"say which commit it is. a release is a claim and it " +
			"wants a tool with provenance: run sh " +
			"bootstrap.sh, or game build, and try again",
	)
}

// export writes a commit's tree into a directory, starting git and tar
// with command.
func export(
	here repo.Repo,
	commit, dir string,
	command func(string, ...string) *exec.Cmd,
) error {
	archive := command("git", "-C", here.Dir(), "archive", commit)
	untar := command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var said strings.Builder
	untar.Stderr = &said
	if err := untar.Start(); err != nil {
		return err
	}
	archiveErr := archive.Run()
	tarErr := untar.Wait()
	var refused *exec.ExitError
	if errors.As(archiveErr, &refused) && refused.Exited() {
		return archiveErr
	}
	if tarErr != nil {
		text := strings.TrimSpace(said.String())
		return fmt.Errorf("tar: %s: %w", text, tarErr)
	}
	return archiveErr
}

// stuckPush refuses release push on a claimed repository.
func stuckPush(here repo.Repo, cfg config.Config) error {
	mine, _ := cfg.Mine()
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	if mine.Road() != "" {
		road, err := roadFor(here, cfg)
		if err != nil {
			return err
		}
		behind, err := sticky.Behind(here, road, branch)
		if err != nil {
			return fmt.Errorf(
				"stuck, and the road could not be read: %v",
				err,
			)
		}
		if count.Positive(behind) {
			return mergeFirst()
		}
	}
	return fmt.Errorf(
		"stuck: unstick first, then release again, so the flat " +
			"commit carries no trace of the claim",
	)
}

// mergeFirst is the error when the road is ahead of a claimed tree.
func mergeFirst() error {
	return fmt.Errorf(
		"cannot un-stick, sticker has changes which require " +
			"merge; pull or build first",
	)
}

// releasePush sends the prepared commit and its tag to dist.
func releasePush(
	here repo.Repo,
	cfg config.Config,
	tag string,
	publish bool,
	out io.Writer,
	gameCommit, gameBuilt string,
) error {
	if stuck, err := isStuck(here); err != nil {
		return err
	} else if stuck {
		return stuckPush(here, cfg)
	}
	name, dist, err := distFor(cfg, noDistForRelease)
	if err != nil {
		return err
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	if err := needStamp(gameCommit, gameBuilt); err != nil {
		return err
	}
	if here.Leaves(dist) && !publish {
		return fmt.Errorf(
			"%s is not a path on this machine, so this push "+
				"publishes. that is a deliberate act and it "+
				"wants the word: game release push %s "+
				"--publish. preparing a release needs no "+
				"such thing",
			dist,
			tag,
		)
	}
	res, err := release.Push(here, release.For(name, dist, tag))
	if err != nil {
		return err
	}
	commit := repo.Short(res.Commit())
	fmt.Fprintf(out, "dist (clean): %s: main at %s, tagged %s\n", dist,
		commit, tag)
	painted(out, here.Dir(), cfg.Named())
	return nil
}

// hookMark is the line that says a pre-push hook is game's to rewrite.
const hookMark = "# game: the guard on dist"

// guard installs the pre-push hook that refuses any push to dist that
// did not come from game release push. a pre-push hook that is not
// game's is left alone, and guard says so.
func guard(here repo.Repo, dist string, out io.Writer) error {
	if dist == "" {
		return nil
	}
	dir, err := here.Git("rev-parse", "--git-path", "hooks")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(dir) {
		dir = here.Path(dir)
	}
	path := filepath.Join(dir, "pre-push")
	if hook, err := os.ReadFile(path); err == nil &&
		!strings.Contains(string(hook), hookMark) {
		fmt.Fprintf(
			out,
			"guard: %s is not game's; dist is not guarded here\n",
			path,
		)
		return nil
	}
	script := "#!/bin/sh\n" + hookMark +
		". only game release push sends to it.\n" +
		"dist='" + strings.ReplaceAll(
		dist,
		"'",
		"'\\''",
	) + "'\n" +
		"if [ \"$2\" = \"$dist\" ] && [ \"$" +
		guardName() +
		"\" != 1 ]; then\n" +
		"echo \"refused: $2 is dist; only game release push " +
		"sends there\" >&2\n" +
		"  exit 1\nfi\n"
	if err := os.MkdirAll(dir, folderMode); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(script), runnable)
}

// runnable is the mode a hook is written with: the owner writes, and
// everyone reads and runs it, defined as 0755 in sys/stat.h's bits.
const runnable = 0o755

// guardName is the name of the guard's variable, before its equals
// sign in release.Guard.
func guardName() string {
	name, _, _ := strings.Cut(release.Guard, "=")
	return name
}
