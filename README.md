# game

the verbs a go project needs that go itself does not know are things.
for go projects only, and small ones: it runs git and go and nothing
else. no makefile: `sh bootstrap.sh install` once, then game builds game.

    game check     gofmt, vet and the tests, exit code kept
    game build     every cmd/* into bin/, stamped with commit and time
    game clean     go clean, and bin/ away; --cache for a cold run
    game lint      the house rules: comments, no bare print outside
                   main, no buzzwords or exclamation marks, 80 columns
    game release   one public commit per release, the whole tree and
                   none of the road: proven by hash, swept, lint clean
    game bounce    first person only; the author in the third person
                   waits for her sign-off
    game sweep     what must not ship: trailers, session urls, uuids,
                   machine paths, addresses, and a word list
    game version   which commit this binary is, and how old

settings: `~/.config/game/config`, then `.game` in the repository, one
per line: `author`, `words`, `public`, `exclude`, `lint`. the
repository's win, except the word list. `example/config` is a start.
the window a build will one day draw in is another program's job.

game. just too stupid not to be pretty. let's build forward.
