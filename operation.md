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
