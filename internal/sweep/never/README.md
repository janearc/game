# never

where game's never-words are baked in.

`cmd/never` reads the never-words list, the one outside every
repository: `~/.config/game/sweep.words` by default, or the `words`
line of game's config.

it writes `hashes` here: a salt, then the salted sha-256 of each word or
name, one to a line. go embeds this directory when game is built, so the
binary carries the hashes and never the words. bootstrap.sh runs
`cmd/never` before it builds.

git ignores `hashes`, so no hash reaches a commit. a clone has only
this readme; a game built from it does everything but release, and
says why when asked to.
