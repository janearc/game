# game

the verbs a go project needs that go itself does not know are things.
for go projects only, and small ones: it runs git and go and nothing
else, and it is not a build tool for anything bigger. no makefile:
`sh bootstrap.sh install` once, and game builds game from then on.

    game check        gofmt, vet and the tests, exit code kept
    game build        every cmd/* into bin/, stamped with commit and time
    game clean        go clean, and bin/ away; --cache for a cold run
    game release      the published history is one commit per release,
                      the whole tree and none of the road: proven by
                      hash, swept, pushed to the public root
    game bounce       first person only: a change that adds the author
                      in the third person waits for her sign-off
    game sweep        what must not be published: trailers, session
                      urls, uuids, machine paths, addresses, a word list
    game version      which commit this binary is, and how old

settings live in `~/.config/game/config` and in `.game` in the
repository, one per line: `author`, `words`, `public`, `exclude`; the
repository's win, except the word list, which is the dotfile's alone.

we chose stupid, so you can be pretty. build forward.
