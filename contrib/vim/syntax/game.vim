" vim syntax for game's settings: ~/.config/game/config and a repository's
" .game. copy syntax/, ftdetect/ and ftplugin/ into ~/.vim or ~/.config/nvim.
" the colours come from your colourscheme; nothing here picks one.

if exists("b:current_syntax")
  finish
endif

" game refuses a word it does not know at the start of a line, so vim
" marks one red first. everything below that knows the word wins.
syn match gameUnknown "^\s*\S\+"

syn match gameKey "^\(author\|words\|name\|exclude\|omit\|sweep\|lint\|target\|run\|needs\|set\|stack\|license\)\>"
syn match gameProject "^project\>" nextgroup=gameProjectName skipwhite
syn match gameProjectName "\S\+" contained
syn match gameBlockKey "^\s\+\(road\|set\)\>"
" dist is the remote that publishes, so it stands out.
syn match gameDist "^\s\+dist\>"

syn match gameLintRule "^lint\s\+\zs\(width\|rows\|paragraph\|comments\|print\|exclaim\|shout\|words\|allow\)\>"
syn match gameSweepKind "\<\(paths\|sessions\|uuids\|emails\|word\)\>"
syn match gameName "^\s*\(set\|needs\|stack\)\s\+\zs\S\+"
syn match gameNumber "\s\zs\d\+\>"
syn match gameDelimiter "\s\zs::\ze\s"
syn match gamePath "\~\?/\S*"
syn match gameURL "\<\(https\?\|ssh\|git\)://\S\+\|\<git@\S\+"

syn match gameComment "^\s*#.*$" contains=@Spell

hi def link gameUnknown     Error
hi def link gameKey         Keyword
hi def link gameProject     Keyword
hi def link gameProjectName Title
hi def link gameBlockKey    Statement
hi def link gameDist        WarningMsg
hi def link gameLintRule    Function
hi def link gameSweepKind   Constant
hi def link gameName        Identifier
hi def link gameNumber      Number
hi def link gameDelimiter   Delimiter
hi def link gamePath        String
hi def link gameURL         Underlined
hi def link gameComment     Comment

let b:current_syntax = "game"
