" vim settings for game's settings files, and :GameAlign.

if exists("b:did_ftplugin")
  finish
endif
let b:did_ftplugin = 1

setlocal commentstring=#\ %s comments=:#

" GameAlign lines up the :: in each run of consecutive run and target
" lines, so every command starts in the same column. it works on a range,
" or the whole file, and running it twice changes nothing.
command! -buffer -range=% GameAlign call s:align(<line1>, <line2>)

function! s:align(first, last) abort
  let block = []
  for lnum in range(a:first, a:last + 1)
    let line = lnum <= a:last ? getline(lnum) : ''
    if line =~# '^\(run\|target\)\s.\{-}\s::\s'
      call add(block, lnum)
    else
      call s:pad(block)
      let block = []
    endif
  endfor
endfunction

function! s:pad(block) abort
  let parts = {}
  let width = 0
  for lnum in a:block
    let [left, right] = matchlist(getline(lnum), '^\(.\{-}\)\s\+::\s\+\(.*\)$')[1:2]
    let parts[lnum] = [left, right]
    let width = max([width, strdisplaywidth(left)])
  endfor
  for lnum in a:block
    let [left, right] = parts[lnum]
    call setline(lnum, left . repeat(' ', width - strdisplaywidth(left)) . ' :: ' . right)
  endfor
endfunction
