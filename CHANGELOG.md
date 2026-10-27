# changelog

## v0.4.1, 2026-10-27

this is an interim release on the way to v0.5.0.

new capabilities include hardening, and and assorted additional non-go targets
(c, objc, swift) that were necessary to build xnu and a stub for fseventsd.
these are not going to be supported long-term, but serve as a good way to
mature the rest of the game codebase.

this release is built on `puzzlemouth` (`pzm`, which so far is unreleased). if
you want a version of game not built on pzm, please use v0.4.0.

## game v0.4.1

new verbs:

- mutants changes the source one small thing at a time and runs the tests
  against each change. what survives is listed and is the exit code.
- mutants takes --workers, --timeout, --only and --again.
  it takes --report and --bench. --bench says how long a run would take and
  stops.
- .mutants beside .game names the changes that make no difference, one per
  line as NAME :: WHY.
- each survivor and timeout is printed the moment it is decided.
- SIGINT or SIGTERM stops the run. it still writes the report, marked partial.
  the exit code is nonzero.
- --timeout under twice the time of the unchanged suite is refused. the
  message gives a timeout to use.
- the copies of the tree go in $TMPDIR, or in the user cache folder when
  $TMPDIR is under /var/folders. Xcode does not measure coverage there.
- mutants mutate-c, mutate-xcode and mutate-text reach C, Swift and any other
  file. they call Mull, Muter and universalmutator. each must be installed.
- build-c builds C sources into bin/ with cc. the result is signed ad hoc for
  the hardened runtime. the flags are:
  - C11 and -O2.
  - -Wall -Wextra -Werror -pedantic.
  - PIE and stack protection.
- build-xcode builds a scheme into bin/ with the hardened settings applied
  over the project's own. it takes -configuration and -destination.
  it takes -project or -workspace.
- build-c and build-xcode take -with to fetch theft, fox or swiftqc. each is
  pinned to one commit and one checksum, and a tarball that does not match is
  refused.
- cover reports coverage file by file and exits nonzero when a file is below
  100%. it covers Go with no argument. `cover c` covers C.
  `cover xcode -scheme S` covers Xcode.
- fuzz runs a libFuzzer target built from C or Objective-C. what crashes it is
  kept in bin/.fuzz/. fuzz xcode says Swift has no fuzzer on macOS.
- race builds C or Objective-C with the thread sanitizer, or runs an Xcode
  scheme's tests with it. any race fails the run.
- aslr checks that the built binaries are position-independent. with no
  argument it checks every Mach-O in bin/.
- leaks runs a program under the address sanitizer, then under leaks. it
  takes c, xcode or go.
- harden audits source, binaries and modules. harden xcode -scheme S adds the
  scheme's build settings and runs the Xcode analyzer.
- harden runs gosec, govulncheck and semgrep. tools that are missing count
  as findings. they never pass.
- patch lays a stack member's patches on a fresh clone in bin/patch. the series
  lands whole or not at all. --dry only says whether each patch would land.
- who, also called history, says who made the last release on dist and who
  made the last three commits. it reads the signature line at the end of each
  message.
- help-claude prints a short guide for an agent new to game.
- game and every verb take --help. --agent-help prints the same for an agent.
  most verbs take --dry-run, which says what the verb would do and does
  nothing. every verb is also spelled --VERB.
- doc/getting-started.md and doc/hardening.md are new.

release:

- `lint harden` in .game turns on the hardening source rules. the release then
  builds every cmd/* fresh and audits the binaries. any finding refuses it.
  verify does the same for a stack member. --i-mean-it overrides it and says
  so.
- release push sends the main commit and the tag in one atomic push.
- release push asks git where dist goes before it decides the dist is local.
- release stack vets and tests the assembled modules before it keeps the
  ref. it holds the stack to every rule a release is held to.
- release stack lays each member's patches. members can also omit what
  their own .game omits.
- stack lines may end in `theirs` for a member that is someone else's code.
  that member is held to its own lint rules and not to ours.
- `patches DIR` in .game says where a stack keeps its patches when they are
  not in the member's own repository. unreadable places are errors.
- stack builds retry a member that fails on the network up to three times.
  other failures are tried once.
- stack builds read each member as the stack leaves it, and read its .game
  once.

lint:

- the lint has four rules for comments that are not plain:
  - chain flags chained clauses.
  - commas flags too many commas.
  - opener flags comments that open on an article.
  - history flags a comment that tells the code's past.
- noqa on a line lets it off every rule.
- `rapid checks N` in .game sets how many checks each property test runs under
  game check.
- game check formats with the gofmt of the toolchain the module builds with,
  not the one first on PATH.
- when a module asks for a newer go than game was built with, the parse error
  says which two versions and tells you to rebuild game.
- the vim syntax file knows the new config keys and lint rules.

builds:

- game build stamps who built each binary and the sessions named in recent
  commits. `--who` on a built tool reads them back, and operation.md says
  what a build is and is not stamped with.
- game build signs Go binaries ad hoc on a Mac.
- clean leaves a go.work.sum that git tracks.
- game builds with puzzlemouth.
- exit code 2 means a word game cannot start on, or a config it cannot read.
  exit code 1 means the verb failed.

fixes:

- the sweep reads file names and the bytes of every file.
- the never-words list is read whole, or the sweep says why it could not.
- the Go audit reports a file it cannot read. it used to pass over it.
- mutants past the timeout are reported as timed out and not as killed.
- stack build fails with a message for a member given no child.
  it no longer panics.
- git is not run for a repository that has no directory.
- bounce counts one commit as that commit's lines, and a merge as the lines it
  brought in.
- sticky treats a tree ahead of the road as ahead and not diverged.

removals:

- unused exported names are gone from the internal packages. no caller
  outside the repository is affected.

internal:

- git is run through one runner. tests for repo, bounce and sweep run on an
  in-memory git.
- tests run in parallel and use generated mocks.
