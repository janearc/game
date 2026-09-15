# game

the verbs a go project needs that go itself does not know are things.
for go projects only, and small ones: git and go and nothing else. no
makefile: `sh bootstrap.sh install` once, then game builds game.

    game check     gofmt, vet and the tests, exit code kept
    game build     every cmd/* into bin/, stamped; `build NAME` runs a
                   described target's steps in order, under nice
    game clean     go clean, and bin/ away; --cache for a cold run
    game lint      whatever lint your config describes, counted and
                   listed; example/config describes ours
    game release   one public commit per release, the whole tree and
                   none of the road: proven, swept, lint clean; --tag
    game run       a described run, in your terminal, tmux dropped
    game bounce    first person only; the author in the third person
                   waits for her sign-off
    game sweep     what must not ship: trailers, urls, uuids, paths, words
    game version   which commit this binary is, and how old

settings: `~/.config/game/config`, then `.game` in the repository, one
per line: author, words, public, exclude, lint, target, run; the
repository's win, except the word list. `example/config` is a start.

game. just too stupid not to be pretty. let's build forward.
