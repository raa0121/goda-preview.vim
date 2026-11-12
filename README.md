# goda-preview.vim

Preview Markdown, AsciiDoc, reStructuredText and Textile in the browser, as
you type. Built on [goda.vim](https://github.com/Shougo/goda.vim); a
replacement for [previm](https://github.com/previm/previm).

- The buffer is sent as you edit it, including unsaved changes, and the page
  updates without reloading (server-sent events).
- The page scrolls with the cursor: line by line for Markdown and AsciiDoc,
  in proportion for reStructuredText and Textile.
- Markdown: CommonMark with GFM (tables, task lists, strikethrough,
  autolinks), footnotes, definition lists, East Asian line breaks, mermaid,
  and PlantUML through a server of your choice.
- AsciiDoc: converted in the page by
  [asciidoctor.js](https://github.com/asciidoctor/asciidoctor.js), in its
  secure mode (no include).
- TeX math with KaTeX: `$…$`, `$$…$$` and ```` ```math ```` in Markdown, the
  math role and directive in reStructuredText, latexmath in AsciiDoc.
- Code blocks of Markdown highlighted by [chroma](https://github.com/alecthomas/chroma),
  with a style for light and one for dark colour schemes.
- Your own stylesheet, reloaded in the page whenever you save it.
- One Go binary, no Node.js, Deno or Python. Relative images are served from
  the directory of the file.

## Requirements

- Vim 9.1 or later with `+job`, `+channel` and `+timers`, or Neovim 0.11 or
  later.
- [goda.vim](https://github.com/Shougo/goda.vim).
- Go 1.27.1 or later to build the binary (an older `go` downloads it by
  itself unless `$GOTOOLCHAIN` is `local`).

## Install

```toml
[[plugins]]
repo = 'Shougo/goda.vim'

[[plugins]]
repo = 'raa0121/goda-preview.vim'
depends = 'goda.vim'
```

Then build the binary once, and again after every update:

```vim
:GodaPreviewBuild
```

## Usage

```vim
:GodaPreviewOpen      " preview the current buffer in the browser
:GodaPreviewClose     " end the preview of the current buffer
:GodaPreviewRefresh   " send the buffer now
```

## Options

```vim
" Loaded after the default stylesheet, or instead of it.
let g:goda_preview#custom_css_path = '~/.vim/preview.css'
let g:goda_preview#disable_default_css = 1

" Render ```plantuml blocks with this server. Without it they stay code, and
" nothing is sent anywhere.
let g:goda_preview#plantuml_server = 'http://localhost:8080'

" chroma styles of the code (https://xyproto.github.io/splash/docs/); an
" empty code_style turns highlighting off.
let g:goda_preview#code_style = 'github'
let g:goda_preview#code_style_dark = 'github-dark'

" Open the URL with this command instead of the default browser.
let g:goda_preview#open_cmd = 'firefox'
```

See `:help goda-preview` for all of them.

## Security

The server listens on 127.0.0.1 only, on a random port. Every URL carries a
random token, and requests for another host name are refused. Raw HTML in a
document is not rendered unless `g:goda_preview#unsafe_html` is set, and
the page never runs scripts of the document.

The page loads from jsDelivr mermaid when it has a diagram, KaTeX when it has
math, and asciidoctor.js for AsciiDoc (`g:goda_preview#mermaid_url`,
`g:goda_preview#katex_url` and `g:goda_preview#asciidoctor_url` change where
from).

## Development

```sh
go test ./...
```

## License

MIT. See [LICENSE](LICENSE).
