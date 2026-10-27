# Hardening in game

This documents the ASLR, memory-leak and hardening work added to `game`. It
is written for review: what each verb does, which files hold it, how it fails,
what it leaves out, and what still needs doing.

## New verbs

Three verbs were added to the `verbs` table in `cmd/game/main.go`.

    game aslr     prove the built binaries are position-independent
    game leaks    find memory errors and leaks in the built programs
    game harden   audit source, binaries, modules and build settings

### game aslr

`cmd/game/aslr.go`, tests in `cmd/game/aslr_test.go`,
`cmd/game/hardening_e2e_test.go`.

    game aslr                      check every Mach-O already in bin/
    game aslr c SOURCE...          build, then check its binary
    game aslr xcode -scheme S      build, then check what it left in bin/
    game aslr go                   build every cmd/*, then check

Each Mach-O is read with `otool -hv`. An `EXECUTE` slice without the `PIE`
flag fails the check. Dylibs and frameworks are position-independent by their
filetype and are not asked for the flag.

### game leaks

`cmd/game/leaks.go`, tests in `cmd/game/leaks_test.go`,
`cmd/game/hardening_e2e_test.go`.

    game leaks c SOURCE... [-- ARG...]
    game leaks xcode -scheme S
    game leaks go

C and Objective-C programs are built twice. Once under the address sanitizer
(`-fsanitize=address`) and run, which catches memory errors. Then without
optimising (`-O0 -g -fno-omit-frame-pointer`) and run under Apple's `leaks`
tool, which catches what was left allocated.

The unoptimised build exists because an optimiser would otherwise erase the
allocation it should have leaked.

Xcode schemes are tested with `-enableAddressSanitizer YES`, their products
built into `bin/`, and each command-line executable left there run under
`leaks`. Go programs are built and run under `leaks`, which sees only what the
program holds outside the Go heap, in cgo and system frameworks.

## game harden

`cmd/game/harden.go` is the pipeline. The other layers live in
`cmd/game/harden_macho.go`, `cmd/game/harden_xcode.go`,
`cmd/game/harden_go.go`, `cmd/game/harden_source.go`, and the source rules in
`internal/lint/harden.go`.

    game harden                      source, binaries, modules
    game harden xcode -scheme S      the above, plus build settings

Every finding is a `lint.Finding` with rule `harden`. `sayHarden` prints them
all and fails with the count, so one exit code covers source, binary and build
findings alike.

### Layer 1: source rules

`internal/lint/harden.go`. Enabled by `lint harden` in `.game`, which holds
every `game lint`, `verify` and `release` to the Go and native rules alike;
`game harden` and `game harden xcode` always run them.

For Go, the checks are AST-based:

- the `syscall.` and `unix.` privilege calls (`Setuid`, `Seteuid`, `Setgid`,
  `Setegid`, `Setreuid`, `Setregid`, `Setresuid`, `Setresgid`, `Setgroups`)
- `exec.Command` or `CommandContext` of `sudo` or `su`
- `os.Chmod` and `os.Fchmod` with the setuid or setgid bit set
- `os.WriteFile`, `os.OpenFile`, `os.Mkdir`, `os.MkdirAll`, `os.Chmod` with a
  group-writable or world-writable mode
- `tls.Config` with `InsecureSkipVerify: true`

For C, Objective-C and Swift, the checks are line-based, so each requires a
call or a permission rather than a word in passing:

- the same privilege calls (`setuid`, `seteuid`, `setgid`, and so on)
- `chown`, `fchown`, `lchown`
- `system`, `execl`, `execlp`, `execv`, `execvp` and friends of `sudo` or `su`
- `chmod` or `fchmod` setting the setuid or setgid bit
- `open`, `creat`, `mkdir`, `chmod`, `fchmod` with a group-writable or
  world-writable octal mode
- Swift `FileManager` `.posixPermissions` written loosely, and `Process`
  `launchPath` or `executableURL` of `sudo` or `su`
- `kCFStreamSSLAllowsAnyRoot`

Comment and preprocessor or directive lines are skipped. A line may opt out
with `noqa`, as everywhere else in game's lint.

### Layer 2: Go analyzers and dependencies

`cmd/game/harden_go.go`.

- `gosec -no-fail -fmt=json ./...` is parsed into one finding per issue, at
  the file and line gosec names.
- `govulncheck -json ./...` is a stream of JSON objects, not one document:
  `osv` records naming each vulnerability, and `finding` records tracing
  each reach of one.

A govulncheck finding whose trace names a function is a call the module
makes into vulnerable code, and becomes one finding at the caller in this
tree (or at the module, when the trace ends outside it).

A govulncheck finding with no function is an import that never reaches the
vulnerable symbol, which govulncheck itself calls informational, and is not
held against the tree.

Each tool runs from the path `findTool` resolved, so a `go install` that is
not on `PATH` still runs. A missing tool is itself a finding, never a silent
pass, and so is a tool that ran and fell over.

### Layer 3: Mach-O artifact audit

`cmd/game/harden_macho.go`. Every Mach-O already in `bin/` is read back:

- `otool -hv`: every `EXECUTE` must have `PIE`, and no slice may have
  `ALLOW_STACK_EXECUTION`
- `otool -l`: no segment may be at once writable and executable
- `codesign -d --entitlements -`: an unsigned binary is a finding, and so is
  one carrying a weakening entitlement set `<true/>`; one set false is not
- native binaries are also held to the hardened runtime flag
  (`codesign -d -vv` must show `(runtime)`) and to `__RESTRICT,__restrict`
  (present in `otool -l`, which seals the dyld environment)

`codesign -d` writes everything it says to stderr, exit 0 or not, so the
signature is read through `sh -c '... 2>&1'`; the runner keeps stdout alone.

The weakening entitlements checked are `get-task-allow`,
`disable-library-validation`, `allow-jit`,
`allow-dyld-environment-variables`, and
`allow-unsigned-executable-memory`.

Go binaries are told apart by their build info (`debug/buildinfo`), which
every binary the go tool links carries and nothing else can. (`go version
FILE` exits 0 for any file at all, and the first cut of this pass used it,
so every binary passed as Go and the native checks never ran.)

Go binaries are let off the runtime and dyld checks, because Go cmd binaries
cannot take them. They are still held to PIE, the segment check and the
signing checks.

There is no stack canary check. The compiler only arms a frame that holds an
array or takes an address, so a binary with no such frame has no
`___stack_chk_fail` to show, and its absence proves nothing. The canary is a
compiler flag, and `-fstack-protector-strong` is build-c's to set.

There is no explicit `MH_NO_HEAP_EXECUTION` check. The reliable form of that
property, no writable and executable page, is already enforced by the segment
check. The header flag is left out rather than matched with an unverified
token.

### Layer 4: Xcode build settings and analyzer

`cmd/game/harden_xcode.go`.

`game harden xcode -scheme S` reads `xcodebuild -showBuildSettings` and fails
a release configuration that:

- turns off, or never sets, `ENABLE_HARDENED_RUNTIME` (its default is off)
- turns off `ENABLE_STRICT_OBJC_MSGSEND`
- turns off `DEAD_CODE_STRIPPING`
- turns off, or never sets, `GCC_TREAT_WARNINGS_AS_ERRORS` (default off)
- injects base entitlements (`CODE_SIGN_INJECT_BASE_ENTITLEMENTS`)

It then runs `xcodebuild analyze`, so the clang static analyzer uses the
project's own compiler flags, and its `warning:` and `error:` diagnostics are
read back as findings whatever the exit status: with warnings as errors, a
build that fails is exactly the one with findings.

An analyzer that fell over saying nothing is a finding of its own.

### Layer 5: semgrep

`cmd/game/harden_source.go`.

`game harden` runs `semgrep --json --config auto .` and parses its results
into findings. `--config auto` pulls rules from Semgrep's registry by detected
language. A missing `semgrep` is a finding.

## The gate

With `lint harden` in `.game`:

- `game release` runs the lint as ever, and the source rules are in it.
- After the checks and the test targets, `game release` builds every
  `cmd/*` fresh (a stale `bin/` audited clean would be a false claim about
  the tree) and runs the layers beyond the source rules: the Mach-O pass,
  the Go analyzers and semgrep. Any finding refuses the release.
- `--i-mean-it` overrides a harden refusal exactly as it overrides the
  lint's. It is the glass to break when a release has to go out over a
  finding; the findings are still printed, and the override is said.
- `game verify`, the stack member's child, adds a `harden` step after the
  lint and fails the member on any finding, naming the first.

Without `lint harden`, none of it is asked, and the verbs are as they were.

## Tool resolution

`findTool` in `cmd/game/harden_go.go` resolves an external tool by looking on
`PATH`, then `GOBIN`, then `GOPATH/bin`. This makes `go install` and
Homebrew equal. A tool that is still not found is reported as a finding rather
than silently skipped.

## Building hardened

The second pass of this work: game builds what game harden demands, so a
tree game built passes the gate game keeps.

### build-c

`cFlags` in `cmd/game/buildc.go` gained `-fstack-protector-strong` and
`-fPIE`. `-D_FORTIFY_SOURCE=2` was deliberately not added: Apple's clang
pre-defines `_FORTIFY_SOURCE 0`, so defining it again fails under `-Werror`
with `-Wmacro-redefined`, and the macro does nothing on macOS.

`linkHardened` is what build-c and `aslr c` build through. Beyond cFlags it:

- links `-Wl,-sectcreate,__RESTRICT,__restrict,/dev/null`, the segment on
  which dyld ignores `DYLD_INSERT_LIBRARIES` and the rest of its environment
- signs the result `codesign -s - -o runtime -f`, ad hoc for the hardened
  runtime, so dyld holds it to library validation

The instrumented builds (cover, leaks, fuzz, race) are left unsealed on
purpose: `leaks` reaches its program through dyld's environment, and the
sanitizers want that door open too. They are not what ships.

`TestHardenReadsWhatBuildCMade` audits a real build-c binary and expects it
clean. `TestBuildCSealsDyldsEnvironment` plants a constructor through
`DYLD_INSERT_LIBRARIES` and expects it never to run, where a plain `cc`
binary takes it.

### build-xcode

`xcodeHardenedArgv` says every setting in `xcodeSettings` on the
`xcodebuild` command line, where it overrides the project's: the hardened
runtime, strict Objective-C dispatch, dead code stripping, warnings as
errors, and no base entitlements injected.

It is the same table `game harden xcode` audits, so the two cannot drift
apart.

Signing stays the project's. The hardened runtime is a property of a
signature; a scheme that signs with nothing gets none, and `game harden`
then says the binary is not codesigned. `leaks xcode` builds plain, for the
reason above.

### build

The Go linker signs an Apple silicon binary ad hoc and leaves an Intel one
unsigned. `signGo` signs each `cmd/*` ad hoc after linking, on a Mac, so
`game harden` finds it signed either way.

The hardened runtime is left off Go binaries: it brings library validation,
which refuses any dylib a cgo program loads that is not signed by the same
identity, and ad hoc is no identity. harden lets Go binaries off that flag
for the same reason.

## config, docs and vim

- `harden` was added to the lint rule list in `internal/config/config.go` and
  the rule documentation.
- `contrib/vim/syntax/game.vim` recognises `harden`.
- `example/config` notes `lint harden`.
- `usage()`, the `main.go` doc comment, `helpclaude.go` and `README.md` were
  updated for `aslr`, `leaks` and `harden`.

## Tests

New tests cover the parsers and the passes with stand-in tools, and real
end-to-end tests where the tools exist:

- `internal/lint/harden_test.go`: source rules, native and Go alike
- `cmd/game/aslr_test.go`, `cmd/game/leaks_test.go`,
  `cmd/game/harden_macho_test.go`, `cmd/game/harden_go_test.go`,
  `cmd/game/harden_xcode_test.go`, `cmd/game/harden_source_test.go`
- `cmd/game/harden_gate_test.go`: release refusing a harden finding under
  `--i-mean-it`, the layers beyond source running after the tests, and
  verify's `harden` step
- `cmd/game/hardening_e2e_test.go`: real `cc`, `otool`, `codesign` and
  `leaks`, including the Mach-O pass over a binary build-c made

## Known limitations and deferred work

Kept honest, in the order they matter.

- build-c's `-with` libraries are linked in as objects. A dependency that
  came as a dylib would fall foul of library validation under the ad hoc
  hardened runtime; none does today, and the audit would say so.
- `game harden` on its own, as against release and verify, checks what is
  already in `bin/`. An unbuilt tree passes clean on the binary layer.
- There is no triage or baseline for findings. `noqa` works on source lines,
  but there is no allowance for an unsigned developer tool or a legacy
  entitlement, and no severity ordering.
- No secret or token scanning. `sweep` catches paths, UUIDs, emails and URLs,
  not cryptographic secrets or `.env` files.
- The Xcode entitlements file and `Info.plist` are not read. ATS exceptions
  such as `NSAllowsArbitraryLoads`, and entitlements that live in the file
  rather than the built product, are not caught by the settings pass.
- No Linux or ELF equivalents. The whole layer is `otool`, `codesign` and
  `xcrun`.
- Bare `clang --analyze` on standalone C files is not wired, deliberately,
  because without the project's include flags it produces noise. The Xcode
  `analyze` action covers the Xcode side.
- `semgrep` rules are not shipped locally. The default is the registry's
  `auto` config.
- Go binaries are exempt from the hardened runtime and dyld checks.

## External tooling

    brew install gosec govulncheck semgrep

or, for the Go tools, `go install` with `~/go/bin` on `PATH`:

    go install github.com/securego/gosec/v2/cmd/gosec@latest
    go install golang.org/x/vuln/cmd/govulncheck@latest
    python3 -m pip install --user semgrep

Missing tools are findings, so `game harden` will not silently pass without
them.