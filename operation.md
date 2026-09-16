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
