# oh, agents, how we love them

agents do lots of work for us, but sometimes they're a little too enthusiastic.
so we have two trees, which we will call `-dist` and `-road`.

`-road` is the tree you work in. `-dist` is the remote you push to, for
distribution.

you configure your remotes in your game config, which lives in
`~/.config/game/config`.

when you want to cut a release and you want it to be
clean and free of your environment variables or all caps and so on, which
agents do because they want to be helpful, we just clean that up. this was a
primary consideration in the operation of `game`.

```
$ game trees
road: ~/git/roots/game-road.git
dist: github.com/janearc/game

$ game build [optional: target]
# ... game builds this locally for you, from road

$ game release v0.3.0
# ... game cuts a release at tag v0.3.0 with extensive linting
# ... when this completes successfully you may push

$ game release push v0.3.0
# ... game pushes a flat build from your tree in road to the dist remote
```

## the never-words

the names that must never reach a dist live in one list outside every
repository, `~/.config/game/sweep.words`: a word or a name of two or three
on each line. `sh bootstrap.sh` runs `cmd/never` first, which bakes the
list into `internal/sweep/never/hashes` as salted hashes.

go embeds that file when it builds game, and git ignores it, so neither a
word nor its hash reaches a commit. the salt is made once and kept beside
the list, in `sweep.salt`, so the same list builds the same binary.

the sweep hashes every word it reads, and every run of two and three, so
a name broken across a line is still caught. a hit says "a never-word" and
not which one. change the list, then run bootstrap again.

a game built without the list does everything but release: `game sweep`
will not call a tree clean, and `game release` refuses and says why.

## what a build is stamped with

`game build` stamps each binary with four things. They are the short
commit it was built from, marked `-dirty` when the tree had changes, and
the time of that commit, not the clock. They are who built it, and the
sessions named in the commits.

`game --age` reads back the commit and its time. a tool built this way
can read the builder and the sessions with `--who`. go's `-trimpath` keeps
the builder's paths out of the binary.

the never-words are not in that list. go embeds
`internal/sweep/never/hashes` as the file stands when it compiles. git
ignores that file, so the commit does not say which list a binary carries.

only `sh bootstrap.sh` writes the file, by running `cmd/never`. `game
build` compiles what is there and does not write it. nothing compares the
embedded hashes to the list on disk.

so two binaries from one commit can differ in what they refuse. after the
list changes, run bootstrap again and build again. a game whose hashes are
out of date does not say so.

`never --age` says which commit `cmd/never` was built from. it does not say
how old the list is.
