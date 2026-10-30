package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
	"github.com/janearc/puzzlemouth/pzm"
)

// buildVerb is game build. it builds the binaries, a named target, docker or
// the stack, and shows the art after a build that passed.
func buildVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	stack, err := stackAsked(call)
	if err != nil {
		return err
	}
	return buildWord(here, cfg, call.Args(), stack, call.Out(),
		stdoutColour(), os.Executable)
}

// buildWord is what game build does for the word it was given, saying what
// it built on out, and the art too when coloured. a member of a stack is
// verified by the program executable names.
func buildWord(
	here repo.Repo,
	cfg config.Config,
	args []string,
	stack stackAsk,
	out io.Writer,
	coloured bool,
	executable func() (string, error),
) error {
	var err error
	word, _, said := verbWord(args)
	switch {
	case !said:
		err = buildBinaries(here, cfg, madeBy(here), run, out, coloured)
	case word == "stack":
		child, bad := selfVerify(executable)
		if bad != nil {
			return bad
		}
		err = buildStack(here, cfg, stack, out, child)
	case word == "docker":
		err = buildDocker(here, cfg, exec.Command, out)
	default:
		err = buildTarget(here, cfg, word, out)
	}
	if err == nil && coloured {
		paint(out, here.Dir(), cfg.Named())
	}
	return err
}

// buildBinaries builds every cmd/* into bin/ with a stamp, for --age and
// --who to read.
//
// the stamp is the commit and its time, never the clock. it names who
// built it and the session lines that end the commits behind it. those
// lines are what the messages say, not a signature.
//
// the tree's path is trimmed, so one builder makes the same bytes from
// the same sources in any tree. -X writes the builder in, so two
// builders make two binaries.
//
// while the repository is claimed, a build first fast-forwards this
// branch to the road's tip as game sticky does. it reports what moved.
// every binary it builds after that is marked sticky build in its own
// colour.
//
// the binaries are built by maker, with go run by by, and each one built
// is said on out, in its colour when coloured.
func buildBinaries(here repo.Repo, cfg config.Config, maker string,
	by runner, out io.Writer, coloured bool) error {
	entries, err := os.ReadDir(here.Path("cmd"))
	if err != nil {
		return fmt.Errorf("no cmd/ directory to build")
	}
	stuck, err := isStuck(here)
	if err != nil {
		return err
	}
	if stuck {
		if err := syncBuild(here, cfg, out); err != nil {
			return err
		}
	}
	commit, stamp := stamped(here, maker)
	os.MkdirAll(here.Path("bin"), folderMode)
	if err := bakeNever(here, cfg, by); err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		program := filepath.Join("bin", name)
		_, err := by(here.Dir(), "go", "build", "-trimpath", "-ldflags",
			stamp, "-o", program, "./cmd/"+name)
		if err != nil {
			return err
		}
		err = signGo(here.Dir(), program, by, runtime.GOOS)
		if err != nil {
			return err
		}
		verb := "build"
		if stuck {
			verb = "sticky build"
		}
		line := fmt.Sprintf("%s: bin/%s %s", verb, name, commit)
		if stuck {
			line = stickyColour(line, coloured)
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

// madeBy is who is building here: the session id in the environment and
// this machine.
func madeBy(here repo.Repo) string {
	host, _ := os.Hostname()
	return builder(here, os.Getenv("CLAUDE_CODE_SESSION_ID"), host)
}

// bakeNever puts game's configured never-word hashes where go embeds them.
func bakeNever(here repo.Repo, cfg config.Config, by runner) error {
	if cfg.Named() != "game" {
		return nil
	}
	_, err := by(here.Dir(), "go", "run", "./cmd/never")
	return err
}

// shortCommit is the short hash of HEAD, marked dirty when the tree is.
func shortCommit(here repo.Repo) string {
	commit, _ := here.Git("rev-parse", "--short", "HEAD")
	if clean, _ := here.Clean(); !clean {
		commit += "-dirty"
	}
	return commit
}

// stamped is the commit a build is at, marked dirty when the tree is,
// and the linker flags that write it, its time, its maker and the
// session lines into the binary.
func stamped(here repo.Repo, maker string) (commit, stamp string) {
	commit = shortCommit(here)
	when, _ := here.Git("log", "-1", "--format=%cI")
	sids := sessions(here)
	stamp = fmt.Sprintf(
		"-X main.build=%s -X main.built=%s -X 'main.stampedWho=%s' "+
			"-X main.stampedSids=%s",
		commit, when, maker, sids,
	)
	return commit, stamp
}

// signer is the program that signs a Go binary on a Mac.
const signer = "codesign"

// signGo signs a Go binary ad hoc on a Mac, so one built for Intel is
// signed as the linker already signs one for Apple silicon. Elsewhere
// there is no codesign and nothing to do.
//
// the hardened runtime is left off: it brings library validation, which
// refuses any dylib a cgo program loads that is not signed by the same
// identity, and ad hoc is no identity at all.
//
// goos is the operating system the binary is signed on.
func signGo(dir, program string, by runner, goos string) error {
	if goos != "darwin" {
		return nil
	}
	_, err := by(dir, signer, "-s", "-", "-f", program)
	return err
}

// syncBuild is the fast-forward a build does while the repository is
// claimed, saying on out what it merged. see README.md, "human-agent
// pairing".
func syncBuild(here repo.Repo, cfg config.Config, out io.Writer) error {
	mine, held := cfg.Mine()
	if !held || mine.Road() == "" {
		return nil
	}
	road, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	branch, err := here.Branch()
	if err != nil {
		return fmt.Errorf("no branch to sync: %w", err)
	}
	tip, moved, err := sticky.Sync(here, road, branch)
	if err != nil {
		return err
	}
	if moved {
		fmt.Fprintf(out, "build: updates from %s merged\n",
			repo.Short(tip))
	}
	return nil
}

// stickyColour returns the line in a sticky build's colour when coloured,
// which colour decides as it does for paint: only to a terminal, and
// never with NO_COLOR set.
func stickyColour(line string, coloured bool) string {
	if !coloured {
		return line
	}
	return "\x1b[36m" + line + "\x1b[0m"
}

// buildDocker builds an image of this repository, starting docker with
// start and saying what it built on out.
func buildDocker(here repo.Repo, cfg config.Config,
	start func(string, ...string) *exec.Cmd, out io.Writer) error {
	if _, err := os.Stat(here.Path("Dockerfile")); err != nil {
		return fmt.Errorf("no Dockerfile to build")
	}
	name := cfg.Named()
	commit := shortCommit(here)
	image := name + ":" + commit
	cmd := start("docker", "build", "-t", image, ".")
	cmd.Dir, cmd.Stdout, cmd.Stderr = here.Dir(), out, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build %s: %w", image, err)
	}
	fmt.Fprintf(out, "build docker: %s\n", image)
	if needs := cfg.Needs(); count.None(len(needs)) {
		return nil
	}
	var yaml strings.Builder
	fmt.Fprintf(
		&yaml,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  "+
			"name: %s-settings\ndata:\n",
		name,
	)
	for _, kv := range cfg.Env() {
		key, value, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&yaml, "  %s: %q\n", key, value)
	}
	os.MkdirAll(here.Path("bin"), folderMode)
	path := here.Path("bin", name+"-settings.yaml")
	settings := []byte(yaml.String())
	if err := os.WriteFile(path, settings, ownerOnly); err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"build docker: the settings it needs are in %s, for "+
			"the cluster, not the image\n",
		path,
	)
	return nil
}
