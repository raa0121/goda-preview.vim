if exists('g:loaded_goda_preview')
  finish
endif
let g:loaded_goda_preview = 1

" Only commands: autoload/goda_preview.vim, and goda.vim with it, are read by
" their first use.
command! -bar GodaPreviewOpen call goda_preview#open()
command! -bar GodaPreviewClose call goda_preview#close()
command! -bar GodaPreviewRefresh call goda_preview#refresh()
command! -bar GodaPreviewBuild call goda_preview#build()
