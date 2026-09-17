#!/bin/sh
# bootstrap: from a clone to a game that builds itself, in two steps.
# step 0 is go doing what go does: one plain build, no stamps. step 1 is
# game doing the rest: check, then build, which stamps the binary with
# the commit and the time. after this, game builds game.
#
#   sh bootstrap.sh              leaves bin/game
#   sh bootstrap.sh install      also links it as ~/.local/bin/game
set -e
cd "$(dirname "$0")"
# the same two -ldflags game passes for itself. a binary that cannot say
# which commit it is is a binary whose output cannot be traced back to a
# tree, and this is the one build in the estate that go does rather than
# game, so it is the one place an unstamped binary can enter.
commit=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
	commit="$commit-dirty"
fi
built=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "0: go build ./cmd/game ($commit)"
go build -ldflags "-X main.build=$commit -X main.built=$built" \
	-o bin/game ./cmd/game
echo "1: bin/game check"
bin/game check
echo "1: bin/game build"
bin/game build
bin/game version
if [ "$1" = "install" ]; then
  mkdir -p "$HOME/.local/bin"
  ln -sfn "$(pwd)/bin/game" "$HOME/.local/bin/game"
  echo "installed: ~/.local/bin/game -> $(pwd)/bin/game"
fi
