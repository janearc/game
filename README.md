# game

               _@@====@,_
             _===========@_
            _==============                   @%%@
           %+++============_                  %%%@
           ^%=+============@                 @%%%@
           __===++++==+====@*               _%%%#@
           @====+**+++++===%                %####*    _@@@_
           @%=====+**+*@@#@""               ##%++*  _+*##*#@
       __@@@@@@%#=+++++                     %#****@ @++****@
     _@@@@@@@@@@@=====*@_                    #****#  %++++@"
    |@@@@@@@@@@@@@@=+%#%%@,,,,,__________     %***##%@,_
    @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@#*@,,@@***%%@%@@_
    %@@@@@@@@@@@@@@@@@@@@@@@@@@@#########@@@@@@@@**%#%@@@@
    @@@@@@@@@@@@@@%%%%%%%%%%%%%%*********%@@@@@@@@**#%@@@%_
     %@@%@@@@@@@@***%%%%%%%%%%%%+++++++++%==%@@@@@@%%%@*##"
     @@@@@@@%@@@###@#*###*##%@@@##""""'   " %**%****++***@
      @@@@@@@@@@@@@@@@%#####%               %%*+*+++%@+@"
      @@@@@@@@@@@@@@@%@@@%###@
      @@@@@@@@@@@@@%@@@@@%%#%%      ___ ____ ___ _  ___
      @@@@@@@@@@@@@@@@@@@%%@%%"    / _ `/ _ `/  ' \/ -_)
       %@@@@@@@@@@@@@@@@@%@@@@%/   \_, /\_,_/_/_/_/\__/
       %@@@@@@@@@@@@@@@@@@@@@%%@  /___/
       @@@@@@@@@@@@@@@@@@@@@@%@@  _______________

# why we made this (it is super cool)

go already builds go. we built game anyway, because people who work with
agents run into problems a person working alone never sees. nearly every
verb in game answers one of them.

## what build am i/are you using?

`game --age` was the first. several of us, people and agents, were
building game at the same time, and nobody could say which binary they
were running. a version number didn't help. `--age` prints the commit a
binary was built from, and how long ago that was.

## this tree is unsanitary!

agents commit constantly. that's fine, because their commits go to the
road, a private remote. the dist, the copy people outside see, only ever
gets one flat commit.

before that commit goes out, game sweeps it for what agents leak without
noticing: attribution trailers, session urls, file paths from this
machine, and a list of names that must never be published. game keeps
those names only as hashes, so the game binary contains none of them.

## make is a terrible linter, we can do better.

game's lint checks prose the way gofmt checks spacing. it flags shouting,
exclamation marks, and the words agents overuse. `game bounce` holds any
change that mentions the repository's author in the third person until
she approves it.

## human-agent pairing!

`game stick` is for pairing. it claims a repository for a pairing session
and pushes the claim. `sticky` keeps the other person's copy on the
claim's latest commit, so both of you see the same code. while a
repository is claimed, game will not release it.

## game chose to be pretty, not scary

next, we want pull and push behind game too, so agents never run git push
or touch github directly. game does the risky parts the same safe way
every time.

# unboxing game

the verbs a go project needs that go itself does not know it needs.
all you need is `sh bootstrap.sh install` once, then game builds game.

    game check    gofmt, vet and the tests, exit code kept
    game build    every cmd/* into bin/, stamped; `build NAME` a target,
                  `build stack` a whole stack, `build docker` an image
    game lint     whatever lint your config describes, counted; zero of
                  it is what a release asks for
    game release  one flat commit: the whole tree, none of the road,
                  swept and proven. `release push TAG` is the only thing
                  that reaches the clean remote
    game trees    this project's two remotes, if it has any
    game run      a described run, in your terminal, tmux dropped
    game clean    go clean, and bin/ away; --cache for a cold run
    game bounce   linting for first person; the third person signs off
    game sweep    what must not ship: trailers, urls, uuids, paths, never-words
    game version  which commit this binary is, and how old
    game stick    claims this repository for a pairing session; sticky
                  follows the claim's tip, unstick lets it go clean

settings: `~/.config/game/config`, a block per project, then `.game`,
which names its project. `example/config` is a start.

game. just too stupid not to be pretty.
