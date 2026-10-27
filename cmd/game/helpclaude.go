package main

// helpClaude is what game help-claude prints, for a Claude session that has
// not used game before. it stays under 10,000 characters.
const helpClaude = `game help-claude: using game as an agent on this machine

You are reading this because you are a Claude session on a machine
where game builds, checks, lints and releases the Go projects, and you
have not used it before. Read it whole; it is short. --help after any
verb says what that verb does without doing it.

WHAT GAME IS
  the verbs a Go project needs that go itself does not know it needs:
  check, build, lint, push, pairing, mutants and a release. one binary,
  with git and go underneath and nothing else.

THE TWO FILES
  ~/.config/game/config   the builder's: who you are (author), the
                          never-words list, the licence, and a block for
                          each project naming its road and its dist.
                          this one is yours, and only yours.
  .game                   the repository's, committed: its name, which
                          picks the block in your config, and its lint
                          rules. it never names a remote.

GAME_HOME, FIRST AND EVERY TIME
  the config is read from $HOME/.config/game/config. an agent's $HOME
  is her home, so every call you make sets GAME_HOME to your own:

      GAME_HOME=/path/to/your/home game check

  without it you are quietly using her author line and her roads, and
  a write to her config is a write to her dotfiles, which no agent
  does. your config lives under your own home; edit that one.

ROAD AND DIST
  the road is the dirty remote: a bare repository on this machine that
  takes every commit, yours included. game push sends this branch there
  and says what it did. the dist is the clean remote people see; only
  game release push sends to it, one flat commit per release, and an
  agent never runs that. releases are hers, from her checkout and her
  config. a repository with no block in your config builds, checks and
  lints, and that is all: there is nowhere for it to push.

THE DAILY VERBS, IN THE ORDER YOU RUN THEM
  game trees      which block this repository matched, or that none did
  game check      gofmt -l, go vet, go test. with rapid checks N in
                  .game the property tests run with that many checks.
                  the exit code is the tests' own: never behind a pipe
  game build      every cmd/* into bin/, stamped with the commit; the
                  same sources build the same bytes. bin/NAME --age says
                  which commit a binary is
  game build-c SOURCE...  C sources into bin/NAME with cc and strict
                  flags: C11, -O2, every warning an error, -pedantic,
                  PIE with the stack armed; dyld's environment sealed
                  (__RESTRICT) and signed ad hoc for the hardened
                  runtime, so game harden passes it. NAME is the first
                  source's, or -o NAME. -with theft fetches theft at
                  its pinned commit and links it in
  game build-xcode -scheme S  the scheme built by xcodebuild into bin/
                  with the hardened settings said over the project's:
                  hardened runtime, strict msgSend, dead code stripped,
                  warnings as errors, no base entitlements. The
                  project's signing is used. -configuration (Release),
                  -project or -workspace, -destination (platform=macOS).
                  -with swiftqc or -with fox fetches it into bin/.deps
                  first, pinned
  game cover      how much the tests run, file by file; every file at
                  100% or the exit code says so. Go by default; cover c
                  [-with LIB] SOURCE... [-- ARG...] for a C test program
                  (clang's coverage); cover xcode -scheme S for Swift and
                  Objective-C (xccov). fetched libraries and tests are
                  not counted
  game fuzz c [-time S] SOURCE...  a libFuzzer target (.c or .m) built
                  by cc with the address sanitizer and Homebrew LLVM's
                  libFuzzer (brew install llvm@20), run S seconds (30).
                  what crashes it is kept in bin/.fuzz/ and fails it.
                  Swift has no fuzzer on macOS: fuzz xcode says so
  game race c SOURCE... [-- ARG...]  the program (.c or .m) built with
                  the thread sanitizer and run; a race fails it.
                  race xcode -scheme S runs the tests with it on.
                  a sanitizer runs only on the macOS its Xcode was built
                  for: on a macOS seed, the matching Xcode beta
  game aslr c SOURCE...  the program (.c or .m) built and checked with
                  otool for ASLR; a binary without PIE fails it.
                  aslr xcode -scheme S and aslr go build, then check;
                  plain aslr checks every Mach-O already in bin/
  game leaks c SOURCE... [-- ARG...]  the program run under the address
                  sanitizer, then under leaks; a leak fails it.
                  leaks xcode -scheme S tests under the sanitizer and
                  runs its executables under leaks. leaks go catches
                  what a Go program holds in cgo, not the Go heap,
                  which pprof measures
  game lint     the rules .game describes, counted; --look lists every
                  finding. a release wants zero. a line that says noqa,
                  code or comment, is let off every rule
  game harden    source, binaries and modules held to the hardened
                  default. harden xcode -scheme S adds the scheme's
                  build settings and entitlements. lint harden in .game
                  holds every lint to the source rules, and makes the
                  whole pass a gate: release and verify build, read the
                  binaries back and refuse a finding, as the lint does.
                  missing analyzers are a finding, not a pass
  game push       this branch to the road
  game who        who made the last release on dist, and who the last
                  three commits here were: the author, and the signature
                  line a session leaves last in its message

PAIRING, WHEN TWO OF YOU BUILD ONE TREE
  game stick      claim the repository: a committed .sticky with your
                  name, pushed to the road. refused on main, or when
                  someone else holds it
  game sticky     follow the claim: fast-forward to the holder's commit
                  now and on every build; refused over anything divergent
  game unstick    let go: remove the file, commit, push
  one of you sticks and writes; the other goes sticky and builds the
  same code. a stuck repository does not release.

MUTANTS, BEFORE A RELEASE
  game mutants --bench       how long a run would take at your workers
  game mutants --workers N   change the source one small thing at a time
                             and run the tests against each. what
                             survives is listed and is the exit code.
                             .mutants beside .game names the changes that
                             make no difference, NAME :: WHY, one a line.
                             a run past --timeout is timed out, not killed;
                             nothing is known of it until it runs again
  game mutants mutate-c -plugin PATH SOURCE...  C, by Mull: the test
                             program built with its plugin, then its runner
  game mutants mutate-xcode  Swift, by Muter and the tree's muter.conf.yml
  game mutants mutate-text FILE -- TEST...  any file, by universalmutator;
                             -as c reads an Objective-C file as C
  ask her before a run longer than a few minutes; she sets the workers.

STACKS, MANY REPOSITORIES BUILT AS ONE
  game build stack --from road  each member .game names, cloned fresh
                             into bin/stack at its ref, its patches laid,
                             then checked, tested, built and linted by its
                             own rules. the table says where each stopped
  game patch --dry           whether each member's patches still land
                             on a fresh clone in bin/patch; without --dry
                             they are laid there. a series lands whole or
                             not at all, and nothing is committed
  patches DIR, in .game      also look for NAME under DIR, for a stack that
                             can't keep patches in a member's own repository
                             (carrie: charmbracelet/patches). a member's
                             patches live in one place; two is refused

A RELEASE, WHICH IS HERS
  game release TAG           every check, lint at zero, the sweep clean,
                             one flat commit kept for its push
  game release diff TAG      what that commit would publish
  game release push TAG      the commit and its tag to the dist
  the word and the tag read in either order; a word release does not
  know stops it and says what was meant. the sweep refuses what agents
  leak without noticing: attribution trailers, session urls, this
  machine's paths, email addresses and the never-words. the bounce holds
  any change that names the author in the third person until she says.

WHAT EVERYONE FORGETS
  - GAME_HOME. every call.
  - your config has no block for this repository: game trees says so.
  - bin/game is a build, not an install; game --age tells them apart.
  - lint findings count, and so do the looks; a release wants zero.
  - a gate behind a pipe lies about its exit code. run game check bare.
  - never her config, never her dist, never a release.
`
