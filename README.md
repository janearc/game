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

the verbs a go project needs that go itself does not know are things.
git and go and nothing else, no makefile: `sh bootstrap.sh install`
once, then game builds game.

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
    game bounce   first person only; the third person waits for a sign-off
    game sweep    what must not ship: trailers, urls, uuids, paths, words
    game version  which commit this binary is, and how old

settings: `~/.config/game/config`, a block per project, then `.game`,
which names its project. `example/config` is a start.

game. just too stupid not to be pretty. let's build forward.
