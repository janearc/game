# Hardening in game

This documents the ASLR, memory-leak and hardening verbs in `game`, for Go
projects. It is written for review: what each verb does, which files hold
it, how it fails and what it leaves out.

The C, Objective-C, Swift and Xcode halves of these verbs (`build-c`,
`build-xcode`, `fuzz`, `race`, the Mach-O audit, the native source rules,
semgrep and the Xcode settings pass) are now in dismay. They are not in game.

## Verbs

    game aslr     prove the built Go binaries are position-independent
    game leaks    find what a Go program holds outside the Go heap
    game harden   audit Go source and modules

### game aslr

`cmd/game/aslr.go`, tests in `cmd/game/aslr_test.go`.

    game aslr                      check every Mach-O already in bin/
    game aslr go                   build every cmd/*, then check

Each Mach-O is read with `otool -hv`. An `EXECUTE` slice without the `PIE`
flag fails the check. Dylibs and frameworks are position-independent by
their filetype and are not asked for the flag. `otool` ships with macOS.

### game leaks

`cmd/game/leaks.go`, tests in `cmd/game/leaks_test.go`.

    game leaks go

Every `cmd/*` is built into `bin/` and run under Apple's `leaks` tool, which
sees only what the program holds outside the Go heap, in cgo and system
frameworks. `leaks` ships with macOS.

## game harden

`cmd/game/harden.go` is the pipeline. The Go analyzers live in
`cmd/game/harden_go.go`, and the source rules in `internal/lint/harden.go`.

    game harden                      source rules, then Go analyzers

Every finding is a `lint.Finding` with rule `harden`. `sayHarden` prints
them all and fails with the count, so one exit code covers source and module
findings alike.

### Layer 1: source rules

`internal/lint/harden.go`. Enabled by `lint harden` in `.game`, which holds
every `game lint`, `verify` and `release` to them; `game harden` always runs
them. The checks are AST-based:

- the `syscall.` and `unix.` privilege calls (`Setuid`, `Seteuid`, `Setgid`,
  `Setegid`, `Setreuid`, `Setregid`, `Setresuid`, `Setresgid`, `Setgroups`)
- `exec.Command` or `CommandContext` of `sudo` or `su`
- `os.Chmod` and `os.Fchmod` with the setuid or setgid bit set
- `os.WriteFile`, `os.OpenFile`, `os.Mkdir`, `os.MkdirAll`, `os.Chmod` with a
  group-writable or world-writable mode
- `tls.Config` with `InsecureSkipVerify: true`

A line may opt out with `noqa`, as everywhere else in game's lint.

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

A missing tool is itself a finding, never a silent pass, and so is a tool
that ran and fell over.

## The gate

With `lint harden` in `.game`:

- `game release` runs the lint as ever, and the source rules are in it.
- After the checks and the test targets, `game release` builds every
  `cmd/*` fresh and runs the Go analyzers. Any finding refuses the release.
- `--i-mean-it` overrides a harden refusal exactly as it overrides the
  lint's. The findings are still printed, and the override is said.
- `game verify`, the stack member's child, adds a `harden` step after the
  lint and fails the member on any finding, naming the first.

Without `lint harden`, none of it is asked, and the verbs are as they were.

## Known limitations

- `game harden` on its own, as against release and verify, does not build
  anything first.
- There is no triage or baseline for findings. `noqa` works on source lines,
  but there is no severity ordering.
- No secret or token scanning. `sweep` catches paths, UUIDs, emails and URLs,
  not cryptographic secrets or `.env` files.
- `aslr` and `leaks` need macOS.

## External tooling

    go install github.com/securego/gosec/v2/cmd/gosec@latest
    go install golang.org/x/vuln/cmd/govulncheck@latest

A tool found on `PATH`, in `GOBIN` or in `GOPATH/bin` runs. Missing tools
are findings, so `game harden` will not silently pass without them.
