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
echo "0: go build ./cmd/game"
go build -o bin/game ./cmd/game
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
