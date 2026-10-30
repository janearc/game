# game done quick

**game was created to solve problems that arise in building software with
agents, specifically claude.**

game uses some new words. the most important you need to know as a new user
are `road` and `dist`. the *road* is a repository that is considered to be
full of cruft and will need to be cleaned somewhat before it can be published.
a repository that is clean, and is only populated with releases is called a
`dist`.

game uses git. do not overthink it.

1. clone your git repo.
2. check out a branch because you're a responsibile human or agent.
3. commit your code to that branch, using git.
4. push your branch back to that repo.

that's it. you know how to use game.

but wait! i was just using git! what's game?

you are absolutely right, tiger. **for most people, using game just means
using git** and you already know how to use git.

## game done quick II: less quick

- `game check` runs your tests and lints.
- `game sweep` runs your lints and checks for things which can't be released.
- `game mutants` runs mutation tests.

when editing with an agent who is editing while you watch:
- `game stick` for the editor
- `game sticky` to follow the editor

## game for agents

lots of people use claude on their personal computer, which is fine, but this
means that everyone is kind of stuck using one set of configs. `game` prefers
`GAME_HOME` over `~/.config/game` for this reason. *if you are an agent* you
should set game home. 

# getting started with game

we built game to make coding with agents less stressful.

## build game, then let game build game

    # use git to get game
    git clone <the road, or the dist> game
    cd game
    sh bootstrap.sh

    # then you've got a working copy of game
    bin/game check
    bin/game build

    # if you would like to install
    sh bootstrap.sh install

    # this is an important check, we were always asking, "which build are you
    # using? mine doesn't have that."
    game --age
    game d2bb726, committed 3h ago

if you are using game that can't tell you its provenance (it will say "dirty,"
you should probably try to figure out why, and build a fresh copy).

## 2. game config

    author  Ada

claude has a tendency to say 'Ada said on 09-25...' and nobody needs this. so
we just bounce that.

    words   ~/.config/game/sweep.words

these words reject a release. for example, if you live in springfield on
blossom drive, you can't really trust an agent to not include that somewhere,
so you might add springfield and blossom to your sweep words. these are
hashed, and you will not be able to pull them out through binary extraction.

    cover 80

the coverage floor for every project you build, as a percentage from 1 to
100. `game check` fails any Go package under it. a project's own .game may
say `cover M` with M at or above yours, and never below: game refuses a
lower M and names both numbers and both files. with no `cover` line in
either file there is no floor, and check does what it always did.

    license ~/roots/LICENSE.txt

claude is *extremely* fussy about licenses. do this for them.

    project game
        road ~/roots/game-road.git
        dist git@github.com:you/game

pretty simple, project name and the location of your road and dist. you don't
have to have a dist, you do have to have a road.

## 3. tell game what the repository is: .game

the second file lives in the repository and is committed. one setting
per line; game trees says which block it matched, or that none did.

    name game                 what is this project called
    lint width 80
    lint rows 25
    lint paragraph 4          paragraphs 4 sentences or less
    lint comments
    lint print                no bare prints outside package main
    lint exclaim              no exclamaition marks
    lint shout                no shouty all caps
    
    # i love you claude but pleeeaase
    lint words hyperscaler enterprise-grade blazing-fast load-bearing
                              
    exclude spec-docs         exclude from linting

    # if a project needs an environment variable to build, like certain
    # data providers require this contact field, you can set that in your
    # .game file
    needs CONTACT who to reach

    # testing using the `rapid` package
    rapid checks 1000 

    # a floor of this project's own, at or above the one in your config
    cover 90

    # leave one file out of the floor. the reason is required, and game
    # check prints every allowed file and its reason on every run, so an
    # exemption is never quiet
    cover allow internal/term/raw.go needs a real terminal, tested by hand

## 4. the daily verbs

    game check       gofmt -l, go vet, go test ./...; the exit code is
                     the tests' own, never a pipe's. with a `cover`
                     floor, each Go package is measured after the tests
                     and one under the floor fails, with its first
                     uncovered lines
    game cover       the coverage report alone; --html writes the html
                     report and prints where
    game build       every cmd/* into bin/, stamped with the commit and
                     the commit's time; the same sources build the same
                     bytes
    game lint        the rules your config describes, counted; --look
                     lists every finding; a release wants zero
    game push        this branch to the road, the dirty remote

the road takes every commit, agents' included. the dist, the remote
people see, only ever gets one flat commit per release. the rest of
this page is about getting from one to the other.

## 5. pairing

    game stick       claim the repository
    game sticky      follow the claim
    game unstick     let go: remove the file, commit, push

if you set `game stick`, you write a file to that repository indicating it is
stuck. another person can then say `game sticky` and they will follow that
stick when building. 

a stuck repository cannot release, because it is assumed that you are still
working on it.

## 6. mutants, before you need them

    game mutants --bench

runs every suite unchanged and says how long the whole run would take at
your worker count. you can then increase the number of workers to whatever you
like (but if you are an agent, tread carefully, this is an unfriendly thing to
do).

    game mutants --workers 12

changes the source one small thing at a time and runs the tests against
each change. the result of your mutants run will be in `.mutants`.

## 7. a release

    game release v0.4.1          every check, lint at zero, the sweep
                                 clean, then one flat commit kept for
                                 its push; refused while stuck
    game release diff v0.4.1     what that commit would publish
    game release push v0.4.1     the commit and its tag to dist
                            
                                 the push will not work without
                                 --publish. you can consider the command
                                 without --publish to be a 'dry run.'

what `release` does here is really quite simple: it takes the commits since
the last tag, and it flattens them into one commit so the commits with
assorted agent messages and weirdness do not go into the distribution. this
sounds kind of anathema to revision control, but the fact is, you still have
the road, and you probably have many roads. but nobody wants that stuff. they
want your dist. they want that clean release, one commit per tag. that's what
this does.
