" goda-preview.vim - preview Markdown, reStructuredText and Textile in the
" browser, on goda.vim.

" ScriptVersion in internal/preview/plugin.go.
let s:version = #{version: 3, module: 'github.com/raa0121/goda-preview.vim'}
let s:root = expand('<sfile>:p:h:h')
let s:filetypes = ['asciidoc', 'asciidoctor', 'markdown', 'rst', 'textile']
let s:timers = {}

" ---------------------------------------------------------------------------
" Setup

" The binary is machine specific: it is built into a cache directory, not
" into the plugin's directory, which a plugin manager may replace.
function! s:cache_dir() abort
  if has('nvim')
    let base = stdpath('cache')
  elseif !empty($XDG_CACHE_HOME)
    let base = $XDG_CACHE_HOME . '/vim'
  elseif has('win32') && !empty($LOCALAPPDATA)
    let base = $LOCALAPPDATA . '/vim'
  else
    let base = expand('~/.cache/vim')
  endif
  return base . '/goda-preview'
endfunction

function! s:bin() abort
  if exists('g:goda_preview#binary')
    return expand(g:goda_preview#binary)
  endif
  return s:cache_dir() . '/goda-preview' . (has('win32') ? '.exe' : '')
endfunction

function! goda_preview#build() abort
  call goda#build(#{name: 'preview', dir: s:root, bin: s:bin()})
  call goda#register('preview', function('s:start'), s:version)
endfunction

function! s:start() abort
  let bin = s:bin()
  if !executable(bin)
    throw 'goda-preview: ' . bin . ' does not exist yet; build it with: call goda_preview#build()'
  endif
  let plugins = goda#start(bin)
  if index(plugins, 'preview') < 0
    if !empty(plugins)
      call goda#stop(plugins[0])
    endif
    throw 'goda-preview: ' . bin . ' does not contain the "preview" plugin'
  endif
endfunction

call goda#register('preview', function('s:start'), s:version)

" ---------------------------------------------------------------------------
" API

" Open the preview of the current buffer, and the browser on it.
function! goda_preview#open() abort
  call s:open(bufnr('%'), v:true)
endfunction

" Close the preview of the buffer {bufnr} (the current one when omitted).
function! goda_preview#close(bufnr = bufnr('%')) abort
  if exists('#goda_preview_' . a:bufnr)
    execute 'augroup goda_preview_' . a:bufnr
      autocmd!
    augroup END
    execute 'augroup! goda_preview_' . a:bufnr
  endif
  call s:stop_timer(a:bufnr)
  if goda#is_running('preview')
    call goda#notify('preview', 'close', a:bufnr)
  endif
endfunction

" Send the current buffer now, without waiting for g:goda_preview#delay.
function! goda_preview#refresh() abort
  call s:update(bufnr('%'))
endfunction

" The URL of the preview of the current buffer, or '' when there is none.
function! goda_preview#url() abort
  if !goda#is_running('preview')
    return ''
  endif
  return goda#request('preview', 'url', bufnr('%'))
endfunction

" ---------------------------------------------------------------------------
" Internal

" BUFNR is the current buffer.
function! s:open(bufnr, browse) abort
  let bufnr = a:bufnr
  let filetype = s:filetype(bufnr)
  if filetype ==# ''
    call s:error('filetype "' . &filetype . '" is not supported (supported: '
          \ . join(s:filetypes, ', ') . ')')
    return
  endif
  let open_cmd = get(g:, 'goda_preview#open_cmd', '')
  let config = #{
        \ customCSS: s:custom_css(),
        \ disableDefaultCSS: s:bool('goda_preview#disable_default_css'),
        \ mermaidURL: get(g:, 'goda_preview#mermaid_url', ''),
        \ katexURL: get(g:, 'goda_preview#katex_url', ''),
        \ asciidoctorURL: get(g:, 'goda_preview#asciidoctor_url', ''),
        \ plantumlServer: get(g:, 'goda_preview#plantuml_server', ''),
        \ codeStyle: get(g:, 'goda_preview#code_style', 'github'),
        \ codeStyleDark: get(g:, 'goda_preview#code_style_dark', 'github-dark'),
        \ unsafeHTML: s:bool('goda_preview#unsafe_html'),
        \ hardLineBreak: s:bool('goda_preview#hard_line_break'),
        \ openBrowser: a:browse && empty(open_cmd) ? v:true : v:false,
        \ }
  let buffer = #{
        \ bufnr: bufnr,
        \ path: s:path(bufnr),
        \ filetype: filetype,
        \ text: s:text(bufnr),
        \ line: line('.'),
        \ total: line('$'),
        \ }
  try
    let url = goda#request('preview', 'open', buffer, config)
  catch
    call s:error(v:exception)
    return
  endtry
  if a:browse && !empty(open_cmd)
    call s:run(open_cmd, url)
  endif

  execute 'augroup goda_preview_' . bufnr
    autocmd!
    execute 'autocmd TextChanged,TextChangedI,TextChangedP <buffer=' . bufnr
          \ . '> call s:schedule_update(' . bufnr . ')'
    execute 'autocmd CursorMoved,CursorMovedI <buffer=' . bufnr
          \ . '> call s:send_cursor(' . bufnr . ')'
    " Sent again on a write: the file name may have changed (:saveas), and
    " so may the options.
    execute 'autocmd BufFilePost,BufWritePost <buffer=' . bufnr
          \ . '> call s:reopen(' . bufnr . ')'
    execute 'autocmd BufWipeout <buffer=' . bufnr
          \ . '> call goda_preview#close(' . bufnr . ')'
  augroup END
