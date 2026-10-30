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
road, a private remote, and the road is dirty on purpose. the dist, the
copy people outside see, only ever gets one flat commit.

before that commit goes out, game sweeps it for what agents leak without
noticing: attribution trailers, session urls, file paths from this
machine, and a list of names that must never be published. game keeps
those names only as hashes, so the game binary contains none of them.

## make is a terrible linter, we can do better.

game's lint checks prose the way gofmt checks spacing. it flags
shouting, exclamation marks, the words agents overuse, and the four
habits of a comment that is not plain: chained clauses, too many
commas, an opening a or an, and a comment telling the code's past.

a line that says noqa is let off every rule. `game bounce` holds any
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

    game check    gofmt, vet and the tests, exit code kept. with a `cover`
                  line in your config or the project's .game, it then
                  measures each Go package's coverage, as `game cover`
                  does, and fails a package under the floor. it names the
                  package, its percentage and its first uncovered lines.
                  files with the "Code generated ... DO NOT EDIT." header
                  and packages with no statements are skipped
    game cover    the Go coverage report alone, file by file, least first;
                  `--html` writes the html report and prints its path. for
                  Go only; the C and Xcode verbs are now in dismay
    game mutants  the mutation run: the source changed one thing at a time,
                  the tests run against each; what survives is listed, and
                  .mutants names the changes that make no difference.
                  each survivor and each timeout is printed on its own
                  line the moment it is decided, in the form of the
                  report (`survived   file:line name`), so a long run
                  can be fixed while it runs; a saved copy of those
                  lines goes to `--again`. the tally at the end names
                  them again.
                  SIGINT or SIGTERM stops the run, which still writes
                  --report (a first line says it is partial and how many
                  mutants were not reached), tells its tally, and exits
                  nonzero. `--timeout` must be at least twice the time the
                  unchanged suite takes; with less, the run refuses to
                  start and says the suite's time and the timeout to give.
                  the copies of the tree are made in `$TMPDIR` when that is
                  a folder outside /var/folders, and otherwise in
                  game/mutants in your user cache folder
                  (~/Library/Caches on macOS), because Xcode does not
                  measure coverage on sources under /var/folders. they are
                  removed when the run ends, stopped or not
    game build    every cmd/* into bin/, stamped; `build NAME` a target,
                  `build stack` a whole stack, `build docker` an image
    game lint     whatever lint your config describes, counted; zero of
                  it is what a release asks for. `lint harden` in .game
                  holds the tree to the harden rule below, and a release
                  refuses what fails it, as it refuses the lint
    game harden   Go source and modules held to the hardened default
    game aslr     build, then check the binaries are position-independent
                  (`go`; no word checks every Mach-O in bin/)
    game leaks    build and run under leaks (`go`) to find what the program
                  held outside the Go heap

the C, Objective-C, Swift and Xcode verbs (`build-c`, `build-xcode`, `fuzz`,
`race`, `mutants mutate-c`, `mutate-xcode` and `mutate-text`, and the native
halves of `cover`, `aslr`, `leaks` and `harden`) are now in dismay.

    game release  one flat commit: the whole tree, none of the road,
                  swept and proven. `release push TAG` is the only thing
                  that reaches the clean remote
    game trees    this project's two remotes, if it has any
    game history  the last release and three commits; `game who` also works
    game run      a described run, in your terminal, tmux dropped
    game clean    go clean, and bin/ away; --cache for a cold run
    game bounce   linting for first person; the third person signs off
    game sweep    what must not ship: trailers, urls, uuids, paths, never-words
    game --age    which commit this binary is, and how old
    game stick    claims this repository for a pairing session; sticky
                  follows the claim's tip, unstick lets it go clean

game build stamps four strings into each command's main package, for
its `--age` and `--who`:

    main.build         the commit, short, with -dirty when the tree is
    main.built         that commit's time
    main.stampedWho    the session and machine that built it, jm/4b732342 on
                       loft, or no session on loft when the environment held
                       no session id, or none shaped like one
    main.stampedSids   the last three session lines that end the commits
                       behind it: what the messages say, not a signature

settings: `~/.config/game/config`, a block per project, then `.game`,
which names its project. `example/config` is a start.

game. too stupid not to be pretty.
