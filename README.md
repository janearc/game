# game

the verbs a go project needs that go itself does not know are things.
for go projects only, and small ones: it runs git and go and nothing
else, and it is not a build tool for anything bigger. make is the front
door: each target in a project's makefile is one line that calls a
verb here.

    game release      the published history is one commit per release,
                      each the whole tree and none of the road to it:
                      built from the branch you are on, proven to have
                      its tree, swept, pushed to the public root
    game bounce       a repository written in the first person: a staged
                      change that adds the author in the third person is
                      refused until she signs it off
    game sweep        what must not be published: attribution trailers,
                      session urls, uuids, paths on one machine, email
                      addresses, and a word list kept outside the tree
    game version      which commit this binary is, and how old

settings live in `~/.config/game/config` and in `.game` in the
repository, one per line: `author`, `words`, `public`, `exclude`; the
repository's win, except the word list, which is the dotfile's alone. the window a build will one day draw in
is another program's job; this is the part that runs anywhere.

we chose stupid, so you can be pretty. build forward.