endfunction

" The renderer of the buffer: its filetype, or the first supported one of
" a compound filetype ("markdown.pandoc").
function! s:filetype(bufnr) abort
  for ft in split(getbufvar(a:bufnr, '&filetype'), '\.')
    if index(s:filetypes, ft) >= 0
      return ft
    endif
  endfor
  return ''
endfunction

function! s:path(bufnr) abort
  let name = bufname(a:bufnr)
  return empty(name) ? '' : s:native(fnamemodify(name, ':p'))
endfunction

" The binary is a native Windows program under MSYS2 and Cygwin, where Vim
" has paths of the form /c/....
function! s:native(path) abort
  if !has('win32unix') || !executable('cygpath')
    return a:path
  endif
  let out = system('cygpath -w ' . shellescape(a:path))
  return v:shell_error ? a:path : substitute(out, '\n\+$', '', '')
endfunction

function! s:text(bufnr) abort
  return join(getbufline(a:bufnr, 1, '$'), "\n")
endfunction

function! s:custom_css() abort
  let path = get(g:, 'goda_preview#custom_css_path', '')
  return empty(path) ? '' : s:native(fnamemodify(expand(path), ':p'))
endfunction

" The Go side refuses a number where it expects a boolean.
function! s:bool(name) abort
  return get(g:, a:name, 0) ? v:true : v:false
endfunction

function! s:schedule_update(bufnr) abort
  call s:stop_timer(a:bufnr)
  let s:timers[a:bufnr] = timer_start(get(g:, 'goda_preview#delay', 100),
        \ {-> s:update(a:bufnr)})
endfunction

function! s:stop_timer(bufnr) abort
  if has_key(s:timers, a:bufnr)
    call timer_stop(remove(s:timers, a:bufnr))
  endif
endfunction

function! s:update(bufnr) abort
  if has_key(s:timers, a:bufnr)
    call remove(s:timers, a:bufnr)
  endif
  if !bufexists(a:bufnr) || !goda#is_running('preview')
    return
  endif
  call goda#notify('preview', 'update', a:bufnr, s:text(a:bufnr))
  call s:send_cursor(a:bufnr)
endfunction

function! s:send_cursor(bufnr) abort
  if bufnr('%') != a:bufnr || !goda#is_running('preview')
    return
  endif
  let pos = [line('.'), line('$')]
  if getbufvar(a:bufnr, 'goda_preview_cursor', []) ==# pos
    return
  endif
  call setbufvar(a:bufnr, 'goda_preview_cursor', pos)
  call goda#notify('preview', 'cursor', a:bufnr, pos[0], pos[1])
endfunction

function! s:reopen(bufnr) abort
  if !goda#is_running('preview') || bufnr('%') != a:bufnr
    return
  endif
  call s:stop_timer(a:bufnr)
  " The URL stays the same, and the browser already shows it.
  call s:open(a:bufnr, v:false)
endfunction

function! s:run(cmd, url) abort
  let cmd = type(a:cmd) == v:t_list
        \ ? a:cmd + [a:url]
        \ : a:cmd . ' ' . shellescape(a:url)
  if has('nvim')
    call jobstart(cmd, #{detach: v:true})
  elseif type(cmd) == v:t_list
    call job_start(cmd)
  else
    call job_start([&shell, &shellcmdflag, cmd])
  endif
endfunction

function! s:error(msg) abort
  echohl ErrorMsg
  echomsg a:msg =~# '^goda-preview: ' ? a:msg : 'goda-preview: ' . a:msg
  echohl None
endfunction
